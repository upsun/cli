<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model;

use Platformsh\Cli\Exception\InvalidConfigException;
use Platformsh\Client\Model\Deployment\WebApp;

/**
 * A class to help reading and normalizing an application's configuration.
 */
final class AppConfig
{
    /** @var array<string, mixed> */
    private array $normalizedConfig;

    /**
     * AppConfig constructor.
     *
     * @param array<string, mixed> $config
     */
    public function __construct(private readonly array $config)
    {
        $this->normalizedConfig = $this->normalizeConfig($this->config);
    }

    /**
     * @param WebApp $app
     *
     * @return self
     */
    public static function fromWebApp(WebApp $app): self
    {
        return new self($app->getProperties());
    }

    /**
     * Get normalized configuration.
     *
     * @return array<string, mixed>
     */
    public function getNormalized(): array
    {
        return $this->normalizedConfig;
    }

    /**
     * Get the (normalized) document root as a relative path.
     *
     * @return string
     */
    public function getDocumentRoot(): string
    {
        $documentRoot = '';
        $normalized = $this->getNormalized();
        if (empty($normalized['web']['locations'])) {
            return $documentRoot;
        }
        foreach ($this->getNormalized()['web']['locations'] as $path => $location) {
            if (isset($location['root'])) {
                $documentRoot = $location['root'];
            }
            if ($path === '/') {
                break;
            }
        }

        return ltrim((string) $documentRoot, '/');
    }

    /**
     * Returns the PHP runtime configuration, or null if the app does not use PHP.
     *
     * This is the `runtime` section for a PHP type, or the PHP entry in
     * `stack.runtimes` for a composable image.
     *
     * @return array<string, mixed>|null
     */
    public function getPhpRuntime(): ?array
    {
        $config = $this->getNormalized();
        $type = is_string($config['type'] ?? null) ? $config['type'] : '';
        if ($type === 'php' || str_starts_with($type, 'php:')) {
            return $this->toConfigArray($config['runtime'] ?? null);
        }
        if (!str_starts_with($type, 'composable:') || !is_array($config['stack'] ?? null)) {
            return null;
        }

        // The stack is a list of runtimes and packages, or a mapping of "runtimes" and "packages".
        // Runtimes are a name, a mapping of a name to its config, or a list of either.
        $stack = $config['stack'];
        $runtimes = array_is_list($stack) ? $stack : ($stack['runtimes'] ?? []);
        if (!is_array($runtimes) || !array_is_list($runtimes)) {
            $runtimes = [$runtimes];
        }
        foreach ($runtimes as $runtime) {
            if (is_string($runtime)) {
                $runtime = [$runtime => null];
            }
            if (!is_array($runtime)) {
                continue;
            }
            foreach ($runtime as $name => $runtimeConfig) {
                if ($name === 'php' || str_starts_with((string) $name, 'php@')) {
                    return $this->toConfigArray($runtimeConfig);
                }
            }
        }

        return null;
    }

    /**
     * @return array<string, mixed>
     */
    private function toConfigArray(mixed $value): array
    {
        if (!is_array($value)) {
            return [];
        }
        $result = [];
        foreach ($value as $k => $v) {
            $result[(string) $k] = $v;
        }

        return $result;
    }

    /**
     * Normalize the application config.
     *
     * @param array<string, mixed> $config
     *
     * @return array<string, mixed>
     */
    private function normalizeConfig(array $config): array
    {
        // Backwards compatibility with old config format: `toolstack` is
        // changed to application `type` and `build`.`flavor`.
        if (isset($config['toolstack'])) {
            if (!strpos((string) $config['toolstack'], ':')) {
                throw new InvalidConfigException("Invalid value for 'toolstack'");
            }
            [$config['type'], $config['build']['flavor']] = explode(':', (string) $config['toolstack'], 2);
        }

        // The `web` section has changed to `web`.`locations`.
        if (isset($config['web']['document_root']) && empty($config['web']['locations'])) {
            $oldConfig = $config['web'] + $this->getOldWebDefaults();

            $location = &$config['web']['locations']['/'];

            $location['root'] = $oldConfig['document_root'];
            $location['expires'] = $oldConfig['expires'];
            $location['passthru'] = $oldConfig['passthru'];
            $location['allow'] = true;

            foreach ($oldConfig['whitelist'] as $pattern) {
                $location['allow'] = false;
                $location['rules'][$pattern]['allow'] = true;
            }

            foreach ($oldConfig['blacklist'] as $pattern) {
                $location['rules'][$pattern]['allow'] = false;
            }
        }

        return $config;
    }

    /**
     * @return array<string, mixed>
     */
    private function getOldWebDefaults(): array
    {
        return [
            'document_root' => '/public',
            'expires' => 0,
            'passthru' => '/index.php',
            'blacklist' => [],
            'whitelist' => [
                // CSS and Javascript.
                '\.css$',
                '\.js$',

                // image/* types.
                '\.gif$',
                '\.jpe?g$',
                '\.png$',
                '\.tiff?$',
                '\.wbmp$',
                '\.ico$',
                '\.jng$',
                '\.bmp$',
                '\.svgz?$',

                // audio/* types.
                '\.midi?$',
                '\.mpe?ga$',
                '\.mp2$',
                '\.mp3$',
                '\.m4a$',
                '\.ra$',
                '\.weba$',

                // video/* types.
                '\.3gpp?$',
                '\.mp4$',
                '\.mpe?g$',
                '\.mpe$',
                '\.ogv$',
                '\.mov$',
                '\.webm$',
                '\.flv$',
                '\.mng$',
                '\.asx$',
                '\.asf$',
                '\.wmv$',
                '\.avi$',

                // application/ogg.
                '\.ogx$',

                // application/x-shockwave-flash.
                '\.swf$',

                // application/java-archive.
                '\.jar$',

                // fonts types.
                '\.ttf$',
                '\.eot$',
                '\.woff$',
                '\.otf$',

                // robots.txt.
                '/robots\.txt$',
            ],
        ];
    }
}
