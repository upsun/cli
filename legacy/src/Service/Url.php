<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Util\OsUtil;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;

class Url implements InputConfiguringInterface
{
    protected OutputInterface $stdErr;

    /** WSL's kernel release file, binfmt_misc directory, config file, and default automount root (overridden in tests). */
    public string $osReleasePath = '/proc/sys/kernel/osrelease';
    public string $wslBinfmtDir = '/proc/sys/fs/binfmt_misc';
    public string $wslConfPath = '/etc/wsl.conf';
    public string $wslDefaultMountRoot = '/mnt/';

    private string|false|null $wslBrowser = null;

    public function __construct(protected Shell $shell, protected InputInterface $input, protected OutputInterface $output)
    {
        $this->stdErr = $this->output instanceof ConsoleOutputInterface
            ? $this->output->getErrorOutput()
            : $this->output;
    }

    /**
     * @param InputDefinition $definition
     */
    public static function configureInput(InputDefinition $definition): void
    {
        $definition->addOption(new InputOption(
            'browser',
            null,
            InputOption::VALUE_REQUIRED,
            'The browser to use to open the URL. Set 0 for none.',
        ));
        $definition->addOption(new InputOption(
            'pipe',
            null,
            InputOption::VALUE_NONE,
            'Output the URL to stdout.',
        ));
    }

    /**
     * Test whether URL(s) can be opened in the default browser.
     *
     * @return bool
     */
    public function canOpenUrls(): bool
    {
        return $this->hasDisplay()
            && $this->getBrowser($this->input->hasOption('browser') ? Option::stringOrNull($this->input, 'browser') : null) !== false;
    }

    /**
     * Opens a URL in the browser, or prints it.
     *
     * @return bool True if a browser was used, false otherwise.
     */
    public function openUrl(string $url, bool $print = true): bool
    {
        $browserOption = $this->input->hasOption('browser') ? Option::stringOrNull($this->input, 'browser') : null;
        $open = true;
        $success = false;

        // If the user wants to pipe the output to another command, stop here.
        if ($this->input->hasOption('pipe') && Option::bool($this->input, 'pipe')) {
            $open = false;
            $print = true;
        }
        // Check if the user has requested not to use a browser.
        elseif ($browserOption === '0') {
            $open = false;
        }
        // Check for a display (if not on Windows or OS X).
        elseif (!$this->hasDisplay()) {
            $open = false;
            $this->stdErr->writeln('Not opening URL (no display found)', OutputInterface::VERBOSITY_VERBOSE);
        }

        // Open the URL.
        if ($open && ($browser = $this->getBrowser($browserOption))) {
            if (OsUtil::isWindows() && $browser === 'start') {
                // The start command needs an extra (title) argument.
                $args = [$browser, '', $url];
            } elseif (str_ends_with($browser, 'rundll32.exe')) {
                $args = [$browser, 'url.dll,FileProtocolHandler', $url];
            } else {
                $args  = [$browser, $url];
            }

            $success = $this->shell->execute($args) !== false;
        }

        // Print the URL.
        if ($print) {
            $this->output->writeln($url);
        }

        return $success;
    }

    /**
     * Check for a display (if not on Windows or OS X). WSL counts, if a Windows browser can be opened.
     *
     * @return bool
     */
    public function hasDisplay(): bool
    {
        if (getenv('DISPLAY')) {
            return getenv('DISPLAY') !== 'none';
        }

        return OsUtil::isWindows() || OsUtil::isOsX() || $this->getWslBrowser() !== false;
    }

    /**
     * Finds the browser to use.
     *
     * @param string|null $browserOption
     *
     * @return string|false A browser command, or false if no browser can or
     *                      should be used.
     */
    private function getBrowser(?string $browserOption = null): string|false
    {
        if ($browserOption === '0') {
            return false;
        } elseif (!empty($browserOption)) {
            [$command, ] = explode(' ', $browserOption, 2);
            if (!$this->shell->commandExists($command)) {
                $this->stdErr->writeln(sprintf('Command not found: <error>%s</error>', $command));
                return false;
            }

            return $browserOption;
        }

        return $this->getDefaultBrowser();
    }

