<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Local;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Local\LocalBuild;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Tests\Container;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

class LocalBuildTest extends TestCase
{
    private ?LocalBuild $localBuild;
    private Config $config;

    public function setUp(): void
    {
        $container = Container::instance();
        $this->config = new Config([], __DIR__ . '/../data/mock-cli-config.yaml');
        $container->set(Config::class, $this->config);
        $container->set(InputInterface::class, new ArrayInput([]));
        $container->set(OutputInterface::class, new BufferedOutput());
        $this->localBuild = $container->get(LocalBuild::class);
    }

    public function testGetTreeId(): void
    {
        $treeId = $this->localBuild->getTreeId('tests/data/apps/composer', []);
        $this->assertEquals('0d9f5dd9a2907d905efc298686bb3c4e2f9a4811', $treeId);
        $treeId = $this->localBuild->getTreeId('tests/data/apps/composer', ['clone' => true]);
        $this->assertEquals('7f63ba117166a67cf217294e6a2c7b20c96e09f6', $treeId);
    }

    public function testCleanBuildsKeepsNewest(): void
    {
        $projectRoot = sys_get_temp_dir() . '/cli-test-' . bin2hex(random_bytes(4));
        $buildsDir = $projectRoot . '/' . $this->config->getStr('local.build_dir');
        mkdir($buildsDir, 0o755, true);
        try {
            $now = time();
            foreach (['b1', 'b2', 'b3', 'b4', 'b5'] as $i => $name) {
                mkdir($buildsDir . '/' . $name);
                touch($buildsDir . '/' . $name, $now - (5 - $i) * 100);
            }

            $this->assertNotNull($this->localBuild);
            [$deleted, $kept] = $this->localBuild->cleanBuilds($projectRoot, null, 2);

            $this->assertEquals([3, 2], [$deleted, $kept]);
            $remaining = array_map('basename', glob($buildsDir . '/*') ?: []);
            sort($remaining);
            $this->assertEquals(['b4', 'b5'], $remaining);
        } finally {
            (new \Symfony\Component\Filesystem\Filesystem())->remove($projectRoot);
        }
    }
}
