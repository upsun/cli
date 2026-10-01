<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\SshCert\Certifier;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;

readonly class Login
{
    private OutputInterface $stdErr;

    public function __construct(
        private Certifier      $certifier,
        private Config         $config,
        private QuestionHelper $questionHelper,
        private SshConfig      $sshConfig,
        OutputInterface        $output,
    ) {
        $this->stdErr = $output instanceof ConsoleOutputInterface ? $output->getErrorOutput() : $output;
    }

    /**
     * Sets up SSH after a login: host keys, a certificate, and SSH configuration.
     */
    public function finalize(): void
    {
        // Configure SSH host keys.
        $this->sshConfig->configureHostKeys();

        // Generate a new certificate from the certifier API.
        if ($this->certifier->isAutoLoadEnabled() && $this->sshConfig->checkRequiredVersion()) {
            $this->stdErr->writeln('');
            $this->stdErr->writeln('Generating SSH certificate...');
            try {
                $this->certifier->generateCertificate(null);
                $this->stdErr->writeln('A new SSH certificate has been generated.');
                $this->stdErr->writeln('It will be automatically refreshed when necessary.');
            } catch (\Exception $e) {
                $this->stdErr->writeln('Failed to generate SSH certificate: <error>' . $e->getMessage() . '</error>');
            }
        }

        // Write SSH configuration.
        if ($this->sshConfig->configureSessionSsh()) {
            $this->sshConfig->addUserSshConfig($this->questionHelper);
        }
    }

    /**
     * Get help on how to use API tokens non-interactively.
     *
     * @param string $tag
     *
     * @return string
     */
    public function getNonInteractiveAuthHelp(string $tag = 'info'): string
    {
        $prefix = $this->config->getStr('application.env_prefix');

        return "To authenticate non-interactively, configure an API token using the <$tag>{$prefix}TOKEN</$tag> environment variable.";
    }
}
