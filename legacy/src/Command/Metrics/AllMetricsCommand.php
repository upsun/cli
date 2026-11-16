<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Metrics;

use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Model\Metrics\Aggregation;
use Platformsh\Cli\Model\Metrics\Field;
use Platformsh\Cli\Model\Metrics\Format;
use Platformsh\Cli\Model\Metrics\MetricKind;
use Platformsh\Cli\Model\Metrics\SourceField;
use Platformsh\Cli\Model\Metrics\SourceFieldPercentage;
use Platformsh\Cli\Selector\SelectorConfig;
use Platformsh\Cli\Selector\Selector;
use Khill\Duration\Duration;
use Platformsh\Cli\Service\PropertyFormatter;
use Platformsh\Cli\Service\Table;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'metrics:all', description: 'Show CPU, disk and memory metrics for an environment', aliases: ['metrics', 'met'])]
class AllMetricsCommand extends MetricsCommandBase
{
    /** @var array<string, string> */
    private const TABLE_HEADER = [
        'timestamp' => 'Timestamp',
        'service' => 'Service',
        'type' => 'Type',

        'cpu_used' => 'CPU used',
        'cpu_limit' => 'CPU limit',
        'cpu_percent' => 'CPU %',

        'mem_used' => 'Memory used',
        'mem_limit' => 'Memory limit',
        'mem_percent' => 'Memory %',

        'storage_used' => 'Storage used',
        'storage_limit' => 'Storage limit',
        'storage_percent' => 'Storage %',

        'storage_inodes_used' => 'Storage inodes used',
        'storage_inodes_limit' => 'Storage inodes limit',
        'storage_inodes_percent' => 'Storage inodes %',

        'object_storage_used' => 'Object storage used',
        'object_storage_limit' => 'Object storage limit',
        'object_storage_percent' => 'Object storage %',
        'object_storage_count' => 'Object count',

        'instance_disk_used' => 'Instance disk used',
        'instance_disk_limit' => 'Instance disk limit',
        'instance_disk_percent' => 'Instance disk %',

        'instance_inodes_used' => 'Instance disk inodes used',
        'instance_inodes_limit' => 'Instance disk inodes limit',
        'instance_inodes_percent' => 'Instance disk inodes %',

        'tmp_disk_used' => '/tmp used',
        'tmp_disk_limit' => '/tmp limit',
        'tmp_disk_percent' => '/tmp %',

        'tmp_inodes_used' => '/tmp inodes used',
        'tmp_inodes_limit' => '/tmp inodes limit',
        'tmp_inodes_percent' => '/tmp inodes %',
    ];

    /**
     * Deprecated columns, with the raw /mnt values of every container. They are added only when requested.
     *
     * @var array<string, string>
     */
    private const DEPRECATED_HEADER = [
        'disk_used' => 'Disk used',
        'disk_limit' => 'Disk limit',
        'disk_percent' => 'Disk %',

        'inodes_used' => 'Inodes used',
        'inodes_limit' => 'Inodes limit',
        'inodes_percent' => 'Inodes %',
    ];

    /** @var string[] */
    private array $defaultColumns = [
        'timestamp',
        'service',

        'cpu_percent',
        'mem_percent',
        'storage_percent',
        'object_storage_percent',
        'instance_disk_percent',
        'tmp_disk_percent',
    ];

    public function __construct(
        private readonly PropertyFormatter $propertyFormatter,
        Selector $selector,
        Table $table
    ) {
        parent::__construct($selector, $table);
    }

