<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\Console\ArrayArgument;
use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Console\ProgressMessage;
use Platformsh\Cli\Util\StringUtil;
use Platformsh\Cli\Util\Wildcard;
use Platformsh\Client\Exception\EnvironmentStateException;
use Platformsh\Client\Model\Deployment\EnvironmentDeployment;
use Platformsh\Client\Model\Deployment\Service;
use Platformsh\Client\Model\Deployment\Task;
use Platformsh\Client\Model\Deployment\WebApp;
use Platformsh\Client\Model\Deployment\Worker;
use Platformsh\Client\Model\Environment;
use Platformsh\Client\Model\Project;
use Platformsh\Client\Model\Project\Settings;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * @phpstan-type Sizing array{cpu?: float, memory?: int, disk?: int, profile_size?: string}
 * @phpstan-type ContainerResources array{
 *     profile_size?: string,
 *     base_memory?: int,
 *     memory_ratio?: int,
 *     minimum: Sizing,
 *     default: Sizing,
 *     object_storage?: int,
 * }
 * @phpstan-type ProfileSize array{cpu: float, memory: int, cpu_type: string}
 * @phpstan-type ContainerProfiles array<string, array<int|string, ProfileSize>>
 */
class ResourcesUtil
{
    private readonly OutputInterface $stdErr;

    /** @var array<string, EnvironmentDeployment> */
    private static array $cachedNextDeployment = [];

    public function __construct(private readonly Api $api, private readonly Config $config, OutputInterface $output)
    {
        $this->stdErr = $output instanceof ConsoleOutputInterface ? $output->getErrorOutput() : $output;
    }

    public function featureEnabled(): bool
    {
        return $this->config->getBool('api.sizing');
    }

    /**
     * Prints an error explaining that the project does not support flexible resources.
     */
    public function writeSizingApiDisabledError(Project $project): void
    {
        $this->stdErr->writeln(sprintf('The flexible resources API is not enabled for the project %s.', $this->api->getProjectLabel($project, 'comment')));
        if ($this->config->has('service.fixed_docs_url') && $this->isFixedProject($project)) {
            $this->stdErr->writeln('Flexible resources are not available for Fixed organizations. See: <info>' . $this->config->getStr('service.fixed_docs_url') . '</info>');
        }
    }

    /**
     * Checks if a project belongs to a Fixed organization, returning false if unknown.
     */
    private function isFixedProject(Project $project): bool
    {
        $orgId = $project->getProperty('organization', false, false);
        if (!is_string($orgId) || $orgId === '' || !$this->config->getBool('api.organizations')) {
            return false;
        }
        try {
            $organization = $this->api->getOrganizationById($orgId);
        } catch (\Exception) {
            // The user may not have access to the organization.
            return false;
        }
        return $organization && $organization->getProperty('type', false) === 'fixed';
    }

    /**
     * Lists services in a deployment.
     *
     * @param EnvironmentDeployment $deployment
     *
     * @return array<string, WebApp|Worker|Service|Task>
     *     An array of services keyed by the service name.
     */
    public function allServices(EnvironmentDeployment $deployment): array
    {
        $webapps = $deployment->webapps;
        $workers = $deployment->workers;
        $services = $deployment->services;
        $tasks = !empty($deployment->getData()['tasks']) ? $deployment->tasks : [];
        ksort($webapps, SORT_STRING | SORT_FLAG_CASE);
        ksort($workers, SORT_STRING | SORT_FLAG_CASE);
        ksort($services, SORT_STRING | SORT_FLAG_CASE);
        ksort($tasks, SORT_STRING | SORT_FLAG_CASE);
        $all = [];
        foreach ([$webapps, $workers, $services, $tasks] as $containers) {
            foreach ($containers as $name => $container) {
                $all[(string) $name] = $container;
            }
        }
        return $all;
    }

    /**
     * Checks whether a service can have a persistent disk.
     *
     * This does not mean one is required: a minimum of 0 allows a service to run
     * without a disk.
     */
    public function supportsDisk(WebApp|Worker|Service|Task $service): bool
    {
        // Workers use their parent app's disk; tasks have none.
        if ($service instanceof Worker || $service instanceof Task) {
            return false;
        }
        return isset($this->resources($service)['minimum']['disk']);
    }

