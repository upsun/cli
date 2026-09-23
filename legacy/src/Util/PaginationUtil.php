<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

class PaginationUtil
{
    /**
     * Returns the URL and query parameters for the next page of a collection.
     *
     * The API's next page link contains the pagination cursor, but it does not
     * always repeat the parameters that were sent: the subscriptions endpoint
     * omits a filter written as filter[status][value][]. Guzzle's "query"
     * request option replaces a URL's query string, rather than adding to it,
     * so the two queries have to be merged and passed as the option.
     *
     * @param string|null $nextPageUrl The API's next page link.
     * @param string $url The URL that was requested for the current page.
     * @param array<string, mixed> $query The query that was used for the current page.
     *
     * @return array{0: string, 1: array<string, mixed>}|null
     *     The URL and query for the next page, or null if there is no next page.
     */
    public static function nextPage(?string $nextPageUrl, string $url, array $query): ?array
    {
        if ($nextPageUrl === null || $nextPageUrl === '') {
            return null;
        }

        // Normalize the current query via a round trip, so that keys written in
        // bracket notation (page[size]) match the parsed ones (page => [size]).
        $currentQuery = [];
        parse_str(http_build_query($query), $currentQuery);

        $nextQuery = [];
        parse_str((string) parse_url($nextPageUrl, PHP_URL_QUERY), $nextQuery);

        $merged = self::merge($currentQuery, $nextQuery);

        // Avoid repeating the request that has just been made.
        if ($nextPageUrl === $url && $merged === $currentQuery) {
            return null;
        }

        return [$nextPageUrl, $merged];
    }

    /**
     * Merges two queries recursively, replacing lists rather than merging them by index.
     *
     * @param array<mixed> $base
     * @param array<mixed> $replacement
     *
     * @return array<mixed>
     */
    private static function merge(array $base, array $replacement): array
    {
        foreach ($replacement as $key => $value) {
            if (\is_array($value) && !array_is_list($value) && isset($base[$key]) && \is_array($base[$key])) {
                $value = self::merge($base[$key], $value);
            }
            $base[$key] = $value;
        }
        return $base;
    }
}
