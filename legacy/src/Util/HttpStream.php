<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

use GuzzleHttp\Client;
use GuzzleHttp\Handler\CurlMultiHandler;
use GuzzleHttp\HandlerStack;
use GuzzleHttp\Promise\Is;
use GuzzleHttp\Promise\PromiseInterface;
use GuzzleHttp\Psr7\BufferStream;
use Psr\Http\Message\ResponseInterface;

/**
 * Reads an HTTP response body as it arrives, with Guzzle and curl, waiting for a limited time for new data.
 */
final class HttpStream
{
    private readonly CurlMultiHandler $handler;
    private readonly BufferStream $body;
    private readonly PromiseInterface $promise;
    private bool $started = false;

    /**
     * Starts a GET request.
     *
     * @param array<string, mixed> $options Guzzle request options.
     * @param float $wait How long a read waits for new data, in seconds.
     */
    public function __construct(string $url, array $options, private readonly float $wait)
    {
        $this->handler = new CurlMultiHandler(['select_timeout' => $wait]);
        // The body is drained by each read, so it needs no size limit.
        $this->body = new BufferStream(\PHP_INT_MAX);
        $client = new Client(['handler' => HandlerStack::create($this->handler)]);
        $this->promise = $client->requestAsync('GET', $url, [
            'sink' => $this->body,
            'allow_redirects' => false,
            'on_headers' => function (ResponseInterface $response): void {
                // Only a successful response's body is read.
                if ($response->getStatusCode() < 200 || $response->getStatusCode() >= 300) {
                    throw new \RuntimeException('Unexpected HTTP status: ' . $response->getStatusCode());
                }
                $this->started = true;
            },
        ] + $options);
    }

    /**
     * Waits for a successful response to start.
     *
     * @return bool False if the request failed or timed out (it is then canceled).
     */
    public function start(float $timeout): bool
    {
        $deadline = \microtime(true) + $timeout;
        while (!$this->started && Is::pending($this->promise) && \microtime(true) < $deadline) {
            $this->handler->tick();
        }
        if (!$this->started) {
            $this->promise->cancel();
        }
        return $this->started;
    }

    /**
     * Returns new data from the body, waiting for some for up to the read wait time.
     *
     * @return string An empty string if there is no new data.
     */
    public function read(): string
    {
        $deadline = \microtime(true) + $this->wait;
        while ($this->body->getSize() === 0 && Is::pending($this->promise) && \microtime(true) < $deadline) {
            $this->handler->tick();
        }
        return $this->body->getContents();
    }

    /**
     * Checks whether the response has ended or failed, and all of its data has been read.
     */
    public function eof(): bool
    {
        return $this->body->getSize() === 0 && !Is::pending($this->promise);
    }
}