    /**
     * Find a default browser to use.
     *
     * @return string|false
     */
    private function getDefaultBrowser(): string|false
    {
        if (OsUtil::isWindows()) {
            return 'start';
        }
        if (OsUtil::isOsX()) {
            return 'open';
        }
        if (($browser = $this->getWslBrowser()) !== false) {
            return $browser;
        }
        $browsers = ['xdg-open', 'gnome-open'];
        foreach ($browsers as $browser) {
            if ($this->shell->commandExists($browser)) {
                return $browser;
            }
        }

        return false;
    }

    /**
     * Returns the command to open URLs in a Windows browser from WSL, or false. This matches the Go wrapper.
     */
    private function getWslBrowser(): string|false
    {
        return $this->wslBrowser ??= $this->findWslBrowser();
    }

    private function findWslBrowser(): string|false
    {
        if (!OsUtil::isLinux() || !$this->isWsl()) {
            return false;
        }
        $wslConf = (string) @file_get_contents($this->wslConfPath);
        // The opener runs a Windows program.
        if (!$this->wslInteropEnabled($wslConf)) {
            return false;
        }
        $root = self::wslConfValue($wslConf, 'automount', 'root');
        if ($root === '') {
            $root = $this->wslDefaultMountRoot;
        }
        $path = rtrim($root, '/') . '/c/Windows/System32/rundll32.exe';
        if (is_file($path) && is_executable($path)) {
            return $path;
        }
        // Searching PATH is slow under WSL2 (it has Windows directories), so it is the fallback.
        if ($this->shell->commandExists('rundll32.exe')) {
            return 'rundll32.exe';
        }

        return false;
    }

    /**
     * Tests for WSL. This includes Docker Desktop's containers, which share its kernel, but have no Windows browser.
     */
    private function isWsl(): bool
    {
        if (getenv('WSL_DISTRO_NAME') || getenv('WSL_INTEROP')) {
            return true;
        }
        $release = @file_get_contents($this->osReleasePath);

        return $release !== false && stripos($release, 'microsoft') !== false;
    }

    /**
     * Tests whether WSL can run Windows programs. Its binfmt_misc entry is named WSLInterop or WSLInterop-late.
     * WSL1 removes it when interop is off, but WSL2 keeps it enabled, so wsl.conf is checked too.
     */
    private function wslInteropEnabled(string $wslConf): bool
    {
        foreach (glob($this->wslBinfmtDir . '/WSLInterop*') ?: [] as $path) {
            $contents = @file_get_contents($path);
            if ($contents !== false && str_starts_with($contents, 'enabled')) {
                return strcasecmp(self::wslConfValue($wslConf, 'interop', 'enabled'), 'false') !== 0;
            }
        }

        return false;
    }

    /**
     * Returns the value of a key in a section of wsl.conf's contents, without surrounding quotes, or an empty string.
     */
    public static function wslConfValue(string $wslConf, string $section, string $key): string
    {
        $current = '';
        foreach (preg_split('/\r?\n/', $wslConf) ?: [] as $line) {
            $line = trim(explode('#', $line, 2)[0]);
            if (str_starts_with($line, '[') && str_ends_with($line, ']')) {
                $current = trim(substr($line, 1, -1));
                continue;
            }
            $parts = explode('=', $line, 2);
            if (count($parts) === 2 && strcasecmp($current, $section) === 0 && strcasecmp(trim($parts[0]), $key) === 0) {
                $value = trim($parts[1]);
                if (strlen($value) >= 2 && ($value[0] === '"' || $value[0] === "'") && $value[-1] === $value[0]) {
                    $value = substr($value, 1, -1);
                }

                return $value;
            }
        }

        return '';
    }
}
