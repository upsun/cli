<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Auth;

use Doctrine\Common\Cache\CacheProvider;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Console\ArrayArgument;
use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\SshConfig;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Filesystem\Filesystem;

/**
 * Cleans up after the Go wrapper logs out of sessions: the API cache, SSH certificates and SSH config.
 */
#[AsCommand(name: 'auth:post-logout', description: 'Clean up after a logout (internal)')]
class PostLogoutCommand extends CommandBase
{
    protected bool $hiddenInList = true;

    public function __construct(private readonly CacheProvider $cache, private readonly Config $config, private readonly SshConfig $sshConfig)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->addArgument('session-ids', InputArgument::IS_ARRAY, 'The IDs of the sessions that were logged out');
        $this->addOption('all', null, InputOption::VALUE_NONE, 'Delete the files of all sessions');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $this->cache->flushAll();

        $fs = new Filesystem();
        foreach (ArrayArgument::getArgument($input, 'session-ids') as $id) {
            $this->config->validateSessionId($id);
            if ($id === $this->config->getSessionId()) {
                $this->sshConfig->deleteSessionConfiguration();
            }
            $fs->remove($this->config->getSessionDir() . DIRECTORY_SEPARATOR . 'sess-cli-' . $id);
        }
        if (Option::bool($input, 'all')) {
            $fs->remove($this->config->getSessionDir());
        }

        return 0;
    }
}
