<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\Exception\GoLoginRequiredException;
use Symfony\Component\Console\Output\ConsoleOutput;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Process\Process;

/**
 * Gets tokens and auth state from the Go wrapper, which is the only component that stores or refreshes credentials.
 *
 * Requests are sent over a socket that the wrapper passes as an inherited file descriptor, whose number is in the
 * {prefix}WRAPPER_AUTH_FD environment variable, or on Windows over a named pipe, whose name is in
 * {prefix}WRAPPER_AUTH_PIPE. Otherwise, e.g. in a forked process, the wrapper's hidden auth:internal command is run,
 * using the executable path in the {prefix}WRAPPER_EXECUTABLE environment variable.
 */
class GoAuth
{
    private const LOGIN_REQUIRED_EXIT_CODE = 3;

    /**
     * Cached results, keyed by session ID.
     *
     * These are static so that they are shared by Api instances with different service containers.
     *
     * @var array<string, array{logged_in: bool, session_ids: string[], has_stored_api_token: bool}>
     */
    private static array $status = [];

    /** @var array<string, array{access_token: string, expires?: int}> */
    private static array $tokens = [];

    /** @var resource|false|null The wrapper's auth channel, false if it cannot be used, or null if not opened yet. */
    private static mixed $channel = null;

    /** The ID of the process that opened the channel. */
    private static ?int $channelPid = null;

    private readonly OutputInterface $stdErr;

    public function __construct(private readonly Config $config, ?OutputInterface $output = null)
    {
        $output = $output ?: new ConsoleOutput();
        $this->stdErr = $output instanceof ConsoleOutputInterface ? $output->getErrorOutput() : $output;
    }

    /**
     * Checks whether authentication is handled by the Go wrapper, which is set by the {prefix}GO_AUTH environment
     * variable. Otherwise the legacy implementation is used.
     */
    public function isEnabled(): bool
    {
        $value = getenv($this->config->getStr('application.env_prefix') . 'GO_AUTH');

        return $value !== false && $value !== '' && $value !== '0';
    }

    /**
     * Returns an access token and its expiry, refreshing it in Go if needed.
     *
     * @param string|null $rejected An access token that the API rejected, which Go should replace.
     *
     * @throws GoLoginRequiredException if login is required
     *
     * @return array{access_token: string, expires?: int}
     */
    public function getToken(?string $rejected = null): array
    {
        $sessionId = $this->config->getSessionId();
        $cached = self::$tokens[$sessionId] ?? null;
        if ($cached !== null && $rejected === null && !$this->expiresSoon($cached)) {
            return $cached;
        }
        // The rejected token is passed via stdin, to keep it out of process listings.
        /** @var array{access_token: string, expires?: int} $token */
        $token = $rejected !== null ? $this->run(['token', '--rejected'], $rejected) : $this->run(['token']);

        return self::$tokens[$sessionId] = $token;
    }

    /**
     * Returns whether an API token or access token is set via config, if the wrapper reports it.
     *
     * The wrapper removes these tokens from this process's environment when it serves auth requests over its channel.
     *
     * @return bool|null Null if the wrapper does not report it.
     */
    public function hasConfiguredToken(): ?bool
    {
        $value = getenv($this->config->getStr('application.env_prefix') . 'WRAPPER_HAS_TOKEN');

        return $value === false || $value === '' ? null : $value === '1';
    }

    /**
     * Returns the auth state, without making network requests.
     *
     * @return array{logged_in: bool, session_ids: string[], has_stored_api_token: bool}
     */
    public function getStatus(): array
    {
        $sessionId = $this->config->getSessionId();
        if (!isset(self::$status[$sessionId])) {
            /** @var array{logged_in: bool, session_ids: string[], has_stored_api_token: bool} $status */
            $status = $this->run(['status']);
            self::$status[$sessionId] = $status;
        }

        return self::$status[$sessionId];
    }

    /**
     * Clears cached tokens and state, e.g. after a login.
     */
    public function reset(): void
    {
        self::$status = [];
        self::$tokens = [];
    }

    /**
     * Runs the Go browser login, with the terminal's input and output.
     *
     * @param array<string, string[]|int> $options Login options, e.g. ['--method' => ['mfa'], '--max-age' => 60].
     *
     * @return int The exit code.
     */
    public function login(array $options = []): int
    {
        $command = [$this->executable(), 'auth:browser-login'];
        foreach ($options as $option => $value) {
            $command[] = $option . '=' . (is_array($value) ? implode(',', $value) : (string) $value);
        }
        $process = proc_open($command, [STDIN, STDOUT, STDERR], $pipes);
        if ($process === false) {
            throw new \RuntimeException('Failed to start the login command');
        }
        $exitCode = proc_close($process);
        $this->reset();

        return $exitCode;
    }

    /**
     * @param array{access_token: string, expires?: int} $token
     */
    private function expiresSoon(array $token): bool
    {
        return !empty($token['expires']) && $token['expires'] - 120 < time();
    }

