<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Environment;

use Platformsh\Cli\Console\Argument;
use Platformsh\Cli\Console\ArrayArgument;
use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Selector\Selection;
use Platformsh\Cli\Selector\SelectorConfig;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\Observability;
use Platformsh\Cli\Selector\Selector;
use Doctrine\Common\Cache\CacheProvider;
use GuzzleHttp\Exception\RequestException;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Util\OsUtil;
use Platformsh\Cli\Util\StringUtil;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Formatter\OutputFormatter;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Platformsh\Cli\Console\HiddenAliases;

/**
 * @phpstan-type LogRow array{cursor: string, datetime: string, timestamp: int, severity: string, service: string, log_kind: string, content: string, raw: array<mixed>}
 */
#[AsCommand(name: 'environment:logs', description: "Read an environment's logs", aliases: ['log'])]
#[HiddenAliases(['logs'])]
class EnvironmentLogCommand extends CommandBase
{
    // Log types mapped to the API's log kinds.
    private const LOG_KINDS = [
        'access' => 'access',
        'app' => 'application',
        'application' => 'application',
        'cron' => 'cron',
        'deploy' => 'deployment',
        'deployment' => 'deployment',
        'post-deploy' => 'deployment',
        'platform' => 'platform',
    ];

    // Severities, from most to least severe.
    private const SEVERITIES = ['EMERGENCY', 'ALERT', 'CRITICAL', 'ERROR', 'WARNING', 'NOTICE', 'INFO', 'DEBUG'];

    // Options that only work with the logs API.
    private const API_OPTIONS = ['service', 'severity', 'since', 'until', 'fields'];

    private const FORMATS = ['text', 'raw', 'json'];

    private const PROTOCOLS = ['ssh', 'auto'];

    // Fields that can be displayed with --fields, as well as "context.<key>".
    private const FIELDS = ['datetime', 'severity', 'service', 'unit', 'instance', 'host', 'command', 'container_id', 'container_image', 'log_kind', 'content', 'context'];

    // Limits for the "context" field, to avoid excessively long lines.
    private const MAX_CONTEXT_FIELDS = 8;
    private const MAX_CONTEXT_VALUE_LENGTH = 60;
    private const MAX_CONTEXT_LENGTH = 200;

    // Top-level keys of structured log lines that are not displayed in the "context" field.
    private const SKIP_CONTEXT_KEYS = [
        'time', 'timestamp', 'ts', '@timestamp', 'datetime',
        'level', 'level_name', 'lvl', 'severity',
        'msg', 'message',
        'trace_id', 'traceid', 'span_id', 'spanid',
    ];

    // How far behind the current time to read when tailing, so that late-arriving logs are not skipped.
    private const TAIL_DELAY = 15;

    private const TAIL_INTERVAL = 5;

    // Limits for the time windows queried by fetchRecent().
    private const MIN_WINDOW = 60;
    private const MAX_WINDOW = 7 * 86400;
    private const WINDOW_GROWTH = 4;