    protected function configure(): void
    {
        $this->addOption('bytes', 'B', InputOption::VALUE_NONE, 'Show sizes in bytes');
        $this->addExample('Show metrics for the last ' . (new Duration())->humanize(self::DEFAULT_RANGE));
        $this->addExample('Show metrics over the last hour', ' -r 1h');
        $this->addExample('Show metrics for all SQL services', '--type mariadb,%sql');
        $this->addMetricsOptions();
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);
        Table::configureInput($this->getDefinition(), self::TABLE_HEADER, $this->defaultColumns);
        PropertyFormatter::configureInput($this->getDefinition());
    }

    protected function getChooseEnvFilter(): ?callable
    {
        return SelectorConfig::filterEnvsMaybeActive();
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        [$values, $environment] = $this->processQuery($input, [
            MetricKind::API_TYPE_CPU,
            MetricKind::API_TYPE_DISK,
            MetricKind::API_TYPE_MEMORY,
            MetricKind::API_TYPE_INODES,
        ], [MetricKind::API_AGG_AVG]);

        $bytes = Option::bool($input, 'bytes');

        $fields = [
            'cpu_used' => new Field(
                Format::Rounded2p,
                new SourceField(MetricKind::CpuUsed, Aggregation::Avg),
            ),
            'cpu_limit' => new Field(
                Format::Rounded2p,
                new SourceField(MetricKind::CpuLimit, Aggregation::Max),
            ),
            'cpu_percent' => new Field(
                Format::Percent,
                new SourceFieldPercentage(
                    new SourceField(MetricKind::CpuUsed, Aggregation::Avg),
                    new SourceField(MetricKind::CpuLimit, Aggregation::Max)
                ),
            ),

            'mem_used' => new Field(
                $bytes ? Format::Rounded : Format::Memory,
                new SourceField(MetricKind::MemoryUsed, Aggregation::Avg),
            ),
            'mem_limit' => new Field(
                $bytes ? Format::Rounded : Format::Memory,
                new SourceField(MetricKind::MemoryLimit, Aggregation::Max),
            ),
            'mem_percent' => new Field(
                Format::Percent,
                new SourceFieldPercentage(
                    new SourceField(MetricKind::MemoryUsed, Aggregation::Avg),
                    new SourceField(MetricKind::MemoryLimit, Aggregation::Max)
                ),
                false,
            ),
        ];
        $fields += $this->diskFields(self::STORAGE, $bytes, 'storage_', 'storage_inodes_');
        $fields += $this->objectStorageFields($bytes, 'object_storage_');
        $fields += $this->diskFields(self::INSTANCE_DISK, $bytes, 'instance_disk_', 'instance_inodes_');
        $fields += $this->diskFields('/tmp', $bytes, 'tmp_disk_', 'tmp_inodes_');

        $replacements = [];
        foreach (['used', 'limit', 'percent'] as $suffix) {
            $replacements['disk_' . $suffix] = \sprintf('<info>storage_%s</info> (services) or <info>instance_disk_%1$s</info> (apps and workers)', $suffix);
            $replacements['inodes_' . $suffix] = \sprintf('<info>storage_inodes_%s</info> (services) or <info>instance_inodes_%1$s</info> (apps and workers)', $suffix);
        }
        [$header, $fields] = $this->addDeprecatedColumns(self::TABLE_HEADER, $fields, self::DEPRECATED_HEADER, $this->diskFields('/mnt', $bytes, 'disk_', 'inodes_'), $replacements, $output);

        $rows = $this->buildRows($values, $fields, $environment);
        $defaultColumns = $this->defaultColumns;
        if (!$this->showInstanceDisk($values, $environment)) {
            $defaultColumns = array_values(array_diff($defaultColumns, ['instance_disk_percent']));
        }
        if (!$this->showObjectStorage($values, $environment)) {
            $defaultColumns = array_values(array_diff($defaultColumns, ['object_storage_percent']));
        }

        if (!$this->table->formatIsMachineReadable()) {
            $formatter = $this->propertyFormatter;
            $this->stdErr->writeln(\sprintf(
                'Metrics at <info>%s</info> intervals from <info>%s</info> to <info>%s</info>:',
                (new Duration())->humanize($values['_grain']),
                $formatter->formatDate($values['_from']),
                $formatter->formatDate($values['_to']),
            ));
        }

        $this->table->render($rows, $header, $defaultColumns);

        if (!$this->table->formatIsMachineReadable()) {
            $this->explainHighMemoryServices();
            $this->stdErr->writeln('');
            $this->stdErr->writeln('You can run the <info>cpu</info>, <info>disk</info> and <info>mem</info> commands for more detail.');
        }

        return 0;
    }
}
