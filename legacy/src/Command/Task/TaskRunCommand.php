<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Task;

use GuzzleHttp\Exception\BadResponseException;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Model\Variable;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\ActivityMonitor;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Client\Exception\ApiResponseException;
use Platformsh\Client\Model\Activity;
use Platformsh\Client\Model\Result;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'task:run', description: 'Execute a task on an environment')]
class TaskRunCommand extends CommandBase
{
    public function __construct(private readonly ActivityMonitor $activityMonitor, private readonly Api $api, private readonly Config $config, private readonly QuestionHelper $questionHelper, private readonly Selector $selector)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addArgument('task', InputArgument::REQUIRED, 'The name of the task to execute')
            ->addOption('variable', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'A variable to set when running the task, in the format <info>type:name=value</info>')
            // Tasks can run for a long time, so waiting is opt-in rather than the default.
            ->addOption('wait', null, InputOption::VALUE_NONE, 'Wait for the task to complete');

        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->addCompleter($this->selector);

        $this->addExample('Run the "migrate" task on the environment "main"', 'migrate --environment main');
        $this->addExample('Run the "migrate" task, setting environment variable FOO=bar', 'migrate -e main --variable env:FOO=bar');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $selection = $this->selector->getSelection($input);
        $environment = $selection->getEnvironment();

        $taskName = $input->getArgument('task');
        $variables = (new Variable())->parseMultiple($input->getOption('variable'));

        $tasks = $this->api->getEnvironmentTasks($environment);
        if (!isset($tasks[$taskName])) {
            if ($tasks === []) {
                $this->stdErr->writeln(sprintf(
                    'No tasks were found on the environment %s.',
                    $this->api->getEnvironmentLabel($environment, 'comment'),
                ));

                return 1;
            }

            $this->stdErr->writeln(sprintf(
                'The task <error>%s</error> was not found on the environment %s.',
                $taskName,
                $this->api->getEnvironmentLabel($environment, 'comment'),
            ));
            $this->stdErr->writeln('');
            $this->stdErr->writeln(sprintf(
                'To list tasks, run: <comment>%s tasks</comment>',
                $this->config->getStr('application.executable'),
            ));

            return 1;
        }

        if ($environment->type === 'production' && !$this->questionHelper->confirm(sprintf(
            'Are you sure you want to run the task <comment>%s</comment> on the production environment %s?',
            $taskName,
            $this->api->getEnvironmentLabel($environment, 'comment'),
        ))) {
            return 1;
        }

        $this->stdErr->writeln(sprintf(
            'Executing task <info>%s</info> on the environment %s',
            $taskName,
            $this->api->getEnvironmentLabel($environment),
        ));

        $url = $environment->getUri() . '/tasks/' . rawurlencode($taskName) . '/run';
        try {
            $response = $this->api->getHttpClient()->request('POST', $url, ['json' => ['variables' => (object) $variables]]);
        } catch (BadResponseException $e) {
            throw ApiResponseException::create($e->getRequest(), $e->getResponse(), $e);
        }

        $result = new Result(
            (array) json_decode((string) $response->getBody(), true, 512, JSON_THROW_ON_ERROR),
            $environment->getUri(),
            $this->api->getHttpClient(),
            Activity::class,
        );
        $activities = $result->getActivities();

        $this->stdErr->writeln('');
        $this->stdErr->writeln('The task has been triggered.');

        // Waiting is opt-in so the exit code can reflect a failed activity, e.g. in CI.
        if ($input->getOption('wait') && $activities !== []) {
            $success = $this->activityMonitor->waitMultiple($activities, $selection->getProject());
            return $success ? 0 : 1;
        }

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
}
