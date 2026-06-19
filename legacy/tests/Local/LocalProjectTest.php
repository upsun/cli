<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Local;

use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Local\LocalProject;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Git;
use Platformsh\Cli\Tests\HasTempDirTrait;

#[Group('slow')]
class LocalProjectTest extends TestCase
{
    use HasTempDirTrait;

    public function setUp(): void
    {
        $this->tempDirSetUp();
    }

    /**
     * A Git worktree must be recognised as its own project root.
     *
     * Regression test: in a worktree, .git is a file (a gitlink), not a
     * directory. When the worktree is nested inside another checkout of the
     * same repository, project-root detection used to skip the worktree and
     * climb up to the parent, picking up the parent's branch (e.g. "main").
     */
    public function testGetProjectRootInWorktree(): void
    {
        $git = new Git();

        // A parent repository with an Upsun-style Git remote and no local
        // project config file, so detection relies on the remote URL.
        $parentDir = $this->tempDir . '/parent';
        if (!mkdir($parentDir, 0o755, true)) {
            throw new \RuntimeException('Failed to create directory: ' . $parentDir);
        }
        $git->init($parentDir, 'main', true);
        $git->execute(['config', 'user.email', 'test@example.com'], $parentDir, true);
        $git->execute(['config', 'user.name', 'Test'], $parentDir, true);
        $git->execute(['config', 'commit.gpgsign', 'false'], $parentDir, true);
        $git->execute(
            ['remote', 'add', 'platform', 'abcdefghijkl@git.eu-5.example.com:abcdefghijkl.git'],
            $parentDir,
            true,
        );
        touch($parentDir . '/README.txt');
        $git->execute(['add', '-A'], $parentDir, true);
        $git->execute(['commit', '-qm', 'Initial commit'], $parentDir, true);

        // A worktree nested inside the parent's working tree, on its own branch.
        $worktreeDir = $parentDir . '/nested/worktree';
        $git->execute(['worktree', 'add', '-b', 'feature', $worktreeDir], $parentDir, true);
        $this->assertFalse(is_dir($worktreeDir . '/.git'), 'In a worktree, .git is a file');

        $config = (new Config())->withOverrides([
            'detection.git_domain' => 'example.com',
            'detection.git_remote_name' => 'platform',
            'local.project_config' => '.platform/local/project.yaml',
        ]);
        $localProject = new LocalProject($config, $git);

        $this->assertEquals(realpath($worktreeDir), $localProject->getProjectRoot($worktreeDir));
    }
}
