<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Exception\LogicException;

/**
 * Registers hidden aliases so that lazily-loaded commands can be found by them.
 *
 * It also lists all hidden aliases in a container parameter, for
 * HiddenAliasesCommandLoader.
 *
 * This must run before Symfony's AddConsoleCommandPass, which reads extra
 * "console.command" tags as aliases.
 *
 * @see HiddenAliases
 */
final class HiddenAliasesPass implements CompilerPassInterface
{
    public const PARAMETER = 'cli.hidden_aliases';

    public function process(ContainerBuilder $container): void
    {
        $all = [];
        $names = [];
        foreach (array_keys($container->findTaggedServiceIds('console.command')) as $id) {
            $definition = $container->getDefinition($id);
            $class = $definition->getClass();
            $reflection = $class !== null ? $container->getReflectionClass($class, false) : null;
            $asCommand = $reflection?->getAttributes(AsCommand::class)[0] ?? null;
            if ($asCommand !== null) {
                $names = array_merge($names, explode('|', $asCommand->newInstance()->name));
            }
            $attribute = $reflection?->getAttributes(HiddenAliases::class)[0] ?? null;
            if ($attribute === null) {
                continue;
            }
            foreach ($attribute->newInstance()->aliases as $alias) {
                $definition->addTag('console.command', ['command' => $alias]);
                $all[] = $alias;
            }
        }
        if ($clashes = array_unique(array_merge(array_intersect($all, $names), array_diff_assoc($all, array_unique($all))))) {
            throw new LogicException('Hidden aliases clash with other command names or aliases: ' . implode(', ', $clashes));
        }
        $container->setParameter(self::PARAMETER, $all);
    }
}
