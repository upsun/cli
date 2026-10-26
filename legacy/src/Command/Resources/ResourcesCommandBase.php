<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Resources;

use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\ResourcesUtil;
use Symfony\Contracts\Service\Attribute\Required;

/**
 * @phpstan-import-type ProfileSize from ResourcesUtil
 * @phpstan-import-type ContainerProfiles from ResourcesUtil
 */
class ResourcesCommandBase extends CommandBase
{
    private Config $config;

    #[Required]
    public function autowire(Config $config): void
    {
        $this->config = $config;
    }

    public function isHidden(): bool
    {
        return !$this->config->getBool('api.sizing') || parent::isHidden();
    }

    /**
     * Format CPU Type.
     *
     * @param ProfileSize $sizeInfo
     */
    protected function formatCPUType(array $sizeInfo): string
    {
        return sprintf('(%s)', $sizeInfo['cpu_type']);
    }

    /**
     * Sort container profiles by size.
     *
     * @param ContainerProfiles $profiles
     *
     * @return ContainerProfiles
     */
    protected function sortContainerProfiles(array $profiles): array
    {
        foreach ($profiles as &$profile) {
            uasort($profile, fn(array $a, array $b): int => $a['cpu'] <=> $b['cpu']);
        }

        return $profiles;
    }
}
