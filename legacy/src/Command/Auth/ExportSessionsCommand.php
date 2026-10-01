<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Auth;

use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Console\Option;
use Platformsh\Cli\CredentialHelper\Manager;
use Platformsh\Cli\Service\Config;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Filesystem\Filesystem;

/**
 * Exports sessions from the storage used before credentials moved to the Go wrapper.
 *
 * This must not use the API or the Go wrapper's auth commands, as it is run by the wrapper during its migration.
 */
#[AsCommand(name: 'auth:export-sessions', description: 'Export stored sessions for migration (internal)', hidden: true)]
class ExportSessionsCommand extends CommandBase
{
    // The attribute's "hidden" stops abbreviations from matching, and this hides the command once it is loaded.
    protected bool $hiddenInList = true;

    public function __construct(private readonly Config $config)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->addOption('delete', null, InputOption::VALUE_NONE, 'Delete the stored sessions, without revoking them');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        if (Option::bool($input, 'delete')) {
            $this->delete();
            return 0;
        }

        /** @var array<string, array<string, mixed>> $sessions */
        $sessions = [];
        // Values are only set once: the keychain takes precedence over files, as the legacy CLI only used files
        // when the keychain was unavailable.
        $set = function (string $id, string $key, mixed $value) use (&$sessions): void {
            if ($value !== null && $value !== '' && $value !== false && !isset($sessions[$id][$key])) {
                $sessions[$id][$key] = $value;
            }
        };
        $setFromSession = function (string $id, mixed $data) use ($set): void {
            if (!is_array($data)) {
                return;
            }
            $set($id, 'access_token', $data['accessToken'] ?? null);
            $set($id, 'refresh_token', $data['refreshToken'] ?? null);
            $set($id, 'token_type', $data['tokenType'] ?? null);
            $set($id, 'expires', isset($data['expires']) && is_numeric($data['expires']) ? (int) $data['expires'] : null);
        };

        // Sessions and API tokens in the keychain, if the credential helper is already installed.
        $manager = $this->credentialHelper();
        if ($manager !== null) {
            $prefix = $this->config->getStr('application.slug') . '/';
            foreach (array_keys($manager->listAll()) as $url) {
                if (!str_starts_with((string) $url, $prefix)) {
                    continue;
                }
                $path = substr((string) $url, strlen($prefix));
                $secret = $manager->get((string) $url);
                if ($secret === false) {
                    continue;
                }
                if (str_ends_with($path, '/api-token')) {
                    $set(substr($path, 0, -strlen('/api-token')), 'api_token', trim($secret));
                } else {
                    $setFromSession($path, json_decode((string) base64_decode($secret, true), true));
                }
            }
        }

        // Sessions and API tokens in files.
        foreach ($this->sessionFiles() as $id => $file) {
            $setFromSession($id, json_decode((string) file_get_contents($file), true));
        }
        foreach ($this->apiTokenFiles() as $id => $file) {
            $set($id, 'api_token', trim((string) file_get_contents($file)));
        }

        $output->writeln((string) json_encode((object) $sessions, JSON_UNESCAPED_SLASHES));

        return 0;
    }

    /**
     * Deletes the exported copies. SSH certificates and the credential helper itself are kept.
     */
    private function delete(): void
    {
        $manager = $this->credentialHelper();
        if ($manager !== null) {
            $prefix = $this->config->getStr('application.slug') . '/';
            foreach (array_keys($manager->listAll()) as $url) {
                if (str_starts_with((string) $url, $prefix)) {
                    $manager->erase((string) $url);
                }
            }
        }
        $fs = new Filesystem();
        $fs->remove(array_values($this->sessionFiles()));
        $fs->remove(array_values($this->apiTokenFiles()));
        // Remove the session files' directories if they are now empty. For a session ID beginning with "cli-",
        // the directory may also hold another session's SSH certificates.
        foreach (glob($this->config->getSessionDir() . '/sess-*', GLOB_ONLYDIR | GLOB_NOSORT) ?: [] as $dir) {
            if ((scandir($dir) ?: []) === ['.', '..']) {
                $fs->remove($dir);
            }
        }
    }

    private function credentialHelper(): ?Manager
    {
        $manager = new Manager($this->config);

        return $manager->isSupported() && $manager->isInstalled() ? $manager : null;
    }

    /**
     * Finds session files, which are named sess-<id>/sess-<id>.json.
     *
     * @return array<string, string> Files keyed by session ID.
     */
    private function sessionFiles(): array
    {
        $files = [];
        foreach (glob($this->config->getSessionDir() . '/sess-*/sess-*.json', GLOB_NOSORT) ?: [] as $file) {
            $id = substr(basename($file, '.json'), strlen('sess-'));
            if (basename(dirname($file)) === 'sess-' . $id) {
                $files[$id] = $file;
            }
        }

        return $files;
    }

    /**
     * Finds API token files, which are named sess-cli-<id>/api-token.
     *
     * @return array<string, string> Files keyed by session ID.
     */
    private function apiTokenFiles(): array
    {
        $files = [];
        foreach (glob($this->config->getSessionDir() . '/sess-cli-*/api-token', GLOB_NOSORT) ?: [] as $file) {
            $files[substr(basename(dirname($file)), strlen('sess-cli-'))] = $file;
        }

        return $files;
    }
}
