<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\ResourcesUtil;

class ResourcesUtilTest extends TestCase
{
    public function testFormatObjectStorageGB(): void
    {
        $cases = [
            // [mib, expected_gb_string, description]
            [0, '0', 'zero'],
            [1024, '1', '1 GB exact'],
            [524288, '512', '512 GB exact'],
            [10485760, '10240', '10 TiB exact'],
            [1536, '1.5', '1.5 GB exact'],
            [1280, '1.25', '1.25 GB exact'],
            [1500, '1.46', 'fractional rounds to 2 decimals'],
            [100, '0.1', 'sub-GB value rounds and trims'],
            [1024.0, '1', 'float input, whole GB'],
            [1536.0, '1.5', 'float input, half GB'],
        ];
        foreach ($cases as $key => $case) {
            [$mib, $expected, $description] = $case;
            $this->assertSame(
                $expected,
                ResourcesUtil::formatObjectStorageGB($mib),
                "case $key: $description",
            );
        }
    }
}
