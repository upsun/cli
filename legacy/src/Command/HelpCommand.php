<?php

declare(strict_types=1);

/**
 * @file
 * Override Symfony Console's HelpCommand to customize the appearance of help.
 */

namespace Platformsh\Cli\Command;

use Platformsh\Cli\Console\CustomJsonDescriptor;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Console\CustomMarkdownDescriptor;
use Platformsh\Cli\Console\CustomTextDescriptor;
use Symfony\Component\Console\Application;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Exception\CommandNotFoundException;
use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

class HelpCommand extends CommandBase
{
    private ?Command $command = null;

    public function __construct(private readonly Config $config)
    {
        parent::__construct();
        parent::setConfig($this->config);
    }

    public function setCommand(Command $command): void
    {
        $this->command = $command;
    }

    protected function configure(): void
    {
        $this->ignoreValidationErrors();

        $this->setName('help')
            ->setDescription('Displays help for a command')
            ->setDefinition([
                new InputArgument('command_name', InputArgument::OPTIONAL, 'The command name', 'help'),
                new InputOption('format', null, InputOption::VALUE_REQUIRED, 'The output format (txt, json, or md)', 'txt'),
                new InputOption('raw', null, InputOption::VALUE_NONE, 'To output raw command help'),
            ])
            ->setHelp(
                <<<'EOF'
                    The <info>%command.name%</info> command displays help for a given command:

                      <info>%command.full_name% list</info>

                    You can also output the help in other formats by using the <comment>--format</comment> option:

                      <info>%command.full_name% --format=json list</info>

                    To display the list of available commands, please use the <info>list</info> command.
                    EOF,
            );
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $application = $this->getApplication();
        if ($application !== null && ($namespace = $this->describableNamespace($application, $input)) !== null) {
            return $this->listNamespace($application, $namespace, $input, $output);
        }

        $command = $this->command ?: $this->getApplication()->find($input->getArgument('command_name'));

        $format = $input->getOption('format');
        $options = ['format' => $format, 'raw_text' => $input->getOption('raw'), 'all' => true];

        switch ($format) {
            case 'md':
                (new CustomMarkdownDescriptor($this->config->getStr('application.executable')))->describe($output, $command, $options);
                return 0;
            case 'json':
                (new CustomJsonDescriptor())->describe($output, $command, $options);
                return 0;
            case 'txt':
                (new CustomTextDescriptor($this->config->getStr('application.executable')))->describe($output, $command, $options);
                return 0;
        }

        $originalCommand = $this->getApplication()->find($input->getFirstArgument());
        if ($originalCommand->getName() !== 'help' && $originalCommand->getDefinition()->hasOption('format')) {
            // If the --format is unrecognised, it might be because the
            // command has its own --format option. Fall back to plain text
            // help.
            (new CustomTextDescriptor($this->config->getStr('application.executable')))->describe($output, $command, $options);
            return 0;
        }

        throw new InvalidArgumentException(sprintf('Unsupported format "%s".', $format));
    }

    /**
     * Returns the namespace to describe, if help was asked for one instead of a command.
     *
     * Without this, asking for help on a namespace only reports that the name is
     * ambiguous, and the suggestions it lists include hidden commands.
     */
    private function describableNamespace(Application $application, InputInterface $input): ?string
    {
        $name = $input->getArgument('command_name');
        if ($this->command !== null || !is_string($name) || $name === '') {
            return null;
        }

        try {
            // A command, or an abbreviation of one: describe that, as before.
            // This is checked before findNamespace(), which resolves every
            // command to collect the namespaces.
            $application->find($name);

            return null;
        } catch (CommandNotFoundException) {
            // Not a command. It may still be a namespace.
        }

        try {
            return $application->findNamespace($name);
        } catch (CommandNotFoundException) {
            // Neither a command nor a namespace: let the caller report the error.
            return null;
        }
    }

    /**
     * Lists the commands of a namespace, as the list command does.
     */
    private function listNamespace(Application $application, string $namespace, InputInterface $input, OutputInterface $output): int
    {
        $listInput = new ArrayInput([
            'command' => 'list',
            'namespace' => $namespace,
            '--format' => $input->getOption('format'),
            '--raw' => $input->getOption('raw'),
        ]);
        $listInput->setInteractive(false);

        return $application->get('list')->run($listInput, $output);
    }
}
