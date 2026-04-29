<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\ActivityMonitor;
use Platformsh\Client\Model\Activity;

class ActivityMonitorTest extends TestCase
{
    public function testFormatResultSuccess(): void
    {
        $activity = new Activity(['id' => 'a', 'result' => Activity::RESULT_SUCCESS]);
        $this->assertSame('success', ActivityMonitor::formatResult($activity, false));
    }

    public function testFormatResultFailure(): void
    {
        $activity = new Activity(['id' => 'a', 'result' => Activity::RESULT_FAILURE]);
        $this->assertSame('failure', ActivityMonitor::formatResult($activity, false));
        $this->assertSame('<error>failure</error>', ActivityMonitor::formatResult($activity, true));
    }

    public function testFormatResultNull(): void
    {
        // An in-progress activity has no result yet.
        $activity = new Activity(['id' => 'a', 'state' => Activity::STATE_IN_PROGRESS]);
        $this->assertSame('', ActivityMonitor::formatResult($activity, false));
        $this->assertSame('', ActivityMonitor::formatResult($activity, true));
    }

    public function testFormatResultFailedCommandOverridesSuccess(): void
    {
        $activity = new Activity([
            'id' => 'a',
            'result' => Activity::RESULT_SUCCESS,
            'commands' => [
                ['exit_code' => 0],
                ['exit_code' => 1],
            ],
        ]);
        $this->assertSame('failure', ActivityMonitor::formatResult($activity, false));
        $this->assertSame('<error>failure</error>', ActivityMonitor::formatResult($activity, true));
    }

    public function testFormatResultFailedCommandWithNullResult(): void
    {
        $activity = new Activity([
            'id' => 'a',
            'state' => Activity::STATE_IN_PROGRESS,
            'commands' => [
                ['exit_code' => 1],
            ],
        ]);
        $this->assertSame('failure', ActivityMonitor::formatResult($activity, false));
    }
}
