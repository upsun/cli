<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Resources;

use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Service\ResourcesUtil;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\PropertyFormatter;
use Platformsh\Cli\Service\Table;
use Platformsh\Client\Exception\EnvironmentStateException;
use Platformsh\Client\Model\Deployment\Task;
use Platformsh\Client\Model\Deployment\WebApp;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'resources:get', description: 'View the resources of apps, tasks and services on an environment', aliases: ['resources', 'res'])]
class ResourcesGetCommand extends ResourcesCommandBase
{
    /** @var array<string, string> */
    protected array $tableHeader = [
        'service' => 'Container',
        'type' => 'Type',
        'profile' => 'Profile',
        'profile_size' => 'Size',
        'cpu_type' => 'CPU type',
        'cpu' => 'CPU',
        'memory' => 'Memory (MB)',
        'disk' => 'Disk (MB)',
        'object_storage' => 'Object storage (MB)',
        'instance_count' => 'Instances',
        'base_memory' => 'Base memory',
        'memory_ratio' => 'Memory ratio',
    ];

    /** @var string[] */
    protected array $defaultColumns = ['service', 'profile_size', 'cpu_type', 'cpu', 'memory', 'disk', 'object_storage', 'instance_count'];

    public function __construct(private readonly Api $api, private readonly Config $config, private readonly PropertyFormatter $propertyFormatter, private readonly ResourcesUtil $resourcesUtil, private readonly Selector $selector, private readonly Table $table)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addOption('service', 's', InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by service name. This can select any service, including apps, workers and tasks.')
            ->addOption('app', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by app name')
            ->addOption('worker', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by worker name')
            ->addOption('task', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by task name')
            ->addOption('type', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by service, app or worker type, e.g. "postgresql"')
            ->addOption('cpu-type', null, InputOption::VALUE_OPTIONAL, 'Filter by CPU type, e.g "guaranteed"');
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);
        Table::configureInput($this->getDefinition(), $this->tableHeader, $this->defaultColumns);
        if ($this->config->has('service.resources_help_url')) {
            $this->setHelp('For more information on managing resources, see: <info>' . $this->config->getStr('service.resources_help_url') . '</info>');
        }
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $selection = $this->selector->getSelection($input);
        if (!$this->api->supportsSizingApi($selection->getProject())) {
            $this->resourcesUtil->writeSizingApiDisabledError($selection->getProject());
            return 1;
        }

        $environment = $selection->getEnvironment();

        try {
            $nextDeployment = $this->resourcesUtil->loadNextDeployment($environment);
        } catch (EnvironmentStateException $e) {
            if ($environment->status === 'inactive') {
                $this->stdErr->writeln(sprintf('The environment %s is not active so resource configuration cannot be read.', $this->api->getEnvironmentLabel($environment, 'comment')));
                return 1;
            }
            throw $e;
        }

        $services = $this->resourcesUtil->allServices($nextDeployment);
        if (empty($services)) {
            $this->stdErr->writeln('No apps or services found');
            return 1;
        }

        $services = $this->resourcesUtil->filterServices($services, $input);
        if (empty($services)) {
            return 1;
        }

        $autoscalingEnabled = [];
        // Check autoscaling settings for the environment, as autoscaling prevents changing some resources manually.
        $autoscalingSettings = $this->api->getAutoscalingSettings($environment);
        if ($autoscalingSettings) {
            foreach ($autoscalingSettings->services as $service => $serviceSettings) {
                $autoscalingEnabled[$service] = !empty($serviceSettings['enabled']);
            }
        }

        if (!$this->table->formatIsMachineReadable()) {
            $this->stdErr->writeln(sprintf('Resource configuration for the project %s, environment %s:', $this->api->getProjectLabel($selection->getProject()), $this->api->getEnvironmentLabel($environment)));
        }

        $empty = $this->table->formatIsMachineReadable() ? '' : '<comment>not set</comment>';
        $notApplicable = $this->table->formatIsMachineReadable() ? '' : 'N/A';

        $containerProfiles = $this->sortContainerProfiles($this->resourcesUtil->containerProfiles($nextDeployment));

        $rows = [];
        $cpuTypeOption = Option::stringOrNull($input, 'cpu-type');
        $autoscalingIndicator = '<comment>(A)</comment>';
        $hasAutoscalingIndicator = false;
        $hasObjectStorage = false;
        foreach ($services as $name => $service) {
            $properties = $service->getProperties();
            $resources = $this->resourcesUtil->resources($service);
            $containerProfile = $this->resourcesUtil->containerProfile($service);
            if (!$this->table->formatIsMachineReadable() && !empty($autoscalingEnabled[$name])) {
                $name .= ' ' . $autoscalingIndicator;
                $hasAutoscalingIndicator = true;
            }
            $row = [
                'service' => $name,
                // The deployment exposes task sizing only (no service type).
                'type' => $service instanceof Task ? $empty : $this->propertyFormatter->format($service->type, 'service_type'),
                'profile' => $containerProfile ?: $empty,
                'profile_size' => $empty,
                'base_memory' => $empty,
                'memory_ratio' => $empty,
                'disk' => $empty,
                'object_storage' => $empty,
                'instance_count' => $empty,
                'cpu_type' => $empty,
                'cpu' => $empty,
                'memory' => $empty,
            ];

            $profileSize = $resources['profile_size'] ?? null;
            if ($containerProfile !== null && $profileSize !== null && isset($containerProfiles[$containerProfile][$profileSize])) {
                $profileInfo = $containerProfiles[$containerProfile][$profileSize];
                if ($cpuTypeOption != "" && $profileInfo['cpu_type'] != $cpuTypeOption) {
                    continue;
                }

                $row['cpu_type'] = $profileInfo['cpu_type'];
                $row['cpu'] = $this->resourcesUtil->formatCPU($profileInfo['cpu']);
                $row['memory'] = $profileInfo['memory'];
            }

            if (isset($properties['resources']) && is_array($properties['resources'])) {
                foreach (['profile_size', 'base_memory', 'memory_ratio'] as $key) {
                    if (array_key_exists($key, $properties['resources'])) {
                        $row[$key] = $resources[$key] ?? '';
                    }
                }
            }

            if (!$this->resourcesUtil->supportsDisk($service)) {
                $row['disk'] = $notApplicable;
            } elseif (array_key_exists('disk', $properties)) {
                $disk = $this->resourcesUtil->disk($service);
                if (empty($disk)) {
                    $row['disk'] = empty($properties['resources']) ? $empty : '';
                } else {
                    $row['disk'] = $this->propertyFormatter->format($disk, 'disk');
                }
            }

            // Object storage is only available on apps.
            if (!$service instanceof WebApp) {
                $row['object_storage'] = $notApplicable;
            } elseif (isset($resources['object_storage'])) {
                $row['object_storage'] = (string) $resources['object_storage'];
                if ($resources['object_storage'] > 0) {
                    $hasObjectStorage = true;
                }
            }

            // Tasks are run-to-completion containers and have no instance count.
            if ($service instanceof Task) {
                $row['instance_count'] = $notApplicable;
            } else {
                $instanceCount = $this->resourcesUtil->instanceCount($service);
                $row['instance_count'] = $instanceCount !== null ? $this->propertyFormatter->format($instanceCount, 'instance_count') : '1';
            }

            $rows[] = $row;
        }

        $defaultColumns = $this->defaultColumns;
        if (!$hasObjectStorage) {
            $defaultColumns = array_values(array_diff($defaultColumns, ['object_storage']));
        }
        $this->table->render($rows, $this->tableHeader, $defaultColumns);

        if (!$this->table->formatIsMachineReadable()) {
            if ($hasAutoscalingIndicator) {
                $this->stdErr->writeln($autoscalingIndicator . ' - Indicates that the service has autoscaling enabled');
            }

            $executable = $this->config->getStr('application.executable');

            $isOriginalCommand = $input instanceof ArgvInput;
            if ($isOriginalCommand) {
                $this->stdErr->writeln('');
                $this->stdErr->writeln(sprintf('Configure resources by running: <info>%s resources:set</info>', $executable));
            }
        }

        return 0;
    }
}
