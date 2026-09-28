<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Console;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Console\HiddenAliases;
use Platformsh\Cli\Console\HiddenAliasesPass;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Exception\LogicException;

#[AsCommand(name: 'foo:list|foos')]
#[HiddenAliases(['foo:old'])]
class HiddenAliasesPassFooCommand extends Command {}

#[AsCommand(name: 'bar:list')]
#[HiddenAliases(['foos'])]
class HiddenAliasesPassBarCommand extends Command {}

class HiddenAliasesPassTest extends TestCase
{
    public function testParameter(): void
    {
        $container = $this->container([HiddenAliasesPassFooCommand::class]);
        (new HiddenAliasesPass())->process($container);
        $this->assertSame(['foo:old'], $container->getParameter(HiddenAliasesPass::PARAMETER));
    }

    public function testClash(): void
    {
        $container = $this->container([HiddenAliasesPassFooCommand::class, HiddenAliasesPassBarCommand::class]);
        $this->expectException(LogicException::class);
        $this->expectExceptionMessage('foos');
        (new HiddenAliasesPass())->process($container);
    }

    /**
     * @param class-string[] $classes
     */
    private function container(array $classes): ContainerBuilder
    {
        $container = new ContainerBuilder();
        foreach ($classes as $class) {
            $container->register($class, $class)->addTag('console.command');
        }

        return $container;
    }
}
