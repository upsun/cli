<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\ApiToken\StorageInterface;
use Platformsh\Cli\ApiToken\Storage;

readonly class TokenConfig
{
    private Config $config;
    private StorageInterface $apiTokenStorage;

    public function __construct(?Config $config = null)
    {
        $this->config = $config ?: new Config();
        $this->apiTokenStorage = Storage::factory($this->config);
    }

    public function storage(): StorageInterface
    {
        return $this->apiTokenStorage;
    }

    /**
     * Returns the API token to use: one set via config, or else one saved by auth:api-token-login.
     */
    public function getApiToken(bool $includeStored = true): ?string
    {
        $token = $this->config->getStrOrNull('api.token') ?? '';
        if ($token !== '') {
            return $token;
        }

        $file = $this->config->getStrOrNull('api.token_file') ?? '';
        if ($file !== '') {
            $token = $this->loadTokenFromFile($file);
            if ($token !== '') {
                return $token;
            }
        }

        if ($includeStored) {
            $storedToken = $this->apiTokenStorage->getToken();
            if ($storedToken !== '') {
                return $storedToken;
            }
        }

        return null;
    }

    public function getAccessToken(): ?string
    {
        return $this->config->getStrOrNull('api.access_token');
    }

    /**
     * Loads an API token from a file.
     *
     * @param string $filename
     *   A filename, either relative to the user config directory, or absolute.
     *
     * @return string
     */
    private function loadTokenFromFile(string $filename): string
    {
        if (!str_starts_with($filename, '/') && !str_starts_with($filename, '\\')) {
            $filename = $this->config->getUserConfigDir() . '/' . $filename;
        }

        $content = file_get_contents($filename);
        if ($content === false) {
            throw new \RuntimeException('Failed to read file: ' . $filename);
        }

        return trim($content);
    }
}
