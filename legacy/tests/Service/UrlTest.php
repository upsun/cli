<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\MockObject\MockObject;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Shell;
use Platformsh\Cli\Service\Url;
use Platformsh\Cli\Util\OsUtil;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

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
        foreach (['DISPLAY', 'WSL_DISTRO_NAME', 'WSL_INTEROP'] as $name) {
            $this->env[$name] = getenv($name);
        }
        putenv('DISPLAY');
        putenv('WSL_INTEROP');
        putenv('WSL_DISTRO_NAME=Ubuntu');
    }

    protected function tearDown(): void
    {
        foreach ($this->env as $name => $value) {
            putenv($value === false ? $name : "$name=$value");
        }
    }

    /**
     * @param string[] $commands
     */
    private function urlService(array $commands, bool $interop = true): Url
    {
        $this->shell = $this->createMock(Shell::class);
        $this->shell->method('commandExists')->willReturnCallback(fn(string $c): bool => in_array($c, $commands, true));
        $definition = new InputDefinition();
        Url::configureInput($definition);

        return new class ($interop, $this->shell, new ArrayInput([], $definition), new BufferedOutput()) extends Url {
            public function __construct(private readonly bool $interop, Shell $shell, InputInterface $input, OutputInterface $output)
            {
                parent::__construct($shell, $input, $output);
            }

            protected function isWslInteropEnabled(): bool
            {
                return $this->interop;
            }
        };
    }

    public function testWslOpensUrlsWithRundll32(): void
    {
        $url = $this->urlService(['rundll32.exe']);
        $this->assertTrue($url->hasDisplay());
        $this->assertTrue($url->canOpenUrls());

        $this->shell->expects($this->once())
            ->method('execute')
            ->with(['rundll32.exe', 'url.dll,FileProtocolHandler', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    public function testWslFindsRundll32OutsidePath(): void
    {
        $rundll32 = '/mnt/c/Windows/System32/rundll32.exe';
        $url = $this->urlService([$rundll32]);
        $this->shell->expects($this->once())
            ->method('execute')
            ->with([$rundll32, 'url.dll,FileProtocolHandler', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    public function testWslPrefersWslview(): void
    {
        $url = $this->urlService(['wslview', 'rundll32.exe']);
        $this->shell->expects($this->once())
            ->method('execute')
            ->with(['wslview', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    public function testWslWithoutOpener(): void
    {
        $url = $this->urlService([]);
        $this->assertFalse($url->hasDisplay());
        $this->assertFalse($url->canOpenUrls());
    }

    public function testWslInteropOff(): void
    {
        $url = $this->urlService(['wslview', 'rundll32.exe'], false);
        $this->assertFalse($url->hasDisplay());
        $this->assertFalse($url->canOpenUrls());
    }
}
