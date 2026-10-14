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

    public function testProjectConfigFallbackFile(): void
    {
        $dir = $this->initRepo();
        $this->writeFile($dir . '/.legacy/local/project.yaml', "id: legacyproject\n");
        $localProject = $this->localProjectWithFallbacks();

        $this->assertSame(realpath($dir), $localProject->getProjectRoot($dir));
        $this->assertSame(['id' => 'legacyproject'], $localProject->getProjectConfig($dir));
        $this->assertFileDoesNotExist($dir . '/.mock/local');
    }

    public function testProjectConfigPrimaryFileTakesPrecedence(): void
    {
        $dir = $this->initRepo();
        $this->writeFile($dir . '/.legacy/local/project.yaml', "id: legacyproject\n");
        $this->writeFile($dir . '/.mock/local/project.yaml', "id: mockproject\n");

        $this->assertSame(['id' => 'mockproject'], $this->localProjectWithFallbacks()->getProjectConfig($dir));
    }

    public function testProjectConfigFallbackGitRemote(): void
    {
        $dir = $this->initRepo();
        exec('git -C ' . escapeshellarg($dir) . ' remote add legacy abcdefghijkl@git.eu-1.example.com:abcdefghijkl.git', result_code: $code);
        $this->assertSame(0, $code);

        $this->assertSame(
            ['id' => 'abcdefghijkl', 'host' => 'eu-1.example.com'],
            $this->localProjectWithFallbacks()->getProjectConfig($dir),
        );
    }

    public function testProjectConfigSkipsUnrecognizedGitRemote(): void
    {
        $dir = $this->initRepo();
        exec('git -C ' . escapeshellarg($dir) . ' remote add legacy https://example.org/unrelated.git');
        exec('git -C ' . escapeshellarg($dir) . ' remote add origin abcdefghijkl@git.eu-1.example.com:abcdefghijkl.git');

        $this->assertSame(
            ['id' => 'abcdefghijkl', 'host' => 'eu-1.example.com'],
            $this->localProjectWithFallbacks()->getProjectConfig($dir),
        );
    }

    public function testProjectConfigFallbacksNotConfigured(): void
    {
        $dir = $this->initRepo();
        $this->writeFile($dir . '/.legacy/local/project.yaml', "id: legacyproject\n");
        exec('git -C ' . escapeshellarg($dir) . ' remote add legacy abcdefghijkl@git.eu-1.example.com:abcdefghijkl.git');
        $config = (new Config([], __DIR__ . '/../data/mock-cli-config.yaml'))->withOverrides([
            'detection.git_remote_name' => 'mock',
            'detection.git_domain' => 'example.com',
        ]);

        $this->assertNull((new LocalProject($config))->getProjectConfig($dir));
    }

    private function localProjectWithFallbacks(): LocalProject
    {
        $config = (new Config([], __DIR__ . '/../data/mock-cli-config.yaml'))->withOverrides([
            'detection.git_remote_name' => 'mock',
            'detection.git_remote_fallbacks' => ['legacy'],
            'detection.git_domain' => 'example.com',
            'local.project_config_fallbacks' => ['.legacy/local/project.yaml'],
        ]);

        return new LocalProject($config);
    }

    private function initRepo(): string
    {
        $dir = $this->createTempSubDir('repo');
        exec('git init --quiet ' . escapeshellarg($dir), result_code: $code);
        $this->assertSame(0, $code);

        return $dir;
    }

    private function writeFile(string $filename, string $contents): void
    {
        if (!is_dir(dirname($filename))) {
            mkdir(dirname($filename), 0o755, true);
        }
        file_put_contents($filename, $contents);
    }
}
