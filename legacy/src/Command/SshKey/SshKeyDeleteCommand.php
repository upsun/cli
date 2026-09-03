<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\SshKey;

use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Cli\Service\SshConfig;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'ssh-key:delete', description: 'Delete an SSH key')]
class SshKeyDeleteCommand extends SshKeyCommandBase
{
    public function __construct(private readonly Api $api, private readonly Config $config, private readonly QuestionHelper $questionHelper, private readonly SshConfig $sshConfig)
    {
        parent::__construct();
    }
    protected function configure(): void
    {
        $this
            ->addArgument(
                'id',
                InputArgument::OPTIONAL,
                'The ID of the SSH key to delete',
            );
        $this->addExample('Delete the key with the given ID', '01JX7Q8YV0N4W2S6TRK3M9BAEC');
        $help = 'This command lets you delete SSH keys from your account.'
            . "\n\n" . $this->certificateNotice($this->config);
        $this->setHelp($help);
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $id = $input->getArgument('id');
        if (empty($id) && $input->isInteractive()) {
            $keys = $this->api->getSshKeys(true);
            if (empty($keys)) {
                $this->stdErr->writeln('You do not have any SSH keys in your account.');
                return 1;
            }
            $options = [];
            foreach ($keys as $key) {
                $options[$key->id] = sprintf('%s (%s)', $key->id, $key->label ?: $key->sha256);
            }
            $id = $this->questionHelper->choose($options, 'Enter a number to choose a key to delete:', null, false);
        }
        if (empty($id)) {
            $this->stdErr->writeln('<error>You must specify the ID of the SSH key to delete.</error>');
            $this->stdErr->writeln('');
            $this->stdErr->writeln(
                'List your SSH keys with: <comment>' . $this->config->getStr('application.executable') . ' ssh-keys</comment>',
            );

            return 1;
        }

        $key = $this->api->getSshKey((string) $id);
        if (!$key) {
            $this->stdErr->writeln("SSH key not found: <error>$id</error>");

            return 1;
        }

        $this->api->deleteSshKey($key->id);

        $this->stdErr->writeln(sprintf(
            'The SSH key <info>%s</info> has been deleted from your %s account.',
            $id,
            $this->config->getStr('service.name'),
        ));

        // Reset and warm the SSH keys cache.
        try {
            $this->api->getSshKeys(true);
        } catch (\Exception) {
            // Suppress exceptions; we do not need the result of this call.
        }
        $this->sshConfig->configureSessionSsh();

        return 0;
    }
}
