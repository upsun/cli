<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\Selector\Selection;
use Platformsh\Cli\Tunnel\Tunnel;
use Platformsh\Cli\Util\PortUtil;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Output\StreamOutput;
use Symfony\Component\Process\Process;

class TunnelManager
{
    public const LOCAL_IP = '127.0.0.1';

    /** @var Tunnel[]|null */
    private ?array $tunnels;

    public function __construct(private readonly Config $config, private readonly Io $io, private readonly Relationships $relationships) {}

    /**
     * Derives a tunnel ID from its metadata.
     *
     * The metadata may come from a state file written by an older version, so
     * the parts are not assumed to be present or to be strings.
     *
     * @param array<string, mixed> $metadata
     */
    private function getId(array $metadata): string
    {
        return implode('--', [
            (string) ($metadata['projectId'] ?? ''),
            (string) ($metadata['environmentId'] ?? ''),
            (string) ($metadata['appName'] ?? ''),
            (string) ($metadata['relationship'] ?? ''),
            (string) ($metadata['serviceKey'] ?? ''),
        ]);
    }

    /**
     * @param array<string, mixed> $service
     *
     * @throws \Exception
     */
    public function create(Selection $selection, array $service, ?int $localPort = null): Tunnel
    {
        $metadata = [
            'projectId' => $selection->getProject()->id,
            'environmentId' => $selection->getEnvironment()->id,
            'appName' => $selection->getAppName(),
            'relationship' => $service['_relationship_name'],
            'serviceKey' => $service['_relationship_key'],
            'service' => $service,
        ];

        // The 'port' value from the API relationship metadata is a string, but
        // Tunnel expects an int, so cast it here.
        return new Tunnel($this->getId($metadata), $localPort ?: $this->getPort(), $service['host'], (int) $service['port'], $metadata);
    }

    /**
     * Automatically determines the best port for a new tunnel.
     * @throws \Exception
     */
    protected function getPort(int $default = 30000): int
    {
        $ports = [];
        foreach ($this->getTunnels() as $tunnel) {
            $ports[] = $tunnel->localPort;
        }

        return PortUtil::getPort($ports ? max($ports) + 1 : $default);
    }

    /**
     * Gets info on currently open tunnels.
     *
     * @return Tunnel[]
     */
    public function getTunnels(bool $open = true): array
    {
        if (!isset($this->tunnels)) {
            $this->tunnels = [];
            // @todo move this to State service (in a new major version)
            $filename = $this->config->getWritableUserDir() . '/tunnel-info.json';
            if (file_exists($filename)) {
                $this->io->debug(sprintf('Loading tunnel info from %s', $filename));
                $this->tunnels = $this->unserialize((string) file_get_contents($filename));
            }
        }

        if ($open) {
            $needsSave = false;
            foreach ($this->tunnels as $key => $tunnel) {
                if ($tunnel->pid === null) {
                    $this->io->debug(sprintf(
                        'No PID found for the tunnel at port %d; removing from list',
                        $tunnel->localPort,
                    ));
                    unset($this->tunnels[$key]);
                    $needsSave = true;
                } elseif (function_exists('posix_kill') && !posix_kill($tunnel->pid, 0)) {
                    $this->io->debug(sprintf(
                        'The tunnel at port %d is no longer open, removing from list',
                        $tunnel->localPort,
                    ));
                    unset($this->tunnels[$key]);
                    $needsSave = true;
                }
            }
            if ($needsSave) {
                $this->saveTunnelInfo();
            }
        }

        return $this->tunnels;
    }

    public function saveNewTunnel(Tunnel $tunnel, int $pid): void
    {
        $tunnel->pid = $pid;
        $this->tunnels[] = $tunnel;
        $this->saveTunnelInfo();
    }

    private function saveTunnelInfo(): void
    {
        $filename = $this->config->getWritableUserDir() . '/tunnel-info.json';
        if (!empty($this->tunnels)) {
            $this->io->debug('Saving tunnel info to: ' . $filename);
            if (!file_put_contents($filename, $this->serialize($this->tunnels))) {
                throw new \RuntimeException('Failed to write tunnel info to: ' . $filename);
            }
        } elseif (file_exists($filename) && !unlink($filename)) {
            throw new \RuntimeException('Failed to delete tunnel info file: ' . $filename);
        }
    }

    /**
     * Checks whether a tunnel is already open.
     *
     * @return false|Tunnel
     *   If the tunnel is open, a new Tunnel object is returned with its PID
     *   set.
     */
    public function isOpen(Tunnel $tunnel): false|Tunnel
    {
        foreach ($this->getTunnels() as $t) {
            if ($t->id === $tunnel->id) {
                if ($t->pid && function_exists('posix_kill') && !posix_kill($t->pid, 0)) {
                    $this->io->debug(sprintf(
                        'The tunnel at port %d is no longer open, removing from list',
                        $t->localPort,
                    ));
                    $this->close($t);
                    return false;
                }
                $tunnel->pid = $t->pid;

                return $tunnel;
            }
        }

        return false;
    }

    /**
     * @param Tunnel[] $tunnels
     * @throws \JsonException
     */
    private function serialize(array $tunnels): string
    {
        $data = [];
        foreach ($tunnels as $tunnel) {
            $data[$tunnel->id] = $tunnel->metadata + [
                'id' => $tunnel->id,
                'localPort' => $tunnel->localPort,
                'remoteHost' => $tunnel->remoteHost,
                'remotePort' => $tunnel->remotePort,
                'pid' => $tunnel->pid,
            ];
        }

        return \json_encode($data, JSON_THROW_ON_ERROR);
    }

