<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use GuzzleHttp\Exception\BadResponseException;
use GuzzleHttp\Exception\TransferException;
use GuzzleHttp\Psr7\Utils;
use Platformsh\Cli\Exception\HttpException;
use Psr\Http\Message\ResponseInterface;

/**
 * Describes API errors for users, without the details of the HTTP client.
 */
final class ApiErrorMessage
{
    /**
     * Returns a message for an API error, or null if the error is not from the API.
     */
    public static function fromThrowable(\Throwable $e): ?string
    {
        if ($e instanceof HttpException) {
            return $e->getMessage();
        }
        if ($e instanceof BadResponseException) {
            $response = $e->getResponse();
            $request = $e->getRequest();
            $message = sprintf(
                'The API returned %d %s for %s %s',
                $response->getStatusCode(),
                $response->getReasonPhrase(),
                $request->getMethod(),
                Utils::redactUserInfo($request->getUri()),
            );
            $detail = self::detail($response);

            return $detail === '' ? $message : $message . "\n" . $detail;
        }
        if ($e instanceof TransferException) {
            return $e->getMessage();
        }

        return null;
    }

    /**
     * Returns the API's description of an error response, if it adds to the status.
     */
    public static function detail(ResponseInterface $response): string
    {
        $data = json_decode((string) $response->getBody(), true);
        if (!\is_array($data)) {
            return '';
        }
        // A detail per property, e.g. for validation errors.
        if (!empty($data['detail']) && \is_array($data['detail'])) {
            $lines = [];
            foreach ($data['detail'] as $key => $value) {
                $value = \is_scalar($value) ? (string) $value : (string) json_encode($value);
                $lines[] = \is_string($key) ? $key . ': ' . $value : $value;
            }

            return implode("\n", $lines);
        }
        foreach (['detail', 'message', 'title', 'error_description', 'error'] as $key) {
            if (!empty($data[$key]) && \is_string($data[$key])
                && strcasecmp($data[$key], $response->getReasonPhrase()) !== 0) {
                return $data[$key];
            }
        }

        return '';
    }
}
