<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Input\InputInterface;

/**
 * Typed accessors for input options and arguments.
 *
 * A value of the wrong type indicates a bug (e.g. a bad default or a
 * sub-command called with the wrong input), so it throws a LogicException.
 */
final class InputUtil
{
    public static function getStringOption(InputInterface $input, string $name): string
    {
        return self::string($input->getOption($name), '--' . $name);
    }

    public static function getNullableStringOption(InputInterface $input, string $name): ?string
    {
        return self::nullableString($input->getOption($name), '--' . $name);
    }

    public static function getStringArgument(InputInterface $input, string $name): string
    {
        return self::string($input->getArgument($name), $name);
    }

    public static function getNullableStringArgument(InputInterface $input, string $name): ?string
    {
        return self::nullableString($input->getArgument($name), $name);
    }

    /**
     * @return string[]
     */
    public static function getStringArrayOption(InputInterface $input, string $name): array
    {
        return self::stringArray($input->getOption($name), '--' . $name);
    }

    /**
     * @return string[]
     */
    public static function getStringArrayArgument(InputInterface $input, string $name): array
    {
        return self::stringArray($input->getArgument($name), $name);
    }

    /**
     * Gets the value of a non-negative integer option.
     *
     * @throws InvalidArgumentException if the value is not a non-negative integer
     */
    public static function getIntOption(InputInterface $input, string $name): int
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

    private static function string(mixed $value, string $label): string
    {
        if (!is_string($value)) {
            throw new \LogicException(sprintf('Expected a string value for %s, got %s.', $label, get_debug_type($value)));
        }

        return $value;
    }

    /**
     * @return string[]
     */
    private static function stringArray(mixed $value, string $label): array
    {
        if (!is_array($value)) {
            throw new \LogicException(sprintf('Expected an array value for %s, got %s.', $label, get_debug_type($value)));
        }
        $strings = [];
        foreach ($value as $key => $item) {
            if (!is_string($item)) {
                throw new \LogicException(sprintf('Expected only string values for %s, got %s.', $label, get_debug_type($item)));
            }
            $strings[$key] = $item;
        }

        return $strings;
    }

    private static function nullableString(mixed $value, string $label): ?string
    {
        if ($value !== null && !is_string($value)) {
            throw new \LogicException(sprintf('Expected a string or null value for %s, got %s.', $label, get_debug_type($value)));
        }

        return $value;
    }
}
