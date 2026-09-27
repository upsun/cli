<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\Resources;

use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\Resources\ResourcesSetCommand;
use Platformsh\Cli\Tests\MockApp;
use Platformsh\Client\Model\Deployment\Service;
use Platformsh\Client\Model\Deployment\Task;
use Platformsh\Client\Model\Deployment\WebApp;
use Platformsh\Client\Model\Deployment\Worker;
use Symfony\Component\Console\Command\LazyCommand;
use Symfony\Component\Console\Exception\InvalidArgumentException;

#[Group('commands')]
class ResourcesSetTest extends TestCase
{
    private function getCommandInstance(): ResourcesSetCommand
    {
        $command = MockApp::instance()->find('resources:set');
        if ($command instanceof LazyCommand) {
            $command = $command->getCommand();
        }
        /** @var ResourcesSetCommand $command */
        return $command;
    }

    public function testValidateInstanceCount(): void
    {
        $command = $this->getCommandInstance();
        $m = new \ReflectionMethod($command, 'validateInstanceCount');

        $cases = [
            'app' => [WebApp::fromData([]), false, '2', 2],
            'worker' => [Worker::fromData([]), false, '3', 3],
            'app with autoscaling' => [WebApp::fromData([]), true, '2', 'cannot be changed when autoscaling is enabled'],
            'task' => [Task::fromData([]), false, '2', 'cannot be changed'],
            'service without flag' => [Service::fromData([]), false, '2', 'does not support horizontal scaling'],
            'service not supporting' => [Service::fromData(['supports_horizontal_scaling' => false]), false, '2', 'does not support horizontal scaling'],
            'service supporting' => [Service::fromData(['supports_horizontal_scaling' => true]), false, '2', 2],
            'service supporting with autoscaling' => [Service::fromData(['supports_horizontal_scaling' => true]), true, '2', 'cannot be changed when autoscaling is enabled'],
            'service supporting over limit' => [Service::fromData(['supports_horizontal_scaling' => true]), false, '9', 'exceeds the limit 8'],
        ];
        foreach ($cases as $name => [$service, $autoscalingEnabled, $value, $expected]) {
            if (is_int($expected)) {
                $this->assertSame($expected, $m->invoke($command, $value, 'foo', $service, 8, $autoscalingEnabled), $name);
                continue;
            }
            try {
                $m->invoke($command, $value, 'foo', $service, 8, $autoscalingEnabled);
                $this->fail('Expected an exception: ' . $name);
            } catch (InvalidArgumentException $e) {
                $this->assertStringContainsString($expected, $e->getMessage(), $name);
            }
        }
    }

    /**
     * A container whose minimum disk is 0 supports a disk without needing one,
     * so it must not be treated as unconfigured. Asking about it is what made
     * "resources:set --object-storage <app>:<size>" prompt for the regular disk
     * of every app that had none.
     */
    public function testDiskIsRequiredButUnset(): void
    {
        $command = $this->getCommandInstance();
        $m = new \ReflectionMethod($command, 'diskIsRequiredButUnset');

        // Triples of the resources.minimum.disk value, the allocated disk, and
        // whether a disk is still owed. A null disk means the property is absent.
        $cases = [
            [0, null, false],
            [0, 0, false],
            [512, null, true],
            [512, 0, true],
            [512, 512, false],
            [512, 1024, false],
        ];
        foreach ($cases as [$minimum, $disk, $expected]) {
            $properties = ['resources' => ['minimum' => ['disk' => $minimum]]];
            if ($disk !== null) {
                $properties['disk'] = $disk;
            }
            $this->assertSame($expected, $m->invoke($command, $properties), sprintf(
                'minimum %s, disk %s',
                var_export($minimum, true),
                var_export($disk, true),
            ));
        }
    }
}