    /**
     * Reads a positive integer from state file data, or null if there isn't one.
     *
     * Ports and PIDs are rejected unless they are positive: a PID of 0 would
     * make posix_kill() signal the CLI's whole process group. Non-int, non-string
     * values are rejected before filtering, as true would otherwise pass as 1.
     */
    private function positiveInt(mixed $value): ?int
    {
        if (!is_int($value) && !is_string($value)) {
            return null;
        }
        $int = filter_var($value, FILTER_VALIDATE_INT);

        return is_int($int) && $int > 0 ? $int : null;
    }

    /**
     * @return Tunnel[]
     */
    private function unserialize(string $jsonData): array
    {
        $tunnels = [];
        $data = (array) json_decode($jsonData, true);
        foreach ($data as $item) {
            if (!is_array($item)) {
                $this->io->debug('Ignoring malformed tunnel info entry');
                continue;
            }
            // State files written by older versions can hold the ports as
            // strings, but Tunnel expects ints.
            $localPort = $this->positiveInt($item['localPort'] ?? null);
            $remotePort = $this->positiveInt($item['remotePort'] ?? null);
            if ($localPort === null || $remotePort === null || !isset($item['remoteHost'])) {
                $this->io->debug('Ignoring tunnel info entry with a missing or invalid host or port');
                continue;
            }
            /** @var array<string, mixed> $metadata */
            $metadata = $item;
            unset($metadata['id'], $metadata['localPort'], $metadata['remoteHost'], $metadata['remotePort'], $metadata['pid']);
            // Handle old-format tunnel data that lacks an 'id' field.
            $id = $item['id'] ?? $this->getId($metadata);
            $tunnels[] = new Tunnel(
                (string) $id,
                $localPort,
                (string) $item['remoteHost'],
                $remotePort,
                $metadata,
                $this->positiveInt($item['pid'] ?? null),
            );
        }
        return $tunnels;
    }

    /**
     * Closes an open tunnel.
     */
    public function close(Tunnel $tunnel): void
    {
        if ($tunnel->pid !== null && function_exists('posix_kill')) {
            if (!posix_kill($tunnel->pid, SIGTERM)) {
                throw new \RuntimeException(sprintf(
                    'Failed to kill process %d (POSIX error: %s)',
                    $tunnel->pid,
                    posix_get_last_error(),
                ));
            }
        }
        $pidFile = $this->getPidFilename($tunnel);
        if (file_exists($pidFile) && !unlink($pidFile)) {
            throw new \RuntimeException(sprintf(
                'Failed to delete file: %s',
                $pidFile,
            ));
        }
        $this->forget($tunnel);
    }

    /**
     * Removes a tunnel from the saved state.
     *
     * Pruning in getTunnels() only runs where the posix extension is
     * available, so a closed tunnel has to remove its own entry.
     */
    private function forget(Tunnel $tunnel): void
    {
        $found = false;
        foreach ($this->getTunnels(false) as $key => $t) {
            if ($t->id === $tunnel->id) {
                unset($this->tunnels[$key]);
                $found = true;
            }
        }
        if ($found) {
            $this->saveTunnelInfo();
        }
    }

    public function getPidFilename(Tunnel $tunnel): string
    {
        $dir = $this->config->getWritableUserDir() . '/.tunnels';
        if (!is_dir($dir) && !mkdir($dir, 0o700, true)) {
            throw new \RuntimeException('Failed to create directory: ' . $dir);
        }

        return $dir . '/' . preg_replace('/[^0-9a-z.]+/', '-', $tunnel->id) . '.pid';
    }

    /**
     * @param string[] $extraArgs
     */
    public function createProcess(string $url, Tunnel $tunnel, array $extraArgs = []): Process
    {
        $args = ['ssh', '-n', '-N', '-L', implode(':', [$tunnel->localPort, $tunnel->remoteHost, $tunnel->remotePort]), $url];
        $args = array_merge($args, $extraArgs);
        $process = new Process($args);
        $process->setTimeout(null);

        return $process;
    }

    /**
     * Filters a list of tunnels by the currently selected project/environment.
     *
     * @param Tunnel[] $tunnels
     *
     * @return Tunnel[]
     */
    public function filterBySelection(array $tunnels, Selection $selection): array
    {
        if (!$selection->hasProject()) {
            return $tunnels;
        }
        $project = $selection->getProject();
        $environment = $selection->hasEnvironment() ? $selection->getEnvironment() : null;
        $appName = $selection->hasEnvironment() ? $selection->getAppName() : null;
        foreach ($tunnels as $key => $tunnel) {
            $metadata = $tunnel->metadata;
            if ($metadata['projectId'] !== $project->id
                || ($environment !== null && $metadata['environmentId'] !== $environment->id)
                || ($appName !== null && $metadata['appName'] !== $appName)) {
                unset($tunnels[$key]);
            }
        }

        return $tunnels;
    }

    public function getUrl(Tunnel $tunnel): string
    {
        $localService = array_merge($tunnel->metadata['service'], array_intersect_key([
            'host' => self::LOCAL_IP,
            'port' => $tunnel->localPort,
        ], $tunnel->metadata['service']));

        return $this->relationships->buildUrl($localService);
    }

    /**
     * Formats a tunnel's relationship as a string.
     */
    public function formatRelationship(Tunnel $tunnel): string
    {
        $metadata = $tunnel->metadata;

        return $metadata['serviceKey'] > 0
            ? sprintf('%s.%d', $metadata['relationship'], $metadata['serviceKey'])
            : $metadata['relationship'];
    }

    public function openLog(string $logFile): OutputInterface|false
    {
        $logResource = fopen($logFile, 'a');
        if ($logResource) {
            return new StreamOutput($logResource, OutputInterface::VERBOSITY_VERBOSE);
        }

        return false;
    }
}