    /**
     * @param string[] $args
     * @param string|null $input Input for the command's stdin.
     *
     * @return array<mixed>
     */
    private function run(array $args, ?string $input = null): array
    {
        $channel = $this->channel();
        if ($channel !== null) {
            $response = $this->request($channel, $args[0], $input);
            if ($response !== null) {
                return $this->result($response);
            }
            // The wrapper has exited, e.g. if this is a background process.
            self::$channel = false;
        }

        $process = new Process(array_merge([$this->executable(), 'auth:internal'], $args), null, $this->env(), $input);
        $process->setTimeout(null);
        $process->run();

        $stderr = rtrim($process->getErrorOutput());
        $lines = $stderr === '' ? [] : explode("\n", $stderr);
        if ($process->getExitCode() === self::LOGIN_REQUIRED_EXIT_CODE && $lines !== []) {
            $data = json_decode((string) array_pop($lines), true);
            $this->forwardStderr($lines);
            throw GoLoginRequiredException::fromData(is_array($data) ? $data : []);
        }
        if (!$process->isSuccessful()) {
            throw new \RuntimeException('Failed to get authentication details: ' . ($stderr ?: 'exit code ' . $process->getExitCode()));
        }
        $this->forwardStderr($lines);
        $data = json_decode($process->getOutput(), true);
        if (!is_array($data)) {
            throw new \RuntimeException('Failed to parse authentication details');
        }

        return $data;
    }

    /**
     * Returns the wrapper's auth channel, if this process can use it.
     *
     * The channel is only used by the process that the wrapper started: a process forked by it shares the socket, but
     * may outlive the wrapper. On Windows, the wrapper refuses a connection to the pipe from other processes.
     *
     * @return resource|null
     */
    private function channel(): mixed
    {
        if (self::$channelPid !== null && self::$channelPid !== (int) getmypid()) {
            return null;
        }
        if (self::$channel === null) {
            self::$channel = false;
            self::$channelPid = (int) getmypid();
            $prefix = $this->config->getStr('application.env_prefix');
            $fd = getenv($prefix . 'WRAPPER_AUTH_FD');
            $pipe = getenv($prefix . 'WRAPPER_AUTH_PIPE');
            $wrapperPid = getenv($prefix . 'WRAPPER_PID');
            if (is_string($fd) && ctype_digit($fd) && is_string($wrapperPid) && function_exists('posix_getppid')
                && posix_getppid() === (int) $wrapperPid) {
                self::$channel = @fopen('php://fd/' . $fd, 'r+');
            } elseif (is_string($pipe) && preg_match('/^[\w-]+$/', $pipe)) {
                self::$channel = @fopen('\\\\.\\pipe\\' . $pipe, 'r+');
            }
        }

        return self::$channel ?: null;
    }

    /**
     * Sends a request over the wrapper's auth channel, which answers with one line of JSON.
     *
     * @param resource $channel
     * @param string|null $rejected An access token that the API rejected.
     *
     * @return array<mixed>|null The response, or null if the channel is closed.
     */
    private function request(mixed $channel, string $command, ?string $rejected): ?array
    {
        $request = ['command' => $command, 'session_id' => $this->config->getSessionId()];
        if ($rejected !== null) {
            $request['rejected'] = $rejected;
        }
        if (@fwrite($channel, json_encode($request, JSON_THROW_ON_ERROR) . "\n") === false) {
            return null;
        }
        $line = @fgets($channel);
        if (!is_string($line)) {
            return null;
        }
        $response = json_decode($line, true);
        if (!is_array($response)) {
            throw new \RuntimeException('Failed to parse authentication details');
        }

        return $response;
    }

    /**
     * Returns the result of a response from the wrapper's auth channel.
     *
     * @param array<mixed> $response
     *
     * @throws GoLoginRequiredException if login is required
     *
     * @return array<mixed>
     */
    private function result(array $response): array
    {
        if (isset($response['login_required']) && is_array($response['login_required'])) {
            throw GoLoginRequiredException::fromData($response['login_required']);
        }
        if (isset($response['error']) && is_string($response['error'])) {
            throw new \RuntimeException('Failed to get authentication details: ' . $response['error']);
        }
        if (!isset($response['result']) || !is_array($response['result'])) {
            throw new \RuntimeException('Failed to parse authentication details');
        }

        return $response['result'];
    }

    /**
     * @param string[] $lines
     */
    private function forwardStderr(array $lines): void
    {
        foreach ($lines as $line) {
            $this->stdErr->writeln($line, OutputInterface::OUTPUT_RAW);
        }
    }

    private function executable(): string
    {
        $var = $this->config->getStr('application.env_prefix') . 'WRAPPER_EXECUTABLE';
        $executable = getenv($var);
        if ($executable === false || $executable === '') {
            throw new \RuntimeException(sprintf('The %s environment variable is not set. The CLI must be run via its main executable.', $var));
        }

        return $executable;
    }

    /**
     * Returns environment variables for the Go command, which must use the same session as this process.
     *
     * @return array<string, string>
     */
    private function env(): array
    {
        return [$this->config->getStr('application.env_prefix') . 'SESSION_ID' => $this->config->getSessionId()];
    }
}
