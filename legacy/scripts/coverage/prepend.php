<?php

// Records line coverage with PCOV in a file in PHP_COVERAGE_DIR, for report.php.
if (!extension_loaded('pcov')) {
    fwrite(STDERR, "The pcov extension is not loaded.\n");
    exit(1);
}
\pcov\start();
register_shutdown_function(static function (): void {
    \pcov\stop();
    $data = \pcov\collect(\pcov\inclusive, \pcov\waiting());
    file_put_contents((string) tempnam((string) getenv('PHP_COVERAGE_DIR'), 'pcov-'), serialize($data));
});
