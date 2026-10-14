<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use GuzzleHttp\Client;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Client\Model\Deployment\EnvironmentDeployment;
use Platformsh\Client\Model\Project;
use Platformsh\Client\Model\Project\Capabilities;
use Symfony\Component\Console\Output\BufferedOutput;

class ApiGuaranteedCpuTest extends TestCase
{
    /**
     * @return array<string, array{array<string, mixed>, array<string, mixed>, bool}>
     */
    public static function cases(): array
    {
        return [
            'enabled' => [['enable_guaranteed_resources' => true], ['guaranteed_resources' => ['enabled' => true]], true],
            'not enabled in settings' => [['enable_guaranteed_resources' => false], ['guaranteed_resources' => ['enabled' => true]], false],
            'not enabled in capabilities' => [['enable_guaranteed_resources' => true], ['guaranteed_resources' => ['enabled' => false]], false],
            'missing capability' => [['enable_guaranteed_resources' => true], [], false],
        ];
    }

    /**
     * @param array<string, mixed> $settings
     * @param array<string, mixed> $capabilities
     */
    #[DataProvider('cases')]
    public function testSupportsGuaranteedCpuFromProject(array $settings, array $capabilities, bool $expected): void
    {
        $api = $this->createApi($settings, $capabilities);
        $this->assertSame($expected, $api->supportsGuaranteedCPU($this->project()));
    }

    /**
     * @param array<string, mixed> $settings
     * @param array<string, mixed> $capabilities
     */
    #[DataProvider('cases')]
    public function testSupportsGuaranteedCpuFromDeployment(array $settings, array $capabilities, bool $expected): void
    {
        $api = $this->createApi([], []);
        $deployment = new EnvironmentDeployment(
            ['id' => 'current', 'project_info' => ['settings' => $settings, 'capabilities' => $capabilities]],
            'https://api.example.test/api/projects/test/environments/main/deployments/current',
            new Client(),
        );
        $this->assertSame($expected, $api->supportsGuaranteedCPU($this->project(), $deployment));
    }

    private function project(): Project
    {
        return new Project(['id' => 'test'], 'https://api.example.test/api/projects/test', new Client());
    }

    /**
     * @param array<string, mixed> $settings
     * @param array<string, mixed> $capabilities
     */
    private function createApi(array $settings, array $capabilities): Api
    {
        $config = new Config(['PLATFORMSH_CLI_SESSION_ID' => 'guaranteed-cpu-test']);

        return new class ($config, $settings, Capabilities::fromData($capabilities)) extends Api {
            /**
             * @param array<string, mixed> $settings
             */
            public function __construct(Config $config, private readonly array $settings, private readonly Capabilities $capabilities)
            {
                parent::__construct($config, new ArrayCache(), new BufferedOutput());
            }

            public function getProjectSettings(Project $project, bool $refresh = false): array
            {
                return $this->settings;
            }

            public function getProjectCapabilities(Project $project, bool $refresh = false): Capabilities
            {
                return $this->capabilities;
            }
        };
    }
}
