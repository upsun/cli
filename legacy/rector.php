<?php

declare(strict_types=1);

use Platformsh\Cli\Rector\InjectCommandServicesRector;
use Platformsh\Cli\Rector\NewServicesRector;
use Platformsh\Cli\Rector\UnnecessaryServiceVariablesRector;
use Platformsh\Cli\Rector\UseSelectorServiceRector;
use Rector\Config\RectorConfig;
use Rector\Symfony\Symfony61\Rector\Class_\CommandConfigureToAttributeRector;

return RectorConfig::configure()
    ->withPaths([
        __DIR__ . '/src',
        __DIR__ . '/tests',
    ])
    ->withRules([
        CommandConfigureToAttributeRector::class,
        InjectCommandServicesRector::class,
        UseSelectorServiceRector::class,
        NewServicesRector::class,
        UnnecessaryServiceVariablesRector::class,
    ])
    ->withImportNames(importShortClasses: false, removeUnusedImports: true)
;