    /**
     * Loads the next environment deployment and caches it statically.
     *
     * The static cache means it can be reused while running a sub-command.
     */
    public function loadNextDeployment(Environment $environment, bool $reset = false): EnvironmentDeployment
    {
        $cacheKey = $environment->project . ':' . $environment->id;
        if (isset(self::$cachedNextDeployment[$cacheKey]) && !$reset) {
            return self::$cachedNextDeployment[$cacheKey];
        }
        $progress = new ProgressMessage($this->stdErr);
        try {
            $progress->show('Loading deployment information...');
            $next = $environment->getNextDeployment();
            if (!$next) {
                throw new EnvironmentStateException('No next deployment found', $environment);
            }
        } finally {
            $progress->done();
        }
        return self::$cachedNextDeployment[$cacheKey] = $next;
    }

    /**
     * Filters a list of services according to the --service or --type options.
     *
     * @param array<string, WebApp|Service|Worker|Task> $services
     * @param InputInterface $input
     *
     * @return array<string, WebApp|Service|Worker|Task>|false
     *   False on error, or an array of services.
     */
    public function filterServices(array $services, InputInterface $input): array|false
    {
        $selectedNames = [];

        $requestedServices = ArrayArgument::getOption($input, 'service');
        if (!empty($requestedServices)) {
            $selectedNames = Wildcard::select(array_keys($services), $requestedServices);
            if (!$selectedNames) {
                $this->stdErr->writeln('No services were found matching the name(s): <error>' . implode('</error>, <error>', $requestedServices) . '</error>');
                return false;
            }
            $services = array_intersect_key($services, array_flip($selectedNames));
        }
        $requestedApps = ArrayArgument::getOption($input, 'app');
        if (!empty($requestedApps)) {
            $selectedNames = Wildcard::select(array_keys(array_filter($services, fn($s): bool => $s instanceof WebApp)), $requestedApps);
            if (!$selectedNames) {
                $this->stdErr->writeln('No applications were found matching the name(s): <error>' . implode('</error>, <error>', $requestedApps) . '</error>');
                return false;
            }
            $services = array_intersect_key($services, array_flip($selectedNames));
        }
        $requestedWorkers = ArrayArgument::getOption($input, 'worker');
        if (!empty($requestedWorkers)) {
            $selectedNames = Wildcard::select(array_keys(array_filter($services, fn($s): bool => $s instanceof Worker)), $requestedWorkers);
            if (!$selectedNames) {
                $this->stdErr->writeln('No workers were found matching the name(s): <error>' . implode('</error>, <error>', $requestedWorkers) . '</error>');
                return false;
            }
            $services = array_intersect_key($services, array_flip($selectedNames));
        }
        $requestedTasks = $input->hasOption('task') ? ArrayArgument::getOption($input, 'task') : [];
        if (!empty($requestedTasks)) {
            $selectedNames = Wildcard::select(array_keys(array_filter($services, fn($s): bool => $s instanceof Task)), $requestedTasks);
            if (!$selectedNames) {
                $this->stdErr->writeln('No tasks were found matching the name(s): <error>' . implode('</error>, <error>', $requestedTasks) . '</error>');
                return false;
            }
            $services = array_intersect_key($services, array_flip($selectedNames));
        }

        if ($input->hasOption('type') && ($requestedTypes = ArrayArgument::getOption($input, 'type'))) {
            $byType = [];
            foreach ($services as $name => $service) {
                // Read the type from properties: a service without one (e.g. a
                // task whose deployment has no resolved type) is skipped, not fatal.
                $properties = $service->getProperties();
                if (!isset($properties['type']) || !is_string($properties['type'])) {
                    continue;
                }
                $type = $properties['type'];
                [$prefix] = explode(':', $type, 2);
                $byType[$type][] = $name;
                $byType[$prefix][] = $name;
            }
            $selectedTypes = Wildcard::select(array_keys($byType), $requestedTypes);
            if (!$selectedTypes) {
                $this->stdErr->writeln('No services were found matching the type(s): <error>' . implode('</error>, <error>', $requestedTypes) . '</error>');
                return false;
            }
            foreach ($selectedTypes as $selectedType) {
                $selectedNames = array_merge($selectedNames, $byType[$selectedType]);
            }
            $services = array_intersect_key($services, array_flip($selectedNames));
        }

        return $services;
    }

