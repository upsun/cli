<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Local;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Local\LocalProject;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Git;
use Platformsh\Cli\Tests\HasTempDirTrait;

class LocalProjectTest extends TestCase
{
    use HasTempDirTrait;

    private LocalProject $localProject;

    public function setUp(): void
    {
        $this->tempDirSetUp();
        $this->localProject = new LocalProject(new Config([], __DIR__ . '/../data/mock-cli-config.yaml'));
    }

    public function testWriteGitExcludeInRepository(): void
    {
        $dir = $this->createTempSubDir('repo');
        exec('git init --quiet ' . escapeshellarg($dir), result_code: $code);
        $this->assertSame(0, $code);

        $this->localProject->writeGitExclude($dir);

        $this->assertStringContainsString('Automatically added by the Mock CLI', (string) file_get_contents($dir . '/.git/info/exclude'));
    }

    public function testWriteGitExcludeWhenGitFails(): void
    {
        $dir = $this->createTempSubDir('repo');
        mkdir($dir . '/.git');
        $git = $this->createStub(Git::class);
        $git->method('execute')->willReturn(false);
        $localProject = new LocalProject(new Config([], __DIR__ . '/../data/mock-cli-config.yaml'), $git);

        $localProject->writeGitExclude($dir);

        $this->assertFileExists($dir . '/.git/info/exclude');
    }

    public function testWriteGitExcludeOutsideRepository(): void
    {
        $dir = $this->createTempSubDir('norepo');

        $this->localProject->writeGitExclude($dir);

        $this->assertFileDoesNotExist($dir . '/.git');
    }
}
