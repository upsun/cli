<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

/**
 * Reads API tokens and access tokens set via config.
 *
 * Tokens saved by auth:api-token-login are stored by the Go wrapper.
 */
readonly class TokenConfig
{
    private Config $config;

    public function __construct(?Config $config = null)
    {
        $this->config = $config ?: new Config();
    }

    public function getApiToken(): ?string
    {
        $token = (string) $this->config->getWithDefault('api.token', '');
        if ($token !== '') {
            return $token;
        }

        $file = (string) $this->config->getWithDefault('api.token_file', '');
        if ($file !== '') {
            return $this->loadTokenFromFile($file);
        }

        return null;
    }

    public function getAccessToken(): ?string
    {
        return $this->config->getWithDefault('api.access_token', null);
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
