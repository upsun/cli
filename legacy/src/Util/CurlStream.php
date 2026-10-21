<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

/**
 * Reads an HTTP response body incrementally with cURL, without blocking.
 */
class CurlStream
{
    private ?\CurlMultiHandle $multi;
    private ?\CurlHandle $handle;
    private string $buffer = '';
    private int $status = 0;
    private bool $headersReceived = false;
    private bool $done = false;
    private string $error = '';

    /**
     * Starts a GET request.
     *
     * @param array<int, mixed> $options cURL options (CURLOPT_* constants)
     */
    public function __construct(string $url, array $options = [])
    {
        $this->handle = \curl_init($url);
        \curl_setopt_array($this->handle, [
            CURLOPT_SUPPRESS_CONNECT_HEADERS => true,
            CURLOPT_HEADERFUNCTION => $this->onHeader(...),
            CURLOPT_WRITEFUNCTION => $this->onWrite(...),
        ] + $options);
        $this->multi = \curl_multi_init();
        \curl_multi_add_handle($this->multi, $this->handle);
        $this->perform();
    }

    public function __destruct()
    {
        $this->close();
    }

    /**
     * Waits until the response headers have been received.
     *
     * @return bool True if a successful (2xx) response was received, false on error or timeout.
     */
    public function waitForHeaders(float $timeout): bool
    {
        $deadline = \microtime(true) + $timeout;
        while (!$this->headersReceived && !$this->done) {
            $remaining = $deadline - \microtime(true);
            if ($remaining <= 0) {
                $this->error = 'Timed out waiting for a response';
                return false;
            }
            $this->wait(\min($remaining, 1.0));
            $this->perform();
        }
        if (!$this->headersReceived) {
            return false;
        }
        if ($this->status < 200 || $this->status >= 300) {
            $this->error = 'HTTP status ' . $this->status;
            return false;
        }

        return true;
    }

    /**
     * Returns the body data received so far, waiting up to $timeout seconds for new data.
     */
    public function read(float $timeout): string
    {
        $this->perform();
        if ($this->buffer === '' && !$this->done) {
            $this->wait($timeout);
            $this->perform();
        }
        $data = $this->buffer;
        $this->buffer = '';

        return $data;
    }

    /**
     * Returns whether the transfer has finished and all data has been read.
     */
    public function eof(): bool
    {
        return $this->done && $this->buffer === '';
    }

    /**
     * Returns the reason for a failure, if any.
     */
    public function getError(): string
    {
        return $this->error;
    }

    public function close(): void
    {
        if ($this->multi !== null && $this->handle !== null) {
            \curl_multi_remove_handle($this->multi, $this->handle);
        }
        // Releasing the handle breaks the reference cycle via its callbacks.
        $this->handle = null;
        $this->multi = null;
        $this->done = true;
    }

    private function wait(float $timeout): void
    {
        if ($this->multi === null || \curl_multi_select($this->multi, $timeout) === -1) {
            \usleep((int) ($timeout * 1000000));
        }
    }

    private function perform(): void
    {
        if ($this->done || $this->multi === null) {
            return;
        }
        do {
            $code = \curl_multi_exec($this->multi, $running);
        } while ($code === CURLM_CALL_MULTI_PERFORM);
        if ($code !== CURLM_OK) {
            $this->error = (string) \curl_multi_strerror($code);
            $this->done = true;
            return;
        }
        while (($info = \curl_multi_info_read($this->multi)) !== false) {
            if ($info['msg'] === CURLMSG_DONE) {
                $this->done = true;
                $result = $info['result'];
                if (\is_int($result) && $result !== CURLE_OK) {
                    $this->error = (string) \curl_strerror($result);
                }
            }
        }
        if (!$running) {
            $this->done = true;
        }
    }

    private function onHeader(\CurlHandle $handle, string $line): int
    {
        if (\preg_match('#^HTTP/\S+\s+(\d{3})#', $line, $matches)) {
            $this->status = (int) $matches[1];
        } elseif (\trim($line) === '' && $this->status >= 200) {
            // The end of the final (non-informational) response's headers.
            $this->headersReceived = true;
        }

        return \strlen($line);
    }

    private function onWrite(\CurlHandle $handle, string $data): int
    {
        $this->buffer .= $data;

        return \strlen($data);
    }
}
