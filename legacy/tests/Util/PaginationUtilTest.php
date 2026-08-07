<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Util\PaginationUtil;

class PaginationUtilTest extends TestCase
{
    public function testNextPage(): void
    {
        $url = '/organizations/foo/subscriptions';
        $cursor = 'WyIyMDI1LTA3LTE2VDIwOjA1OjUzLjYzMzA2OVoiLCJ0d3J1d3R3aTJod2lpIl0';
        $filter = ['filter' => ['status' => ['value' => ['active', 'suspended'], 'operator' => 'IN']]];

        $cases = [
            // The cursor is added to the query, and the filter is kept, as the
            // subscriptions endpoint omits it from the next page link.
            [
                $url . '?page%5Bafter%5D=' . $cursor . '&page%5Bsize%5D=50',
                $url,
                $filter + ['page[size]' => 50],
                [
                    $url . '?page%5Bafter%5D=' . $cursor . '&page%5Bsize%5D=50',
                    $filter + ['page' => ['size' => '50', 'after' => $cursor]],
                ],
            ],
            // The page size in the next page link takes precedence.
            [
                $url . '?page%5Bafter%5D=' . $cursor . '&page%5Bsize%5D=100',
                $url,
                ['page[size]' => 20],
                [
                    $url . '?page%5Bafter%5D=' . $cursor . '&page%5Bsize%5D=100',
                    ['page' => ['size' => '100', 'after' => $cursor]],
                ],
            ],
            // Any other parameter that the next page link omits is kept too.
            [
                '/teams/foo/project-access?page%5Bafter%5D=' . $cursor,
                '/teams/foo/project-access',
                ['sort' => 'project_title'],
                [
                    '/teams/foo/project-access?page%5Bafter%5D=' . $cursor,
                    ['sort' => 'project_title', 'page' => ['after' => $cursor]],
                ],
            ],
            // There is no next page.
            [null, $url, $filter, null],
            ['', $url, $filter, null],
            // The next page link repeats the current request.
            [
                $url . '?page%5Bafter%5D=' . $cursor,
                $url . '?page%5Bafter%5D=' . $cursor,
                ['page' => ['after' => $cursor]],
                null,
            ],
        ];
        foreach ($cases as $i => $case) {
            [$nextPageUrl, $url, $query, $expected] = $case;
            $this->assertEquals($expected, PaginationUtil::nextPage($nextPageUrl, $url, $query), "Case $i");
        }
    }
}
