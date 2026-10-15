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
final class EnabledCommandLoader implements CommandLoaderInterface
{
    /** @var array<string, true>|null */
    private ?array $disabled = null;

    public function __construct(private readonly CommandLoaderInterface $loader, private readonly Config $config) {}

    public function get(string $name): Command
    {
        return $this->loader->get($name);
    }

    public function has(string $name): bool
    {
        return $this->loader->has($name) && !isset($this->disabled()[$name]);
    }

    public function getNames(): array
    {
        return array_values(array_filter($this->loader->getNames(), fn(string $name): bool => !isset($this->disabled()[$name])));
    }

    /**
     * Lists the names and aliases of disabled commands, loading only those commands.
     *
     * @return array<string, true>
     */
    private function disabled(): array
    {
        if ($this->disabled === null) {
            $this->disabled = [];
            foreach ($this->loader->getNames() as $name) {
                if ($this->config->isCommandEnabled($name)) {
                    continue;
                }
                $command = $this->loader->get($name);
                if ($command->getName() === $name) {
                    foreach ([$name, ...$command->getAliases()] as $n) {
                        $this->disabled[$n] = true;
                    }
                }
            }
        }

        return $this->disabled;
    }
}