    public function __construct(
        private readonly CacheProvider $cacheProvider,
        private readonly Config $config,
        private readonly Io $io,
        private readonly Observability $observability,
        private readonly QuestionHelper $questionHelper,
        private readonly Selector $selector,
    ) {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addArgument('type', InputArgument::OPTIONAL, 'The log type: "access", "app", "cron", "deploy", "error" or "platform"', null, [
                'access',
                'app',
                'cron',
                'deploy',
                'error',
                'platform',
            ])
            ->addOption('lines', null, InputOption::VALUE_REQUIRED, 'The number of lines to show', 100)
            ->addOption('tail', null, InputOption::VALUE_NONE, 'Continuously tail the log')
            ->addOption('service', 's', InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Filter by service or application name. ' . ArrayArgument::SPLIT_HELP)
            ->addOption('severity', null, InputOption::VALUE_REQUIRED, 'The minimum severity, e.g. "error" or "warning"', null, array_map('strtolower', self::SEVERITIES))
            ->addOption('since', null, InputOption::VALUE_REQUIRED, 'Show logs since this time, e.g. "30m", "2h", "1d", or a date/time')
            ->addOption('until', null, InputOption::VALUE_REQUIRED, 'Show logs until this time, in the same format as --since')
            ->addOption('format', null, InputOption::VALUE_REQUIRED, 'The output format: "text", "raw" (message only), or "json" (one object per line)', 'text', self::FORMATS)
            ->addOption('fields', null, InputOption::VALUE_REQUIRED, 'The fields to display in the text format, e.g. "datetime,service,content,context.status". ' . ArrayArgument::SPLIT_HELP, null, self::FIELDS);
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->selector->addRemoteContainerOptions($this->getDefinition());
        $this->selector->addTaskOption($this->getDefinition());
        $this->addCompleter($this->selector);
        $envVar = $this->config->getStr('application.env_prefix') . 'LOG_PROTOCOL';
        $this->setHelp(<<<EOF
            By default, logs are read over SSH, from files in /var/log.
            The type is the name of a log file (without the .log extension).

            To read logs from the Observability API instead, set the log protocol to "auto",
            using the <comment>$envVar</comment> environment variable, or the api.log_protocol config option.
            The API is then used where it is available for the environment, and SSH otherwise.

            With the API:
              The <comment>error</comment> type shows logs of any type with a severity of ERROR or higher.
              Without a type, all logs are shown except for "platform" logs.
              For structured (JSON) log lines, the text format also shows some of the line's fields as key=value pairs.
              Use --fields to choose the fields, or --format json to see everything.
              Logs take a few seconds to become available, so --tail shows them with a delay of about 15-20 seconds.
              SSH is still used if the --worker, --instance or --task option is used,
              or if the type is not listed above or ends in .log (e.g. "error.log").
            EOF);
        $this->addExample('Display a choice of logs that can be read');
        $this->addExample('Read the deploy log', 'deploy');
        $this->addExample('Read the access log continuously', 'access --tail');
        $this->addExample('Read the last 500 lines of the cron log', 'cron --lines 500');
        $this->addExample('Read warnings and errors from the "app" service in the last hour (with the API)', '--service app --severity warning --since 1h');
        $this->addExample('Read HTTP request details from structured logs (with the API)', 'app --fields datetime,service,content,context.method,context.path,context.status');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $selection = $this->selector->getSelection($input, new SelectorConfig(
            chooseEnvFilter: SelectorConfig::filterEnvsMaybeActive(),
            selectRemoteContainer: false,
        ));

        if (Option::bool($input, 'tail') && $this->runningViaMulti) {
            throw new InvalidArgumentException('The --tail option cannot be used with "multi"');
        }
        $lines = Option::int($input, 'lines');
        if ($lines < 1) {
            throw new InvalidArgumentException('The --lines value must be at least 1');
        }
        $format = Option::string($input, 'format');
        if (!in_array($format, self::FORMATS, true)) {
            throw new InvalidArgumentException(sprintf('Invalid --format: %s (expected one of: %s)', $format, implode(', ', self::FORMATS)));
        }
        $fields = $this->parseFields($input, $format);

        $protocol = $this->config->getStr('api.log_protocol');
        if (!in_array($protocol, self::PROTOCOLS, true)) {
            throw new InvalidArgumentException(sprintf('Invalid log protocol: %s (expected one of: %s)', $protocol, implode(', ', self::PROTOCOLS)));
        }

        $logType = Argument::stringOrNull($input, 'type');
        $sshReason = $protocol === 'ssh'
            ? sprintf('the log protocol is "ssh" (set %sLOG_PROTOCOL=auto to use the logs API)', $this->config->getStr('application.env_prefix'))
            : $this->sshReason($input, $logType);
        if ($logType !== null && str_ends_with($logType, '.log')) {
            $logType = substr($logType, 0, -4);
        }

        $entrypoint = null;
        if ($sshReason === null) {
            $entrypoint = $this->observability->getEntrypoint($selection->getEnvironment());
            if (Observability::getLink($entrypoint, 'logs_query') === null) {
                $sshReason = 'the logs API is not available for this environment';
            }
        }

        if ($sshReason === null) {
            return $this->readFromApi($input, $output, $selection, (array) $entrypoint, $logType, $lines, $format, $fields);
        }

        $this->io->debug('Reading logs via SSH, because ' . $sshReason);
        foreach (self::API_OPTIONS as $option) {
            $used = $option === 'service' ? ArrayArgument::getOption($input, $option) !== [] : $input->getOption($option) !== null;
            if ($used) {
                throw new InvalidArgumentException(sprintf('The --%s option cannot be used: %s', $option, $sshReason));
            }
        }
        if ($format !== 'text') {
            throw new InvalidArgumentException(sprintf('The --format option cannot be used: %s', $sshReason));
        }

        return $this->readViaSsh($input, $this->selector->withRemoteContainer($input, $selection), $logType, $lines);
    }

    /**
     * Returns why the logs must be read over SSH, or null if the API can be used.
     */
    private function sshReason(InputInterface $input, ?string $logType): ?string
    {
        foreach (['worker', 'instance', 'task'] as $option) {
            if (Option::stringOrNull($input, $option) !== null) {
                return sprintf('the --%s option is used', $option);
            }
        }
        if ($logType !== null && str_ends_with($logType, '.log')) {
            return 'a log file name was given';
        }
        if ($logType !== null && $logType !== 'error' && !isset(self::LOG_KINDS[$logType])) {
            return sprintf('the log type "%s" is not supported by the logs API', $logType);
        }

        return null;
    }

    /**
     * @param array<mixed> $entrypoint
     * @param list<string>|null $fields
     */
    private function readFromApi(InputInterface $input, OutputInterface $output, Selection $selection, array $entrypoint, ?string $logType, int $lines, string $format, ?array $fields): int
    {
        $tail = Option::bool($input, 'tail');
        if ($tail && $input->getOption('until') !== null) {
            throw new InvalidArgumentException('The --until option cannot be used with --tail');
        }

        $filters = $this->buildFilters($input, $selection, $logType);

        $now = time();
        $to = $this->parseTime($input, 'until', $now) ?? ($tail ? $now - self::TAIL_DELAY : $now);
        $from = $this->parseTime($input, 'since', $now) ?? $to - $this->defaultRange($entrypoint);
        // With --tail, a --since time within the tail delay is handled by polling.
        if ($from >= $to && (!$tail || $from > $now)) {
            throw new InvalidArgumentException($input->getOption('until') !== null
                ? 'The --since time must be before the --until time'
                : 'The --since time must be in the past');
        }

        $this->selector->ensurePrintedSelection($selection);

        $url = (string) Observability::getLink($entrypoint, 'logs_query');
        $fields ??= $logType === null || $logType === 'error'
            ? ['datetime', 'service', 'log_kind', 'severity', 'content', 'context']
            : ['datetime', 'service', 'severity', 'content', 'context'];

        // Fetch the most recent lines (newest first), and print them oldest first.
        $rows = $from < $to ? $this->fetchRecent($url, $filters, $from, $to, $lines, $this->initialWindow($entrypoint)) : [];
        $rows = array_reverse($rows);

        if ($rows === [] && !$tail) {
            $this->stdErr->writeln(sprintf('No logs found between %s and %s.', date('Y-m-d H:i:s T', $from), date('Y-m-d H:i:s T', $to)));

            return 0;
        }
        foreach ($rows as $row) {
            $this->printRow($output, $row, $format, $fields);
        }
        if (!$tail) {
            return 0;
        }

        // Poll for newer logs (oldest first), after the newest printed line.
        // Each poll starts a second before the previous one ended (the cursor prevents duplicates).
        $last = end($rows);
        $cursor = $last !== false ? $last['cursor'] : null;
        $from = max($from, $to - 1);
        while (true) { // @phpstan-ignore while.alwaysTrue
            sleep(self::TAIL_INTERVAL);
            // Limit the window, e.g. to catch up gradually after the computer was suspended.
            $to = min(time() - self::TAIL_DELAY, $from + self::MAX_WINDOW);
            if ($to <= $from + 1) {
                continue;
            }
            do {
                $page = $this->queryPage($url, $filters, $from, $to, 'ASC', $cursor);
                foreach ($page['data'] as $row) {
                    $this->printRow($output, $row, $format, $fields);
                }
                $cursor = $page['_cursor'] ?? $cursor;
            } while ($page['_has_more_results']);
            $from = $to - 1;
        }
    }

    /**
     * Fetches up to $lines of the most recent logs, newest first.
     *
     * Large time ranges can be rejected by the API, so this queries windows
     * of increasing size, moving back in time until enough lines are found.
     *
     * @param list<array{string, string}> $filters
     *
     * @return list<LogRow>
     */
    private function fetchRecent(string $url, array $filters, int $from, int $to, int $lines, int $window): array
    {
        $rows = [];
        $noCursor = 0;
        $end = $to;
        while ($end > $from && count($rows) < $lines) {
            $start = max($from, $end - $window);
            $cursor = null;
            do {
                try {
                    $page = $this->queryPage($url, $filters, $start, $end, 'DESC', $cursor);
                } catch (RequestException $e) {
                    // The API responds with 499 if a query would read too much data: retry with a smaller window.
                    if ($e->getResponse()?->getStatusCode() !== 499 || $cursor !== null || $end - $start <= self::MIN_WINDOW) {
                        throw $e;
                    }
                    $window = intdiv($end - $start, 2);
                    $this->io->debug(sprintf('Too much data: retrying with a window of %d seconds', $window));
                    continue 2;
                }
                foreach ($page['data'] as $row) {
                    // Windows overlap by a second, so skip duplicates.
                    $rows[$row['cursor'] !== '' ? $row['cursor'] : 'no-cursor-' . $noCursor++] ??= $row;
                }
                $cursor = $page['_cursor'];
            } while (count($rows) < $lines && $page['_has_more_results']);
            if ($start === $from) {
                break;
            }
            $end = $start + 1;
            $window = min($window * self::WINDOW_GROWTH, self::MAX_WINDOW);
        }

        return array_slice(array_values($rows), 0, $lines);
    }

    /**
     * Builds query parameters for filtering logs.
     *
     * @return list<array{string, string}> A list of key-value pairs.
     */
    private function buildFilters(InputInterface $input, Selection $selection, ?string $logType): array
    {
        $params = [];
        if ($logType === null || $logType === 'error') {
            $params[] = ['log_kinds[]', 'platform'];
            $params[] = ['log_kinds_mode', '-1'];
        } else {
            $params[] = ['log_kinds[]', self::LOG_KINDS[$logType]];
            $params[] = ['log_kinds_mode', '1'];
        }

        $severity = Option::stringOrNull($input, 'severity');
        if ($severity === null && $logType === 'error') {
            $severity = 'ERROR';
        }
        if ($severity !== null) {
            $index = array_search(strtoupper($severity), self::SEVERITIES, true);
            if ($index === false) {
                throw new InvalidArgumentException(sprintf('Invalid --severity: %s (expected one of: %s)', $severity, strtolower(implode(', ', self::SEVERITIES))));
            }
            foreach (array_slice(self::SEVERITIES, 0, $index + 1) as $s) {
                $params[] = ['severities[]', $s];
            }
            $params[] = ['severities_mode', '1'];
        }

        $services = ArrayArgument::getOption($input, 'service');
        // The app may also come from the project URL or an environment variable.
        if (($app = $selection->getAppName()) !== null) {
            $services[] = $app;
        }
        if ($services !== []) {
            foreach (array_unique($services) as $service) {
                $params[] = ['services[]', $service];
            }
            $params[] = ['services_mode', '1'];
        }

        return $params;
    }

    /**
     * Queries a page of logs.
     *
     * @param list<array{string, string}> $filters
     *
     * @return array{data: list<LogRow>, _cursor: ?string, _has_more_results: bool}
     */
    private function queryPage(string $url, array $filters, int $from, int $to, string $order, ?string $cursor): array
    {
        $params = [['from', (string) $from], ['to', (string) $to], ['order_by', $order], ...$filters];
        if ($cursor !== null) {
            $params[] = ['cursor', $cursor];
        }
        $query = implode('&', array_map(fn(array $p): string => rawurlencode($p[0]) . '=' . rawurlencode($p[1]), $params));
        $page = $this->observability->get($url . (str_contains($url, '?') ? '&' : '?') . $query);

        $data = is_array($page['data'] ?? null) ? $page['data'] : [];
        $newCursor = $page['_cursor'] ?? null;

        return [
            'data' => array_values(array_map(fn($row): array => $this->normalizeRow($row), array_filter($data, 'is_array'))),
            '_cursor' => is_string($newCursor) && $newCursor !== '' ? $newCursor : null,
            // Guard against an unchanged cursor, which would repeat the same query.
            '_has_more_results' => !empty($page['_has_more_results']) && $newCursor !== null && $newCursor !== $cursor,
        ];
    }

    /**
     * @param array<mixed> $row
     *
     * @return LogRow
     */
    private function normalizeRow(array $row): array
    {
        $str = fn(string $key): string => is_scalar($row[$key] ?? null) ? (string) $row[$key] : '';
        $time = strtotime($str('datetime'));

        return [
            'cursor' => $str('cursor'),
            'datetime' => $str('datetime'),
            'timestamp' => $time !== false ? $time : time(),
            'severity' => $str('severity'),
            'service' => $str('service'),
            'log_kind' => $str('log_kind'),
            'content' => $str('content'),
            'raw' => $row,
        ];
    }

    /**
     * Parses the --fields option.
     *
     * @return list<string>|null
     */
    private function parseFields(InputInterface $input, string $format): ?array
    {
        if ($input->getOption('fields') === null) {
            return null;
        }
        if ($format !== 'text') {
            throw new InvalidArgumentException('The --fields option can only be used with the "text" format');
        }
        $fields = array_values(ArrayArgument::split([Option::string($input, 'fields')]));
        foreach ($fields as $field) {
            if (!in_array($field, self::FIELDS, true) && !preg_match('/^context\.[^.]+/', $field)) {
                throw new InvalidArgumentException(sprintf('Invalid field: %s (expected one of: %s, or "context.<key>")', $field, implode(', ', self::FIELDS)));
            }
        }
        if ($fields === []) {
            throw new InvalidArgumentException('The --fields option must not be empty');
        }

        return $fields;
    }

    /**
     * @param LogRow $row
     * @param list<string> $fields
     */
    private function printRow(OutputInterface $output, array $row, string $format, array $fields): void
    {
        if ($format === 'json') {
            $output->writeln((string) json_encode($row['raw'], JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE), OutputInterface::OUTPUT_RAW);

            return;
        }
        if ($format === 'raw') {
            $output->writeln($row['content'], OutputInterface::OUTPUT_RAW);

            return;
        }

        $context = $this->contextFields($row['raw']['context'] ?? null);
        $keyValue = fn(string $key, string $value): string => '<fg=gray>' . OutputFormatter::escape($key) . '=</>' . OutputFormatter::escape($value);
        $parts = [];
        foreach ($fields as $field) {
            if ($field === 'context') {
                $parts = array_merge($parts, $this->formatContext($context, $keyValue));
                continue;
            }
            if (str_starts_with($field, 'context.')) {
                $key = substr($field, 8);
                if (isset($context[$key])) {
                    $parts[] = $keyValue($key, $context[$key]);
                }
                continue;
            }
            $raw = $row['raw'][$field] ?? null;
            $value = is_scalar($raw) ? (string) $raw : '';
            if ($value === '') {
                continue;
            }
            $parts[] = match ($field) {
                'datetime' => '<comment>' . OutputFormatter::escape($value) . '</comment>',
                'service' => '<info>' . OutputFormatter::escape($value) . '</info>',
                'severity' => $this->formatSeverity($value),
                default => OutputFormatter::escape($value),
            };
        }
        $output->writeln(implode(' ', $parts));
    }

    private function formatSeverity(string $severity): string
    {
        $tag = match (array_search($severity, self::SEVERITIES, true)) {
            0, 1, 2, 3 => 'fg=red',
            4, 5 => 'fg=yellow',
            default => null,
        };

        return $tag !== null ? "<$tag>" . OutputFormatter::escape($severity) . '</>' : OutputFormatter::escape($severity);
    }

    /**
     * Formats context fields for display, skipping some and limiting their number and length.
     *
     * @param array<string, string> $context
     * @param callable(string, string): string $keyValue
     *
     * @return list<string>
     */
    private function formatContext(array $context, callable $keyValue): array
    {
        $parts = [];
        $omitted = 0;
        $length = 0;
        foreach ($context as $key => $value) {
            // Skip fields that are already displayed, or that are only useful in tracing tools.
            if (in_array(strtolower(explode('.', $key, 2)[0]), self::SKIP_CONTEXT_KEYS, true)) {
                continue;
            }
            if (mb_strlen($value) > self::MAX_CONTEXT_VALUE_LENGTH) {
                $value = mb_substr($value, 0, self::MAX_CONTEXT_VALUE_LENGTH - 1) . '…';
            }
            $length += mb_strlen($key) + mb_strlen($value) + 2;
            if ($omitted > 0 || count($parts) >= self::MAX_CONTEXT_FIELDS || ($parts !== [] && $length > self::MAX_CONTEXT_LENGTH)) {
                $omitted++;
                continue;
            }
            $parts[] = $keyValue($key, $value);
        }
        if ($omitted > 0) {
            $parts[] = sprintf('<fg=gray>(+%d more)</>', $omitted);
        }

        return $parts;
    }

    /**
     * Extracts fields from a structured (JSON) log line, as key-value strings.
     *
     * Nested objects are flattened, with dot-separated keys.
     *
     * @return array<string, string>
     */
    private function contextFields(mixed $context): array
    {
        if (!is_string($context) || $context === '' || $context[0] !== '{') {
            return [];
        }
        $decoded = json_decode($context, true);
        if (!is_array($decoded)) {
            return [];
        }
        $fields = [];
        $flatten = function (array $data, string $prefix) use (&$flatten, &$fields): void {
            foreach ($data as $key => $value) {
                $key = $prefix . $key;
                if (is_array($value) && $value !== [] && !array_is_list($value)) {
                    $flatten($value, $key . '.');
                } elseif ($value !== null && $value !== '' && $value !== []) {
                    // Quote strings that would be ambiguous in key=value output; encode other values as JSON.
                    $fields[$key] = is_string($value) && !preg_match('/[\s"=]/', $value)
                        ? $value
                        : (string) json_encode($value, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
                }
            }
        };
        $flatten($decoded, '');

        return $fields;
    }

    /**
     * Returns the size of the first time window to query, in seconds.
     *
     * @param array<mixed> $entrypoint
     */
    private function initialWindow(array $entrypoint): int
    {
        $minutes = Observability::nested($entrypoint, 'data_retention', 'logs', 'recommended_default_range');

        return is_int($minutes) && $minutes > 0 ? min($minutes * 60, self::MAX_WINDOW) : 900;
    }

    /**
     * Returns the default time range to search, in seconds.
     *
     * @param array<mixed> $entrypoint
     */
    private function defaultRange(array $entrypoint): int
    {
        $minutes = array_filter([
            Observability::nested($entrypoint, 'data_retention', 'logs', 'retention_period'),
            Observability::nested($entrypoint, 'data_retention', 'logs', 'max_range'),
            Observability::nested($entrypoint, 'retention', 'logs'),
        ], fn($v): bool => is_int($v) && $v > 0);

        return $minutes !== [] ? min($minutes) * 60 : 86400;
    }

    /**
     * Parses a time option, as a relative duration (e.g. "2h") or a date/time.
     */
    private function parseTime(InputInterface $input, string $option, int $now): ?int
    {
        $value = Option::stringOrNull($input, $option);
        if ($value === null) {
            return null;
        }
        if (preg_match('/^(\d+)([smhd])$/', $value, $matches)) {
            return $now - (int) $matches[1] * ['s' => 1, 'm' => 60, 'h' => 3600, 'd' => 86400][$matches[2]];
        }
        $time = strtotime($value, $now);
        if ($time === false) {
            throw new InvalidArgumentException(sprintf('Invalid --%s time: %s', $option, $value));
        }

        return $time;
    }

    private function readViaSsh(InputInterface $input, Selection $selection, ?string $logType, int $lines): int
    {
        $host = $this->selector->getHostFromSelection($input, $selection);

        $logDir = '/var/log';

        // Special handling for Dedicated Generation 2 environments, for which
        // the SSH URL contains something like "ssh://1.ent-" or "1.ent-" or "ent-".
        if (preg_match('%(^|[/.])ent-[a-z0-9]%', $host->getLabel())) {
            $logDir = '/var/log/platform/"$USER"';
            $this->io->debug('Detected Dedicated environment: using log directory: ' . $logDir);
        }

        // Select the log file that the user specified.
        if ($logType !== null) {
            $logFilename = $logDir . '/' . OsUtil::escapePosixShellArg($logType . '.log');
        } elseif (!$input->isInteractive()) {
            $this->stdErr->writeln('No log type specified.');
            return 1;
        } else {

            // Read the list of files from the environment.
            $cacheKey = sprintf('log-files:%s', $host->getCacheKey());
            $cache = $this->cacheProvider;
            if (!$result = $cache->fetch($cacheKey)) {
                $result = $host->runCommand('echo -n _BEGIN_FILE_LIST_; ls -1 ' . $logDir . '/*.log; echo -n _END_FILE_LIST_');
                if (is_string($result)) {
                    $result = trim((string) StringUtil::between($result, '_BEGIN_FILE_LIST_', '_END_FILE_LIST_'));
                }

                // Cache the list for 1 day.
                $cache->save($cacheKey, $result, 86400);
            }

            // Provide a fallback list of files, in case the SSH command failed.
            $defaultFiles = [
                $logDir . '/access.log',
                $logDir . '/error.log',
            ];
            $files = $result && is_string($result) ? explode("\n", $result) : $defaultFiles;

            // Ask the user to choose a file.
            $files = array_combine($files, array_map(fn($file): string => str_replace('.log', '', basename(trim((string) $file))), $files));
            $logFilename = $this->questionHelper->choose($files, 'Enter a number to choose a log: ');
        }

        $command = sprintf('tail -n %1$d %2$s', $lines, $logFilename);
        if (Option::bool($input, 'tail')) {
            $command .= ' -f';
        }

        $this->stdErr->writeln(sprintf('Reading log file <info>%s:%s</info>', $host->getLabel(), $logFilename));

        return $host->runCommandDirect($command);
    }
}
