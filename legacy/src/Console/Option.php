<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Input\InputInterface;

/**
 * Typed accessors for input options.
 */
final class Option
{
    public static function string(InputInterface $input, string $name): string
    {
        return InputValue::string($input->getOption($name), '--' . $name);
    }

    public static function stringOrNull(InputInterface $input, string $name): ?string
    {
        return InputValue::stringOrNull($input->getOption($name), '--' . $name);
    }

    /**
     * @return string[]
     */
    public static function stringArray(InputInterface $input, string $name): array
    {
        return InputValue::stringArray($input->getOption($name), '--' . $name);
    }

    /**
     * Gets the value of a flag (VALUE_NONE) option.
     */
    public static function bool(InputInterface $input, string $name): bool
    {
        return InputValue::bool($input->getOption($name), '--' . $name);
    }

    /**
     * Gets the value of an option that takes a boolean value, like the form fields' booleans.
     *
     * @throws InvalidArgumentException if the value is not a boolean
     */
    public static function boolValue(InputInterface $input, string $name): bool
    {
        $value = $input->getOption($name);
        if (is_bool($value)) {
            return $value;
        }
        if (is_string($value) && preg_match('/^(1|true|yes|y)$/i', $value)) {
            return true;
        }
        if (is_string($value) && preg_match('/^(0|false|no|n)$/i', $value)) {
            return false;
        }
        throw new InvalidArgumentException(sprintf('The --%s value must be true or false (or 1 or 0).', $name));
    }

    /**
     * Gets the value of a non-negative integer option.
     *
     * @throws InvalidArgumentException if the value is not a non-negative integer
     */
    public static function int(InputInterface $input, string $name): int
    {
        $value = $input->getOption($name);
        if (is_int($value) && $value >= 0) {
            return $value;
        }
        if (!is_string($value) || !preg_match('/^[0-9]+$/', $value)) {
            throw new InvalidArgumentException(sprintf('The --%s value must be a non-negative integer.', $name));
        }

        return (int) $value;
    }

    /**
     * Gets the value of a non-negative integer option, or null if it is not set.
     *
     * @throws InvalidArgumentException if the value is not a non-negative integer
     */
    public static function intOrNull(InputInterface $input, string $name): ?int
    {
        return $input->getOption($name) === null ? null : self::int($input, $name);
    }
}
