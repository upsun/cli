<?php

declare(strict_types=1);

namespace Platformsh\Cli;

use GuzzleHttp\Client;
use Platformsh\Client\Connection\Connector as BaseConnector;
use Platformsh\Client\Session\SessionInterface;
use Platformsh\OAuth2\Client\Provider\Platformsh;
use Psr\Http\Message\RequestInterface;

/**
 * Adds a "curl" option to the API client library's Connector.
 *
 * The curl options apply to every request, including those for OAuth 2.0 tokens, which the library makes with a
 * separate HTTP client.
 */
class Connector extends BaseConnector
{
    /** @var array<mixed> */
    private readonly array $curlOptions;

    /**
     * @param array<string, mixed> $config
     *   The library's options, plus "curl" (array<int, mixed>): curl options.
     */
    public function __construct(array $config = [], ?SessionInterface $session = null)
    {
        $curlOptions = \is_array($config['curl'] ?? null) ? $config['curl'] : [];
        unset($config['curl']);
        $this->curlOptions = $curlOptions;
        if ($curlOptions) {
            $middlewares = \is_array($config['middlewares'] ?? null) ? $config['middlewares'] : [];
            $middlewares[] = fn(callable $handler): \Closure => function (RequestInterface $request, array $options) use ($handler, $curlOptions) {
                $options['curl'] = (\is_array($options['curl'] ?? null) ? $options['curl'] : []) + $curlOptions;
                return $handler($request, $options);
            };
            $config['middlewares'] = $middlewares;
        }
        parent::__construct($config, $session);
    }

    public function getOAuth2Provider(): Platformsh
    {
        $provider = parent::getOAuth2Provider();
        if ($this->curlOptions) {
            // The same options as the library's provider passes to its HTTP client.
            $provider->setHttpClient(new Client([
                'debug' => $this->config['debug'],
                'verify' => $this->config['verify'],
                'proxy' => $this->config['proxy'],
                'curl' => $this->curlOptions,
            ]));
        }
        return $provider;
    }
}
