<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\Resources;

use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\Resources\ResourcesSetCommand;
use Platformsh\Cli\Tests\MockApp;
use Symfony\Component\Console\Command\LazyCommand;

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
