<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

/**
 * Declares command aliases that work but are hidden from help and lists.
 *
 * @see HiddenAliasesPass
 */
#[\Attribute(\Attribute::TARGET_CLASS)]
final readonly class HiddenAliases
{
    /**
     * @param string[] $aliases
     */
    public function __construct(public array $aliases) {}
}
