<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model;

/**
 * Autoscaling settings, typed from the API's data.
 *
 * Values that are missing or of an unexpected type are left out, so a partial update's settings can be read safely.
 *
 * @phpstan-type Condition array{threshold?: float, duration?: int}
 * @phpstan-type Trigger array{enabled?: bool, up?: Condition, down?: Condition}
 * @phpstan-type ServiceSettings array{
 *     enabled?: bool,
 *     triggers: array<string, Trigger>,
 *     instances: array{min?: int, max?: int},
 *     scale_cooldown: array{up?: int, down?: int},
 * }
 */
final class AutoscalingSettings
{
    /**
     * @param ServiceSettings|null $defaults
     * @param array<string, ServiceSettings> $services
     */
    private function __construct(public readonly ?array $defaults, public readonly array $services) {}

    /**
     * @param array<mixed> $data
     */
    public static function fromData(array $data): self
    {
        $services = [];
        if (isset($data['services']) && is_array($data['services'])) {
            foreach ($data['services'] as $name => $settings) {
                $services[(string) $name] = self::serviceSettings($settings);
            }
        }
        $defaults = isset($data['defaults']) && is_array($data['defaults']) ? self::serviceSettings($data['defaults']) : null;

        return new self($defaults, $services);
    }

    /**
     * @return ServiceSettings
     */
    private static function serviceSettings(mixed $data): array
    {
        $data = is_array($data) ? $data : [];
        $settings = [
            'triggers' => [],
            'instances' => [],
            'scale_cooldown' => [],
        ];
        if (isset($data['enabled']) && is_bool($data['enabled'])) {
            $settings['enabled'] = $data['enabled'];
        }
        if (isset($data['triggers']) && is_array($data['triggers'])) {
            foreach ($data['triggers'] as $metric => $trigger) {
                $settings['triggers'][(string) $metric] = self::trigger($trigger);
            }
        }
        $instances = isset($data['instances']) && is_array($data['instances']) ? $data['instances'] : [];
        if (isset($instances['min']) && is_int($instances['min'])) {
            $settings['instances']['min'] = $instances['min'];
        }
        if (isset($instances['max']) && is_int($instances['max'])) {
            $settings['instances']['max'] = $instances['max'];
        }
        $cooldown = isset($data['scale_cooldown']) && is_array($data['scale_cooldown']) ? $data['scale_cooldown'] : [];
        if (isset($cooldown['up']) && is_int($cooldown['up'])) {
            $settings['scale_cooldown']['up'] = $cooldown['up'];
        }
        if (isset($cooldown['down']) && is_int($cooldown['down'])) {
            $settings['scale_cooldown']['down'] = $cooldown['down'];
        }

        return $settings;
    }

    /**
     * @return Trigger
     */
    private static function trigger(mixed $data): array
    {
        $data = is_array($data) ? $data : [];
        $trigger = [];
        if (isset($data['enabled']) && is_bool($data['enabled'])) {
            $trigger['enabled'] = $data['enabled'];
        }
        if (isset($data['up']) && is_array($data['up'])) {
            $trigger['up'] = self::condition($data['up']);
        }
        if (isset($data['down']) && is_array($data['down'])) {
            $trigger['down'] = self::condition($data['down']);
        }

        return $trigger;
    }

    /**
     * @param array<mixed> $data
     *
     * @return Condition
     */
    private static function condition(array $data): array
    {
        $condition = [];
        if (isset($data['threshold']) && (is_int($data['threshold']) || is_float($data['threshold']))) {
            $condition['threshold'] = (float) $data['threshold'];
        }
        if (isset($data['duration']) && is_int($data['duration'])) {
            $condition['duration'] = $data['duration'];
        }

        return $condition;
    }
}
