<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Metrics;

use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Model\Metrics\MetricKind;
use Platformsh\Cli\Selector\Selector;
use Khill\Duration\Duration;
use Platformsh\Cli\Service\PropertyFormatter;
use Platformsh\Cli\Service\Table;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'metrics:disk-usage', description: 'Show disk usage of an environment', aliases: ['disk'])]
class DiskUsageCommand extends MetricsCommandBase
{
    /** @var array<string, string> */
    private const TABLE_HEADER = [
        'timestamp' => 'Timestamp',
        'service' => 'Service',
        'type' => 'Type',
        'storage_used' => 'Storage used',
        'storage_limit' => 'Storage limit',
        'storage_percent' => 'Storage %',
        'storage_iused' => 'Storage inodes used',
        'storage_ilimit' => 'Storage inodes limit',
        'storage_ipercent' => 'Storage inodes %',
        'object_used' => 'Object storage used',
        'object_limit' => 'Object storage limit',
        'object_percent' => 'Object storage %',
        'object_count' => 'Object count',
        'instance_used' => 'Instance disk used',
        'instance_limit' => 'Instance disk limit',
        'instance_percent' => 'Instance disk %',
        'instance_iused' => 'Instance disk inodes used',
        'instance_ilimit' => 'Instance disk inodes limit',
        'instance_ipercent' => 'Instance disk inodes %',
        'tmp_used' => '/tmp used',
        'tmp_limit' => '/tmp limit',
        'tmp_percent' => '/tmp %',
        'tmp_iused' => '/tmp inodes used',
        'tmp_ilimit' => '/tmp inodes limit',
        'tmp_ipercent' => '/tmp inodes %',
    ];
    /**
     * Deprecated columns, with the raw /mnt values of every container. They are added only when requested.
     *
     * @var array<string, string>
     */
    private const DEPRECATED_HEADER = [
        'used' => 'Used',
        'limit' => 'Limit',
        'percent' => 'Used %',
        'iused' => 'Inodes used',
        'ilimit' => 'Inodes limit',
        'ipercent' => 'Inodes %',
    ];
    /** @var string[] */
    private array $defaultColumns = [
        'timestamp',
        'service',
        'storage_used',
        'storage_limit',
        'storage_percent',
        'storage_ipercent',
        'object_used',
        'object_limit',
        'object_percent',
        'instance_used',
        'instance_limit',
        'instance_percent',
        'instance_ipercent',
        'tmp_percent',
    ];
    /** @var string[] */
    private array $tmpReportColumns = ['timestamp', 'service', 'tmp_used', 'tmp_limit', 'tmp_percent', 'tmp_ipercent'];

    public function __construct(
        private readonly PropertyFormatter $propertyFormatter,
        Selector $selector,
        Table $table
    ) {
        parent::__construct($selector, $table);
    }

    protected function configure(): void
    {
        $this->addOption('bytes', 'B', InputOption::VALUE_NONE, 'Show sizes in bytes')
            ->addOption('tmp', null, InputOption::VALUE_NONE, 'Report temporary disk usage (shows columns: ' . implode(', ', $this->tmpReportColumns) . ', unless --columns is used)');
        $this->addMetricsOptions();
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);
        Table::configureInput($this->getDefinition(), self::TABLE_HEADER, $this->defaultColumns);
        PropertyFormatter::configureInput($this->getDefinition());
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        // --tmp sets the default columns, which --columns overrides.
        if (Option::bool($input, 'tmp') && Option::stringArray($input, 'columns') === []) {
            $input->setOption('columns', $this->tmpReportColumns);
        }
        $this->table->removeDeprecatedColumns(['interval'], '', $input, $output);

        [$values, $environment] = $this->processQuery($input, [MetricKind::API_TYPE_DISK, MetricKind::API_TYPE_INODES], [MetricKind::API_AGG_AVG]);

        $bytes = Option::bool($input, 'bytes');

        $fields = $this->diskFields(self::STORAGE, $bytes, 'storage_', 'storage_i')
            + $this->objectStorageFields($bytes, 'object_')
            + $this->diskFields(self::INSTANCE_DISK, $bytes, 'instance_', 'instance_i')
            + $this->diskFields('/tmp', $bytes, 'tmp_', 'tmp_i');

        $replacements = [];
        foreach (array_keys(self::DEPRECATED_HEADER) as $column) {
            $replacements[$column] = \sprintf('<info>storage_%s</info> (services) or <info>instance_%1$s</info> (apps and workers)', $column);
        }
        [$header, $fields] = $this->addDeprecatedColumns(self::TABLE_HEADER, $fields, self::DEPRECATED_HEADER, $this->diskFields('/mnt', $bytes, '', 'i'), $replacements, $output);

        $rows = $this->buildRows($values, $fields, $environment);
        $defaultColumns = $this->defaultColumns;
        if (!$this->showInstanceDisk($values, $environment)) {
            $defaultColumns = array_values(array_diff($defaultColumns, ['instance_used', 'instance_limit', 'instance_percent', 'instance_ipercent']));
        }
        if (!$this->showObjectStorage($values, $environment)) {
            $defaultColumns = array_values(array_diff($defaultColumns, ['object_used', 'object_limit', 'object_percent']));
        }

        if (!$this->table->formatIsMachineReadable()) {
            $formatter = $this->propertyFormatter;
            $this->stdErr->writeln(\sprintf(
                'Average %s at <info>%s</info> intervals from <info>%s</info> to <info>%s</info>:',
                Option::bool($input, 'tmp') ? 'temporary disk usage' : 'disk usage',
                (new Duration())->humanize($values['_grain']),
                $formatter->formatDate($values['_from']),
                $formatter->formatDate($values['_to']),
            ));
        }

        $this->table->render($rows, $header, $defaultColumns);

        return 0;
    }
}
