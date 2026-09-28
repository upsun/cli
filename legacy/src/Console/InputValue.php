<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

/**
 * Type checks shared by Option and Argument.
 *
 * A value of the wrong type indicates a bug (e.g. a bad default or a
 * sub-command called with the wrong input), so it throws a LogicException.
 *
 * @internal
 */
final class InputValue
{
    public static function string(mixed $value, string $label): string
    {
        if (!is_string($value)) {
            throw new \LogicException(sprintf('Expected a string value for %s, got %s.', $label, get_debug_type($value)));
        }

        return $value;
    }

    public static function bool(mixed $value, string $label): bool
    {
        if (!is_bool($value)) {
            throw new \LogicException(sprintf('Expected a boolean value for %s, got %s.', $label, get_debug_type($value)));
        }

        return $value;
    }

    public static function stringOrNull(mixed $value, string $label): ?string
    {
        if ($value !== null && !is_string($value)) {
            throw new \LogicException(sprintf('Expected a string or null value for %s, got %s.', $label, get_debug_type($value)));
        }

        return $value;
    }

    /**
     * @return string[]
     */
    public static function stringArray(mixed $value, string $label): array
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
}
