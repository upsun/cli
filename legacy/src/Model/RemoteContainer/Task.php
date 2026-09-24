<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model\RemoteContainer;

use GuzzleHttp\ClientInterface;
use GuzzleHttp\Utils;
use Platformsh\Cli\Model\AppConfig;
use Platformsh\Client\Model\Environment;

/**
 * Represents a running task container, which allows SSH access.
 *
 * Unlike apps and workers, a task container is ephemeral: it only exists while
 * an `environment.task` activity is in progress.
 */
readonly class Task implements RemoteContainerInterface
{
    private string $containerName;

    public function __construct(private Environment $environment, private string $taskName, string $activityId, private ClientInterface $client)
    {
        // The SSH gateway addresses a task container as
        // "<task-name>--task--<first 8 chars of the activity ID>".
        $this->containerName = $taskName . '--task--' . substr($activityId, 0, 8);
    }

    public function getSshUrl($instance = ''): string
    {
        // A task is a single ephemeral container; it has no instances, so the
        // instance argument (which may be null) is ignored.
        return $this->constructTaskSshUrl($this->referenceSshUrl(), $this->containerName);
    }

    public function getName(): string
    {
        return $this->containerName;
    }

    public function getConfig(): AppConfig
    {
        // The task's configuration (type, run, mounts, relationships, etc.) is
        // exposed at /environments/<id>/tasks/<name>, independently of whether a
        // run is in progress. Fetch it lazily, so callers that only need SSH
        // access (ssh, log, scp) pay nothing.
        $response = $this->client->request('GET', $this->environment->getUri() . '/tasks/' . rawurlencode($this->taskName));
        $data = (array) Utils::jsonDecode((string) $response->getBody(), true);

        return new AppConfig($data);
    }

    public function getRuntimeOperations(): array
    {
        return [];
    }

    /**
     * Returns any of the environment's SSH URLs, to derive the host and user prefix from.
     */
    private function referenceSshUrl(): string
    {
        $urls = $this->environment->getSshUrls();
        if ($urls === []) {
            throw new \RuntimeException(sprintf(
                'Could not determine the SSH endpoint for the environment %s.',
                $this->environment->id,
            ));
        }
        return reset($urls);
    }

    /**
     * Builds the task container's SSH URL from a reference URL.
     *
     * A container SSH URL has the form "<project>-<environment>--<container>@<host>".
     * All containers in an environment share the same "<project>-<environment>"
     * user prefix and host, so the task URL is the reference URL with its
     * container suffix replaced by the task's container name.
     */
    private function constructTaskSshUrl(string $referenceUrl, string $containerName): string
    {
        $atPos = strrpos($referenceUrl, '@');
        if ($atPos === false) {
            throw new \RuntimeException(sprintf('Unexpected SSH URL format: %s', $referenceUrl));
        }
        $user = substr($referenceUrl, 0, $atPos);
        $hostPart = substr($referenceUrl, $atPos + 1);

        // The "--" double-dash separates the shared user prefix from the
        // container name. Project IDs and environment machine names only use
        // single dashes, so the prefix is everything before the first "--".
        $prefix = explode('--', $user, 2)[0];

        return $prefix . '--' . $containerName . '@' . $hostPart;
    }
}
