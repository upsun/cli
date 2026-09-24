<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Exception\ProcessFailedException;
use Platformsh\Cli\Service\Shell;
use Platformsh\Cli\Tests\HasTempDirTrait;
use Symfony\Component\Process\Exception\ProcessStartFailedException;

class ShellServiceTest extends TestCase
{
    use HasTempDirTrait;

    /**
     * Test Shell::execute().
     */
    public function testExecute(): void
    {
        $shell = new Shell();

        // Find a command that will work on all platforms.
        $workingCommand = str_contains(PHP_OS, 'WIN') ? 'help' : 'pwd';

        // Test commandExists().
        $this->assertTrue($shell->commandExists($workingCommand));
        $this->assertFalse($shell->commandExists('nonexistent'));

        // With $mustRun disabled.
        $this->assertNotEmpty($shell->execute([$workingCommand]));
        $this->assertFalse($shell->execute(['which', 'nonexistent']));

        // With $mustRun enabled.
        $this->assertNotEmpty($shell->execute([$workingCommand], mustRun: true));
        $this->expectException(\Exception::class);
        $shell->execute(['which', 'nonexistent'], mustRun: true);
    }

    /**
     * Test Shell::mustExecute().
     */
    public function testMustExecute(): void
    {
        $shell = new Shell();

        $workingCommand = str_contains(PHP_OS, 'WIN') ? 'help' : 'pwd';

        $this->assertNotEmpty($shell->mustExecute($workingCommand));
        $this->expectException(ProcessFailedException::class);
        $shell->mustExecute(['which', 'nonexistent']);
    }

    /**
     * Test Shell::execute() when the process cannot start, so has no exit code.
     */
    public function testExecuteStartFailure(): void
    {
        $this->tempDirSetUp();
        assert($this->tempDir !== null);

        // A phar:// directory passes is_dir() but cannot be used by proc_open().
        $archive = new \PharData($this->tempDir . '/test.tar');
        $archive->addFromString('dir/file.txt', 'test');
        $dir = 'phar://' . $this->tempDir . '/test.tar/dir';

        $shell = new Shell();
        $this->assertFalse($shell->execute(['pwd'], $dir));
        try {
            $shell->mustExecute(['pwd'], $dir);
            $this->fail('Expected a ProcessStartFailedException');
        } catch (ProcessStartFailedException $e) {
            $this->assertFalse($shell->exceptionMeansCommandDoesNotExist($e));
        }
    }
}
