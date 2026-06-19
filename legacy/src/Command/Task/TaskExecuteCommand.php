<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Task;

use GuzzleHttp\Utils;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Model\Variable;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Client\Model\Activity;
use Platformsh\Client\Model\Result;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'task:execute', description: 'Execute a task on an environment')]
class TaskExecuteCommand extends CommandBase
{
    public function __construct(private readonly Api $api, private readonly Config $config, private readonly Selector $selector)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addArgument('task', InputArgument::REQUIRED, 'The name of the task to execute')
            ->addOption('variable', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'A variable to set when running the task, in the format <info>type:name=value</info>');

        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);

        $this->addExample('Execute the "migrate" task on the environment "main"', 'migrate --environment main');
        $this->addExample('Execute the "migrate" task, setting environment variable FOO=bar', 'migrate -e main --variable env:FOO=bar');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $selection = $this->selector->getSelection($input);
        $environment = $selection->getEnvironment();

        $taskName = $input->getArgument('task');
        $variables = $this->parseVariables($input->getOption('variable'));

        $this->stdErr->writeln(sprintf(
            'Executing task <info>%s</info> on the environment %s',
            $taskName,
            $this->api->getEnvironmentLabel($environment),
        ));

        $url = $environment->getUri() . '/tasks/' . rawurlencode($taskName) . '/run';
        $response = $this->api->getHttpClient()->request('POST', $url, ['json' => ['variables' => (object) $variables]]);

        $result = new Result(
            (array) Utils::jsonDecode((string) $response->getBody(), true),
            $environment->getUri(),
            $this->api->getHttpClient(),
            Activity::class,
        );
        $activities = $result->getActivities();

        $this->stdErr->writeln('');
        $this->stdErr->writeln('The task has been triggered.');

        $executable = $this->config->getStr('application.executable');
        if ($activities !== []) {
            // Reference the exact activity ID so the log can be followed even
            // when several activities are running in parallel.
            $activity = reset($activities);
            $this->stdErr->writeln(sprintf(
                'To follow its log, run: <info>%s activity:log %s</info>',
                $executable,
                $activity->id,
            ));
        } else {
            $this->stdErr->writeln(sprintf(
                'To follow its log, run: <info>%s activity:log --type environment.task -e %s</info>',
                $executable,
                $environment->id,
            ));
        }

        return 0;
    }

    /**
     * Parses variables in the format type:name=value into a nested array.
     *
     * @param string[] $variables
     *
     * @return array<string, array<string, string>>
     */
    private function parseVariables(array $variables): array
    {
        $map = [];
        $variable = new Variable();
        foreach ($variables as $var) {
            [$type, $name, $value] = $variable->parse($var);
            $map[$type][$name] = $value;
        }

        return $map;
    }
}