    /**
     * Returns a container's resources, typed from its deployment properties.
     *
     * Values that are missing or of an unexpected type are left out.
     *
     * @return ContainerResources
     */
    public function resources(WebApp|Worker|Service|Task $service): array
    {
        $data = $service->getProperties()['resources'] ?? null;
        $data = is_array($data) ? $data : [];
        $resources = [
            'minimum' => self::sizing($data['minimum'] ?? null),
            'default' => self::sizing($data['default'] ?? null),
        ];
        if (isset($data['profile_size']) && (is_string($data['profile_size']) || is_int($data['profile_size']) || is_float($data['profile_size']))) {
            $resources['profile_size'] = (string) $data['profile_size'];
        }
        if (isset($data['base_memory']) && is_int($data['base_memory'])) {
            $resources['base_memory'] = $data['base_memory'];
        }
        if (isset($data['memory_ratio']) && is_int($data['memory_ratio'])) {
            $resources['memory_ratio'] = $data['memory_ratio'];
        }
        $disk = isset($data['disk']) && is_array($data['disk']) ? $data['disk'] : [];
        if (isset($disk['object']) && is_int($disk['object'])) {
            $resources['object_storage'] = $disk['object'];
        }

        return $resources;
    }

    /**
     * @return Sizing
     */
    private static function sizing(mixed $data): array
    {
        $data = is_array($data) ? $data : [];
        $sizing = [];
        if (isset($data['cpu']) && (is_int($data['cpu']) || is_float($data['cpu']))) {
            $sizing['cpu'] = (float) $data['cpu'];
        }
        if (isset($data['memory']) && is_int($data['memory'])) {
            $sizing['memory'] = $data['memory'];
        }
        if (isset($data['disk']) && is_int($data['disk'])) {
            $sizing['disk'] = $data['disk'];
        }
        if (isset($data['profile_size']) && (is_string($data['profile_size']) || is_int($data['profile_size']) || is_float($data['profile_size']))) {
            $sizing['profile_size'] = (string) $data['profile_size'];
        }

        return $sizing;
    }

    /**
     * Returns the container profiles of a deployment, typed.
     *
     * Sizes without a CPU, memory and CPU type are left out.
     *
     * @return ContainerProfiles
     */
    public function containerProfiles(EnvironmentDeployment $deployment): array
    {
        $data = $deployment->getProperty('container_profiles', false);
        $profiles = [];
        foreach (is_array($data) ? $data : [] as $profileName => $sizes) {
            $profiles[(string) $profileName] = [];
            foreach (is_array($sizes) ? $sizes : [] as $sizeName => $size) {
                if (is_array($size) && isset($size['cpu'], $size['memory'], $size['cpu_type'])
                    && (is_int($size['cpu']) || is_float($size['cpu']))
                    && is_int($size['memory'])
                    && is_string($size['cpu_type'])) {
                    $profiles[(string) $profileName][$sizeName] = ['cpu' => (float) $size['cpu'], 'memory' => $size['memory'], 'cpu_type' => $size['cpu_type']];
                }
            }
        }

        return $profiles;
    }

    /**
     * Returns a project's build resources.
     *
     * @return array{cpu: float, memory: int}
     */
    public function buildResources(Settings $settings): array
    {
        $current = $settings['build_resources'] ?? null;
        $cpu = is_array($current) ? $current['cpu'] ?? null : null;
        $memory = is_array($current) ? $current['memory'] ?? null : null;
        if (!(is_int($cpu) || is_float($cpu)) || !is_int($memory)) {
            throw new \RuntimeException('The project settings do not include build resources');
        }

        return ['cpu' => (float) $cpu, 'memory' => $memory];
    }

    /**
     * Returns a container's profile name: tasks may lack one, and default to BALANCED.
     */
    public function containerProfile(WebApp|Worker|Service|Task $service): ?string
    {
        return $service->container_profile ?? ($service instanceof Task ? 'BALANCED' : null);
    }

