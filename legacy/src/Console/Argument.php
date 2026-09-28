<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Input\InputInterface;

/**
 * Typed accessors for input arguments.
 */
final class Argument
{
    public static function string(InputInterface $input, string $name): string
    {
        return InputValue::string($input->getArgument($name), $name);
    }

    public static function stringOrNull(InputInterface $input, string $name): ?string
    {
        return InputValue::stringOrNull($input->getArgument($name), $name);
    }

    /**
     * @return string[]
     */
    public static function stringArray(InputInterface $input, string $name): array
    {
        return InputValue::stringArray($input->getArgument($name), $name);
    }
}
