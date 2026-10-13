<?php

declare(strict_types=1);

/**
 * @file
 * A script containing any set-up steps required for PHPUnit testing.
 */

require __DIR__ . '/../vendor/autoload.php';

error_reporting(E_ALL);
ini_set('display_errors', 'stderr');

putenv('PLATFORMSH_CLI_TOKEN=');

// Credentials are managed by the Go wrapper, which is replaced by a stub.
putenv('MOCK_CLI_WRAPPER_EXECUTABLE=' . __DIR__ . '/data/go-auth-stub');

// Tests run commands in the temporary directory, and the CLI searches parent
// directories for a Git repository, so a stray .git would leak into tests.
for ($dir = sys_get_temp_dir(); ; $dir = dirname($dir)) {
    if (file_exists($dir . '/.git')) {
        fwrite(STDERR, "Cannot isolate tests: found $dir/.git\nRemove it, or set TMPDIR to a directory outside any Git repository.\n");
        exit(1);
    }
    if (dirname($dir) === $dir) {
        break;
    }
}