    /**
     * Returns a container's instance count, if it is set.
     */
    public function instanceCount(WebApp|Worker|Service|Task $service): ?int
    {
        $count = $service->getProperties()['instance_count'] ?? null;
        return is_int($count) ? $count : null;
    }

    /**
     * Returns a container's disk size in MB, if it is set.
     */
    public function disk(WebApp|Worker|Service|Task $service): ?int
    {
        $disk = $service->getProperties()['disk'] ?? null;
        return is_int($disk) ? $disk : null;
    }

    /**
     * Formats a change in a value.
     *
     * @param int|float|string|null $previousValue
     * @param int|float|string|null $newValue
     * @param string $suffix A unit suffix e.g. ' MB'
     * @param callable|null $comparator
     *
     * @return string
     */
    public function formatChange(int|float|string|null $previousValue, int|float|string|null $newValue, string $suffix = '', ?callable $comparator = null): string
    {
        if ($previousValue === null || $newValue === $previousValue) {
            return sprintf('<info>%s%s</info>', $newValue, $suffix);
        }
        if ($comparator !== null) {
            $changeText = $comparator($previousValue, $newValue) ? '<fg=green>increasing</>' : '<fg=yellow>decreasing</>';
        } elseif ($newValue === "true" || $newValue === "false") {
            $color = $newValue === "true" ? 'green' : 'yellow';
            $changeText = '<fg=' . $color . '>changing</>';
        } else {
            $changeText = $newValue > $previousValue ? '<fg=green>increasing</>' : '<fg=yellow>decreasing</>';
        }
        return sprintf(
            '%s from %s%s to <info>%s%s</info>',
            $changeText,
            $previousValue,
            $suffix,
            $newValue,
            $suffix
        );
    }

    /**
     * Formats a CPU amount.
     *
     * @param int|float|string $unformatted
     *
     * @return string
     *   A numeric (still comparable) string with 1 decimal place.
     */
    public function formatCPU(int|float|string $unformatted): string
    {
        return sprintf('%.1f', $unformatted);
    }

    /**
     * Adds a --resources-init option to commands that support it.
     *
     * The option will only be added if the api.sizing feature is enabled.
     *
     * @param InputDefinition $definition
     * @param string[] $values
     *   The possible values, with the default as the first element.
     * @param string $description
     *
     * @return self
     *
     * @see ResourcesUtil::validateResourcesInitInput()
     */
    public function addOption(InputDefinition $definition, array $values, string $description = ''): self
    {
        if (!$this->featureEnabled()) {
            return $this;
        }
        if ($description === '') {
            $description = 'Set the resources to use for new services';
            $description .= ': ' . StringUtil::formatItemList($values);
            $default = array_shift($values);
            $description .= ".\n" . sprintf('If not set, "%s" will be used.', $default);
        }
        $definition->addOption(new InputOption('resources-init', null, InputOption::VALUE_REQUIRED, $description));

        return $this;
    }

    /**
     * Validates and returns the --resources-init input, if any.
     *
     * @param InputInterface $input
     * @param Project $project
     * @param string[] $allowedValues
     * @return string|false|null
     *   The input value, or false if there was a validation error, or null if
     *   nothing was specified or the input option didn't exist.
     *
     * @see ResourcesUtil::addResourcesInitOption()
     */
    public function validateInput(InputInterface $input, Project $project, array $allowedValues): false|string|null
    {
        $resourcesInit = $input->hasOption('resources-init') ? Option::stringOrNull($input, 'resources-init') : null;
        if ($resourcesInit !== null) {
            if (!\in_array($resourcesInit, $allowedValues, true)) {
                $this->stdErr->writeln('The value for <error>--resources-init</error> must be one of: ' . \implode(', ', $allowedValues));
                return false;
            }
            if (!$this->api->supportsSizingApi($project)) {
                $this->stdErr->writeln('The <comment>--resources-init</comment> option cannot be used as the project does not support flexible resources.');
                return false;
            }
        }
        return $resourcesInit;
    }
}
