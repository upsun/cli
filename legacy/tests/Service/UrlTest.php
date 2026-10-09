<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\MockObject\MockObject;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Shell;
use Platformsh\Cli\Service\Url;
use Platformsh\Cli\Util\OsUtil;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Output\BufferedOutput;

class UrlTest extends TestCase
{
    /** @var array<string, string|false> */
    private array $env = [];

    private Shell&MockObject $shell;

    protected function setUp(): void
    {
        if (!OsUtil::isLinux()) {
            $this->markTestSkipped('WSL is Linux');
        }
        foreach (['DISPLAY', 'MOCK_CLI_WSL_BROWSER'] as $name) {
            $this->env[$name] = getenv($name);
        }
        putenv('DISPLAY');
        putenv('MOCK_CLI_WSL_BROWSER');
    }

    protected function tearDown(): void
    {
        foreach ($this->env as $name => $value) {
            putenv($value === false ? $name : "$name=$value");
        }
    }

    private function urlService(): Url
    {
        $this->shell = $this->createMock(Shell::class);
        $this->shell->method('commandExists')->willReturn(false);
        $definition = new InputDefinition();
        Url::configureInput($definition);

        return new Url(
            $this->shell,
            new ArrayInput([], $definition),
            new BufferedOutput(),
            new Config([], __DIR__ . '/../data/mock-cli-config.yaml'),
        );
    }

    public function testWslOpensUrlsWithRundll32(): void
    {
        $rundll32 = '/mnt/c/Windows/System32/rundll32.exe';
        putenv('MOCK_CLI_WSL_BROWSER=' . $rundll32);
        $url = $this->urlService();
        $this->assertTrue($url->hasDisplay());
        $this->assertTrue($url->canOpenUrls());

        $this->shell->expects($this->once())
            ->method('execute')
            ->with([$rundll32, 'url.dll,FileProtocolHandler', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    public function testWslOpensUrlsWithWslview(): void
    {
        putenv('MOCK_CLI_WSL_BROWSER=/usr/bin/wslview');
        $url = $this->urlService();
        $this->shell->expects($this->once())
            ->method('execute')
            ->with(['/usr/bin/wslview', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    public function testNoWslBrowser(): void
    {
        putenv('MOCK_CLI_WSL_BROWSER=');
        $url = $this->urlService();
        $this->assertFalse($url->hasDisplay());
        $this->assertFalse($url->canOpenUrls());
    }
}
