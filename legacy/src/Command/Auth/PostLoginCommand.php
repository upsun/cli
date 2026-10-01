<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Auth;

use Doctrine\Common\Cache\CacheProvider;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Login;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Filesystem\Filesystem;

/**
 * Completes a login made by the Go wrapper: SSH host keys, certificate and config, and the account summary.
 */
#[AsCommand(name: 'auth:post-login', description: 'Complete a login (internal)')]
class PostLoginCommand extends CommandBase
{
    protected bool $hiddenInList = true;

    public function __construct(private readonly CacheProvider $cache, private readonly Config $config, private readonly Login $login)
    {
        parent::__construct();
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        // Clear state from the previous login, including its SSH certificate.
        $this->cache->flushAll();
        (new Filesystem())->remove($this->config->getSessionDir(true));

        $this->login->finalize();

        return 0;
    }
}
