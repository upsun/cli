<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\MockObject\MockObject;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Shell;
use Platformsh\Cli\Service\Url;
use Platformsh\Cli\Util\OsUtil;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Filesystem\Filesystem;

class UrlTest extends TestCase
{
    private const ENV = ['DISPLAY', 'WSL_DISTRO_NAME', 'WSL_INTEROP'];

    /** @var array<string, string|false> */
    private array $env = [];

    private string $dir;

    private Shell&MockObject $shell;

    protected function setUp(): void
    {
        if (!OsUtil::isLinux()) {
            $this->markTestSkipped('WSL is Linux');
        }
        foreach (self::ENV as $name) {
            $this->env[$name] = getenv($name);
            putenv($name);
        }
        putenv('WSL_DISTRO_NAME=Ubuntu');
        $this->dir = sys_get_temp_dir() . '/cli-url-test-' . bin2hex(random_bytes(6));
        mkdir($this->dir . '/binfmt', 0o700, true);
    }

    protected function tearDown(): void
    {
        foreach ($this->env as $name => $value) {
            putenv($value === false ? $name : "$name=$value");
        }
        if (isset($this->dir)) {
            (new Filesystem())->remove($this->dir);
        }
    }

    private function urlService(bool $rundll32InPath = false): Url
    {
        $this->shell = $this->createMock(Shell::class);
        $this->shell->method('commandExists')->willReturnCallback(
            fn(string $command): bool => $rundll32InPath && $command === 'rundll32.exe',
        );
        $definition = new InputDefinition();
        Url::configureInput($definition);
        $url = new Url($this->shell, new ArrayInput([], $definition), new BufferedOutput());
        $url->osReleasePath = $this->dir . '/osrelease';
        $url->wslBinfmtDir = $this->dir . '/binfmt';
        $url->wslConfPath = $this->dir . '/wsl.conf';
        $url->wslDefaultMountRoot = $this->dir . '/mnt/';

        return $url;
    }

    private function setInterop(string $state): void
    {
        file_put_contents($this->dir . '/binfmt/WSLInterop', $state . "\ninterpreter /init\n");
    }

    private function writeStub(string $path): void
    {
        if (!is_dir(dirname($path))) {
            mkdir(dirname($path), 0o700, true);
        }
        file_put_contents($path, "#!/bin/sh\n");
        chmod($path, 0o700);
    }

    private function defaultRundll32(): string
    {
        return $this->dir . '/mnt/c/Windows/System32/rundll32.exe';
    }

    public function testWslOpensUrlsWithRundll32(): void
    {
        $this->setInterop('enabled');
        $this->writeStub($this->defaultRundll32());
        $url = $this->urlService();
        $this->assertTrue($url->hasDisplay());
        $this->assertTrue($url->canOpenUrls());

        $this->shell->expects($this->once())
            ->method('execute')
            ->with([$this->defaultRundll32(), 'url.dll,FileProtocolHandler', 'http://127.0.0.1:5000'])
            ->willReturn('');
        $this->assertTrue($url->openUrl('http://127.0.0.1:5000', false));
    }

    /**
     * @return array<string, array{string, string, string, bool, string|false}>
     */
    public static function wslBrowserCases(): array
    {
        $custom = "[automount]\nroot = %s/custom/\n";

        return [
            'default root' => ['enabled', '', 'mnt', false, 'mnt'],
            'custom root' => ['enabled', $custom, 'custom', false, 'custom'],
            'custom root, quoted, no slash' => ['enabled', "[Automount]\nRoot = \"%s/custom\" # comment\n", 'custom', false, 'custom'],
            'custom root, rundll32 in default root' => ['enabled', $custom, 'mnt', false, false],
            'PATH fallback' => ['enabled', '', '', true, 'rundll32.exe'],
            'no rundll32' => ['enabled', '', '', false, false],
            'no interop entry' => ['', '', 'mnt', true, false],
            'interop entry disabled' => ['disabled', '', 'mnt', true, false],
            'wsl.conf disables interop' => ['enabled', "[boot]\nsystemd=true\n[Interop]\n  Enabled = 'False'\n", 'mnt', true, false],
            'wsl.conf enables interop' => ['enabled', "[interop]\nenabled=true\n", 'mnt', false, 'mnt'],
            'commented out' => ['enabled', "[interop]\n# enabled=false\n", 'mnt', false, 'mnt'],
        ];
    }

    #[DataProvider('wslBrowserCases')]
    public function testWslBrowser(string $interop, string $wslConf, string $stubRoot, bool $rundll32InPath, string|false $want): void
    {
        if ($interop !== '') {
            $this->setInterop($interop);
        }
        if ($wslConf !== '') {
            file_put_contents($this->dir . '/wsl.conf', sprintf($wslConf, $this->dir));
        }
        if ($stubRoot !== '') {
            $this->writeStub($this->dir . '/' . $stubRoot . '/c/Windows/System32/rundll32.exe');
        }
        $url = $this->urlService($rundll32InPath);
        if ($want === false) {
            $this->assertFalse($url->hasDisplay());
            $this->assertFalse($url->canOpenUrls());
            return;
        }
        $expected = $want === 'rundll32.exe' ? $want : $this->dir . '/' . $want . '/c/Windows/System32/rundll32.exe';
        $this->assertTrue($url->hasDisplay());
        $this->shell->expects($this->once())
            ->method('execute')
            ->with([$expected, 'url.dll,FileProtocolHandler', 'https://example.com'])
            ->willReturn('');
        $url->openUrl('https://example.com', false);
    }

    public function testWslDetectedFromKernelRelease(): void
    {
        putenv('WSL_DISTRO_NAME');
        $this->setInterop('enabled');
        $this->writeStub($this->defaultRundll32());
        $url = $this->urlService();
        $this->assertFalse($url->hasDisplay(), 'not WSL');

        file_put_contents($this->dir . '/osrelease', "5.15.167.4-Microsoft-standard-WSL2\n");
        $this->assertTrue($this->urlService()->hasDisplay());
    }

    public function testWslConfValue(): void
    {
        $conf = "[boot]\nsystemd=true\nroot=/boot/\n\n[Automount] # mounts\n  Root = /win/ # comment\n"
            . "[a]\nd=\"x\"\ns='y'\nm=\"z'\nq=\"\n";
        $this->assertSame('true', Url::wslConfValue($conf, 'boot', 'systemd'));
        $this->assertSame('/win/', Url::wslConfValue($conf, 'automount', 'root'));
        $this->assertSame('x', Url::wslConfValue($conf, 'a', 'd'));
        $this->assertSame('y', Url::wslConfValue($conf, 'a', 's'));
        $this->assertSame("\"z'", Url::wslConfValue($conf, 'a', 'm'));
        $this->assertSame('"', Url::wslConfValue($conf, 'a', 'q'));
        $this->assertSame('', Url::wslConfValue($conf, 'interop', 'enabled'));
        $this->assertSame('', Url::wslConfValue('', 'automount', 'root'));
    }
}
