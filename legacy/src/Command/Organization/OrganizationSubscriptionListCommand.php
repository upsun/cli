<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Organization;

use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Console\ProgressMessage;
use Platformsh\Cli\Service\Table;
use Platformsh\Cli\Util\PaginationUtil;
use Platformsh\Client\Model\Organization\Project as OrgProject;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Platformsh\Cli\Console\HiddenAliases;

#[AsCommand(name: 'organization:subscription:list', description: 'List projects and their subscriptions within an organization', aliases: ['org:subs', 'org:projects'])]
#[HiddenAliases(['organization:subscriptions', 'organization:projects'])]
class OrganizationSubscriptionListCommand extends OrganizationCommandBase
{
    /** The maximum page size allowed by the API. */
    public const MAX_COUNT = 100;

    /** @var array<string, string> */
    private array $tableHeader = [
        'id' => 'Subscription ID',
        'project_id' => 'Project ID',
        'project_title' => 'Title',
        'project_region' => 'Region',
        'status' => 'Status',
        'created_at' => 'Created at',
        'updated_at' => 'Updated at',
    ];

    /** @var string[] */
    private array $defaultColumns = ['id', 'project_id', 'project_title', 'project_region'];

    public function __construct(private readonly Api $api, private readonly Config $config, private readonly Selector $selector, private readonly Table $table)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addOption('page', null, InputOption::VALUE_REQUIRED, 'Page number. This enables pagination, despite the configuration or --count 0.')
            ->addOption('count', 'c', InputOption::VALUE_REQUIRED, 'The number of items to display per page (max: ' . self::MAX_COUNT . '). Use 0 to disable pagination.');
        $this->selector->addOrganizationOptions($this->getDefinition(), true);
        $this->addCompleter($this->selector);
        Table::configureInput($this->getDefinition(), $this->tableHeader, $this->defaultColumns);
    }

    /**
     * {@inheritdoc}
     */
    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $options = [];
        $options['query']['filter[status][in]'] = 'active,suspended';

        $count = Option::stringOrNull($input, 'count');
        $itemsPerPage = max(1, min($this->config->getInt('pagination.count'), self::MAX_COUNT));
        if ($count !== null && $count !== '0') {
            if (!\ctype_digit($count) || $count < 1 || $count > self::MAX_COUNT) {
                $this->stdErr->writeln('The --count must be a number between 1 and ' . self::MAX_COUNT . ', or 0 to disable pagination.');
                return 1;
            }
            $itemsPerPage = (int) $count;
        }

        $fetchAllPages = !$this->config->getBool('pagination.enabled');
        if ($count === '0') {
            $fetchAllPages = true;
            $itemsPerPage = self::MAX_COUNT;
        }
        $options['query']['page[size]'] = $itemsPerPage;

        $requestedPage = 1;
        if (($pageOption = Option::stringOrNull($input, 'page')) !== null) {
            if (!\ctype_digit($pageOption) || $pageOption < 1) {
                $this->stdErr->writeln('The --page must be a number greater than 0.');
                return 1;
            }
            $requestedPage = (int) $pageOption;
            $fetchAllPages = false;
        }

        $organization = $this->selector->selectOrganization($input);

        $httpClient = $this->api->getHttpClient();
        $projects = [];
        $url = '/organizations/' . rawurlencode($organization->id) . '/projects';
        $pageNumber = 1;
        $hasNextPage = false;
        $progress = new ProgressMessage($output);
        while (true) {
            $progress->showIfOutputDecorated(\sprintf('Loading projects (page %d)...', $pageNumber));
            $result = OrgProject::getCollectionWithParent($url, $httpClient, $options);
            $progress->done();
            // The API paginates with a cursor, so pages before the requested
            // one have to be fetched, and discarded, to reach it.
            if ($pageNumber >= $requestedPage) {
                $projects = \array_merge($projects, $result['items']);
            }
            $nextPage = PaginationUtil::nextPage($result['collection']->getNextPageUrl(), $url, $options['query']);
            $hasNextPage = $nextPage !== null;
            if (!$hasNextPage || (!$fetchAllPages && $pageNumber >= $requestedPage)) {
                break;
            }
            [$url, $options['query']] = $nextPage;
            $pageNumber++;
        }

        if (empty($projects)) {
            if ($requestedPage > 1) {
                $this->stdErr->writeln('No projects were found on this page.');
                return 0;
            }
            $this->stdErr->writeln(\sprintf('No projects were found belonging to the organization %s.', $this->api->getOrganizationLabel($organization)));
            return 0;
        }

        $rows = [];
        foreach ($projects as $project) {
            $rows[] = [
                'id' => $project->subscription_id,
                'project_id' => $project->id,
                'project_title' => $project->title,
                'project_region' => $project->region,
                'status' => $project->status,
                'created_at' => $project->created_at,
                'updated_at' => $project->updated_at,
            ];
        }

        if (!$this->table->formatIsMachineReadable()) {
            $title = \sprintf('Projects belonging to the organization <info>%s</info>', $this->api->getOrganizationLabel($organization));
            if (($pageNumber > 1 || $hasNextPage) && !$fetchAllPages) {
                $title .= \sprintf(' (page %d)', $pageNumber);
            }
            $this->stdErr->writeln($title);
        }

        $this->table->render($rows, $this->tableHeader, $this->defaultColumns);

        if (!$this->table->formatIsMachineReadable() && $hasNextPage) {
            $this->stdErr->writeln(\sprintf('More projects are available on the next page (<info>--page %d</info>)', $pageNumber + 1));
            $this->stdErr->writeln('List all items with: <info>--count 0</info> (<info>-c0</info>)');
        }

        return 0;
    }
}
