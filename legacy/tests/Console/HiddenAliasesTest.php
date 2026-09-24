<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Console;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Application;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Console\CustomJsonDescriptor;
use Platformsh\Cli\Console\CustomTextDescriptor;
use Platformsh\Cli\Service\Config;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Command\LazyCommand;
use Symfony\Component\Console\Exception\CommandNotFoundException;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\NullOutput;
use Symfony\Component\Yaml\Yaml;

class HiddenAliasesTest extends TestCase
{
    private Application $app;

    public function setUp(): void
    {
        // A separate application, as loading commands affects MockApp's state.
        $this->app = new Application(new Config([], __DIR__ . '/../data/mock-cli-config.yaml'));
        $this->app->setIO(new ArrayInput([]), new NullOutput());
    }

    /**
     * @return array<array{string, string}>
     */
    public static function aliasProvider(): array
    {
        return [
            ['snapshots', 'backup:list'],
            ['snapshot:list', 'backup:list'],
            ['logs', 'environment:logs'],
            ['user:role', 'user:get'],
            ['environment:sql', 'db:sql'],
        ];
    }

    #[DataProvider('aliasProvider')]
    public function testFindHiddenAlias(string $alias, string $expected): void
    {
        $this->assertSame($expected, $this->app->find($alias)->getName());
    }

    public function testAllHiddenAliasesResolve(): void
    {
        $count = 0;
        foreach ($this->app->all() as $name => $command) {
            $command = $this->load($command);
            if ($command->getName() !== $name || !$command instanceof CommandBase) {
                continue;
            }
            foreach ($command->getHiddenAliases() as $alias) {
                $this->assertSame($name, $this->app->find($alias)->getName(), "Hidden alias $alias");
                $count++;
            }
        }
        $this->assertGreaterThan(0, $count);
    }

    public function testHiddenAliasesAreNotVisible(): void
    {
        $command = $this->load($this->app->find('backup:list'));
        $this->assertInstanceOf(CommandBase::class, $command);
        $this->assertSame(['backups'], $command->getVisibleAliases());
        $this->assertSame(['snapshots', 'snapshot:list'], $command->getHiddenAliases());

        $output = new BufferedOutput();
        (new CustomTextDescriptor('mock-cli'))->describe($output, $command);
        $help = $output->fetch();
        $this->assertStringContainsString('Aliases: backups', $help);
        $this->assertStringNotContainsString('snapshot', $help);

        (new CustomJsonDescriptor())->describe($output, $command);
        $data = json_decode($output->fetch(), true);
        $this->assertIsArray($data);
        $this->assertSame(['backups'], $data['aliases']);
        $this->assertSame(['snapshots', 'snapshot:list'], $data['hidden_aliases']);
    }

    public function testHiddenAliasesDoNotDefineNamespaces(): void
    {
        $this->assertSame('db', $this->app->findDescribableNamespace('db'));
        foreach (['snapshot', 'int', 'i'] as $name) {
            $this->assertNull($this->app->findDescribableNamespace($name), $name);
        }
    }

    public function testHiddenAliasesAreNotAbbreviated(): void
    {
        $this->assertSame('db:dump', $this->app->find('sql-dump')->getName());
        $this->expectException(CommandNotFoundException::class);
        $this->app->find('sql-dum');
    }

    public function testHasAndGetAgreeForHiddenAliases(): void
    {
        $this->assertTrue($this->app->has('snapshots'));
        $this->assertSame('backup:list', $this->app->get('snapshots')->getName());
    }

    public function testDisabledCommands(): void
    {
        $config = Yaml::parseFile(__DIR__ . '/../data/mock-cli-config.yaml');
        $this->assertIsArray($config);
        $this->assertIsArray($config['application']);
        $config['application']['disabled_commands'] = ['completion', 'backup:list'];
        $file = tempnam(sys_get_temp_dir(), 'cli-config-');
        $this->assertIsString($file);
        file_put_contents($file, Yaml::dump($config));
        try {
            $app = new Application(new Config([], $file));
            $app->setIO(new ArrayInput([]), new NullOutput());
            $this->assertFalse($app->has('completion'));
            $this->assertFalse($app->has('backup:list'));
            $this->assertFalse($app->has('snapshots'));
            $this->assertTrue($app->has('backup:get'));
        } finally {
            unlink($file);
        }
    }

    private function load(Command $command): Command
    {
        return $command instanceof LazyCommand ? $command->getCommand() : $command;
    }
}
