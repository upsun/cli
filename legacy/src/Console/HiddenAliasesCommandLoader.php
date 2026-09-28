<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\CommandLoader\CommandLoaderInterface;

/**
 * Leaves hidden aliases out of the command names.
 *
 * This keeps them out of abbreviations and suggestions: they work only when
 * given in full.
 *
 * @see \Platformsh\Cli\Application::find()
 * @see HiddenAliasesPass
 */
final readonly class HiddenAliasesCommandLoader implements CommandLoaderInterface
{
    /**
     * @param string[] $hiddenAliases
     */
    public function __construct(private CommandLoaderInterface $loader, private array $hiddenAliases) {}

    public function get(string $name): Command
    {
        return $this->loader->get($name);
    }

    public function has(string $name): bool
    {
        return $this->loader->has($name);
    }

    public function getNames(): array
    {
        return array_values(array_diff($this->loader->getNames(), $this->hiddenAliases));
    }
}
