<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Local;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Local\ApplicationFinder;
use Platformsh\Cli\Service\Config;

class ApplicationFinderTest extends TestCase
{
    private ApplicationFinder $finder;

    public function setUp(): void
    {
        $config = (new Config())->withOverrides([
            'service.app_config_file' => '_platform.app.yaml',
            'service.applications_config_file' =>  '_platform/applications.yaml',
        ]);
        $this->finder = new ApplicationFinder($config);
    }

    public function testFindNestedApps(): void
    {
        $fakeAppRoot = 'tests/data/repositories/multiple/nest';

        $apps = $this->finder->findApplications($fakeAppRoot);
        $this->assertCount(3, $apps);
    }

    public function testFindAppsUnderGroupedConfig(): void
    {
        $fakeAppRoot = 'tests/data/repositories/multi-grouped-config';

        $apps = $this->finder->findApplications($fakeAppRoot);
        $this->assertCount(3, $apps);
    }

    public function testDetectMultiple(): void
    {
        $fakeRepositoryRoot = 'tests/data/repositories/multiple';

        $apps = $this->finder->findApplications($fakeRepositoryRoot);
        $this->assertCount(6, $apps);
    }

    public function testFindAppsUnderFlexConfig(): void
    {
        $finder = new ApplicationFinder((new Config())->withOverrides([
            'service.app_config_file' => null,
            'service.project_config_dir' => '_upsun',
        ]));
        $fakeRepositoryRoot = 'tests/data/repositories/flex';

        $types = [];
        foreach ($finder->findApplications($fakeRepositoryRoot) as $app) {
            $types[$app->getName()] = [$app->getType(), $app->getRoot()];
        }
        $this->assertEquals([
            'app' => ['php:8.4', $fakeRepositoryRoot],
            'frontend' => ['nodejs:22', $fakeRepositoryRoot . '/frontend'],
            'worker' => ['composable:25.05', $fakeRepositoryRoot],
        ], $types);
    }
}
