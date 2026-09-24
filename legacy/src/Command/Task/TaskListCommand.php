<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Task;

use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Table;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'task:list', description: 'Get a list of tasks on an environment', aliases: ['tasks'])]
class TaskListCommand extends CommandBase
{
    /** @var array<string, string> */
    private array $tableHeader = [
        'name' => 'Name',
        'type' => 'Type',
        'command' => 'Command',
        'timeout' => 'Timeout (s)',
    ];

    public function __construct(private readonly Api $api, private readonly Selector $selector, private readonly Table $table)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        Table::configureInput($this->getDefinition(), $this->tableHeader);
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $selection = $this->selector->getSelection($input);
        $environment = $selection->getEnvironment();

        $tasks = $this->api->getEnvironmentTasks($environment);

        if ($tasks === []) {
            $this->stdErr->writeln(sprintf(
                'No tasks were found on the environment %s.',
                $this->api->getEnvironmentLabel($environment),
            ));

            return 0;
        }

        $rows = [];
        foreach ($tasks as $name => $task) {
            $rows[] = [
                'name' => $name,
                'type' => $task['type'] ?? '',
                'command' => isset($task['run']['command']) ? trim((string) $task['run']['command']) : '',
                'timeout' => $task['run']['timeout'] ?? '',
            ];
        }

        if (!$this->table->formatIsMachineReadable()) {
            $this->stdErr->writeln(sprintf(
                'Tasks on the environment %s:',
                $this->api->getEnvironmentLabel($environment),
            ));
        }

        $this->table->render($rows, $this->tableHeader);

        return 0;
    }
}
