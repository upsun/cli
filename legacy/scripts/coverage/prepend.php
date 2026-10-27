<?php

// Records line coverage with PCOV in a file in PHP_COVERAGE_DIR, for report.php.
$coverageDir = (string) getenv('PHP_COVERAGE_DIR');
if (!extension_loaded('pcov') || !is_dir($coverageDir)) {
    fwrite(STDERR, "Coverage needs the pcov extension, and an existing directory in PHP_COVERAGE_DIR.\n");
    exit(1);
}
\pcov\start();
register_shutdown_function(static function () use ($coverageDir): void {
    \pcov\stop();
    $data = \pcov\collect(\pcov\inclusive, \pcov\waiting());
    $file = sprintf('%s/pcov-%d-%s', $coverageDir, getmypid(), bin2hex(random_bytes(4)));
    if (file_put_contents($file, serialize($data)) === false) {
        fwrite(STDERR, "Failed to write coverage to $file\n");
        exit(1);
    }
});
