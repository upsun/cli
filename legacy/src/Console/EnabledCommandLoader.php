<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Platformsh\Cli\Service\Config;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\CommandLoader\CommandLoaderInterface;

/**
 * Leaves commands disabled by config out of the command loader.
 *
 * This keeps them and their aliases out of abbreviations and suggestions.
 *
 * @see Config::isCommandEnabled()
 */
final readonly class EnabledCommandLoader implements CommandLoaderInterface
{
    public function __construct(private CommandLoaderInterface $loader, private Config $config) {}

    public function get(string $name): Command
    {
        return $this->loader->get($name);
    }

    public function has(string $name): bool
    {
        return $this->loader->has($name)
            && $this->config->isCommandEnabled($this->loader->get($name)->getName() ?? $name);
    }

    public function getNames(): array
    {
        return array_values(array_filter($this->loader->getNames(), $this->has(...)));
    }
}
