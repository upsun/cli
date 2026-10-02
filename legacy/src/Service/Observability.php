<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use GuzzleHttp\Exception\BadResponseException;
use GuzzleHttp\Exception\RequestException;
use GuzzleHttp\Psr7\Request;
use Platformsh\Client\Exception\ApiResponseException;
use Platformsh\Client\Model\Environment;

/**
 * Client for the Observability Pipeline API.
 */
readonly class Observability
{
    public function __construct(private Api $api) {}

    /**
     * Fetches the observability entrypoint for an environment.
     *
     * @return array<mixed>|null The entrypoint data, or null if it is not available.
     */
    public function getEntrypoint(Environment $environment): ?array
    {
        $url = rtrim($environment->getUri(), '/') . '/observability/';
        try {
            $data = $this->get($url);
        } catch (RequestException $e) {
            if ($e->getResponse()?->getStatusCode() === 404) {
                return null;
            }
            throw $e;
        }

        return $data;
    }

    /**
     * Returns a link from the entrypoint, or null if it is not available.
     *
     * @param array<mixed>|null $entrypoint
     */
    public static function getLink(?array $entrypoint, string $name): ?string
    {
        $link = self::nested($entrypoint, '_links', $name);
        $href = is_array($link) ? $link['href'] ?? null : null;

        return is_string($href) && $href !== '' ? $href : null;
    }

    /**
     * Returns a nested value from decoded JSON data, or null if it is not found.
     */
    public static function nested(mixed $data, string ...$keys): mixed
    {
        foreach ($keys as $key) {
            if (!is_array($data) || !isset($data[$key])) {
                return null;
            }
            $data = $data[$key];
        }

        return $data;
    }

    /**
     * Sends a GET request and decodes the JSON response.
     *
     * @return array<mixed>
     */
    public function get(string $url): array
    {
        $request = new Request('GET', $url);
        try {
            $response = $this->api->getHttpClient()->send($request);
        } catch (BadResponseException $e) {
            throw ApiResponseException::create($request, $e->getResponse(), $e);
        }
        $data = json_decode((string) $response->getBody(), true);
        if (!is_array($data)) {
            throw new \RuntimeException('Failed to decode observability API response from: ' . $url);
        }

        return $data;
    }
}
