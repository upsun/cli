<?php

declare(strict_types=1);

namespace Platformsh\Cli;

use Platformsh\Cli\Service\Config;
use Psr\Http\Message\RequestInterface;

/**
 * Guzzle middleware that adds an X-CLI-Event header to requests.
 *
 * The event name is typically the command name (e.g., "backup:restore") and
 * is used for analytics tracking via Pendo.
 */
class EventHeaderMiddleware
{
    public function __construct(private readonly Config $config) {}

    public function __invoke(callable $next): callable
    {
        return function (RequestInterface $request, array $options) use ($next) {
            if ($this->isTelemetryDisabled()) {
                return $next($request, $options);
            }
            $eventName = $this->config->getEventName();
            if ($eventName !== null) {
                $interactive = $this->config->isInteractive() ? 'true' : 'false';
                $headerValue = sprintf('command=%s; interactive=%s', $eventName, $interactive);
                $request = $request->withHeader('X-CLI-Event', $headerValue);
            }
            return $next($request, $options);
        };
    }

    /**
     * Checks if telemetry is disabled via DO_NOT_TRACK or {PREFIX}DISABLE_TELEMETRY.
     */
    private function isTelemetryDisabled(): bool
    {
        $dnt = getenv('DO_NOT_TRACK');
        if ($dnt !== false && filter_var($dnt, FILTER_VALIDATE_BOOLEAN)) {
            return true;
        }
        $prefix = $this->config->getStr('application.env_prefix');
        return getenv($prefix . 'DISABLE_TELEMETRY') === '1';
    }
}
