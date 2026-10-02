<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Auth;

use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Service\Login;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * Sets up SSH after a login made by the Go wrapper: host keys, a certificate, and SSH configuration.
 */
#[AsCommand(name: 'auth:post-login', description: 'Set up SSH after a login (internal)', hidden: true)]
class PostLoginCommand extends CommandBase
{
    // The attribute's "hidden" stops abbreviations from matching, and this hides the command once it is loaded.
    protected bool $hiddenInList = true;

    public function __construct(private readonly Login $login)
    {
        parent::__construct();
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $this->login->finalize();

        return 0;
    }
}
