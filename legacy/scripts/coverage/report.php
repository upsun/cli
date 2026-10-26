<?php

// Prints a Markdown report of the legacy CLI's line coverage, from the files written by prepend.php.
// Usage: php report.php COVERAGE_DIR [HTML_DIR]

declare(strict_types=1);

use SebastianBergmann\CodeCoverage\CodeCoverage;
use SebastianBergmann\CodeCoverage\Data\RawCodeCoverageData;
use SebastianBergmann\CodeCoverage\Driver\Driver;
use SebastianBergmann\CodeCoverage\Filter;
use SebastianBergmann\CodeCoverage\Node\Directory;
use SebastianBergmann\CodeCoverage\Report\Html\Facade as HtmlReport;
use Symfony\Component\Finder\Finder;

require __DIR__ . '/../../vendor/autoload.php';

$coverageDir = $argv[1] ?? '';
if ($coverageDir === '') {
    fwrite(STDERR, "Usage: php report.php COVERAGE_DIR [HTML_DIR]\n");
    exit(1);
}
$htmlDir = $argv[2] ?? null;

$src = dirname(__DIR__, 2) . '/src';
$filter = new Filter();
foreach ((new Finder())->files()->in($src)->name('*.php') as $file) {
    $filter->includeFile($file->getRealPath());
}

// The data is already collected, so the driver does nothing.
$driver = new class extends Driver {
    public function nameAndVersion(): string
    {
        return 'PCOV (collected)';
    }

    public function start(): void {}

    public function stop(): RawCodeCoverageData
    {
        return RawCodeCoverageData::fromXdebugWithoutPathCoverage([]);
    }
};
$coverage = new CodeCoverage($driver, $filter);
$coverage->includeUncoveredFiles();

$files = glob($coverageDir . '/pcov-*') ?: [];
foreach ($files as $file) {
    /** @var array<string, array<int, int>> $data */
    $data = unserialize((string) file_get_contents($file));
    $coverage->append(RawCodeCoverageData::fromXdebugWithoutPathCoverage($data), basename($file));
}

$report = $coverage->getReport();

$percent = static fn(int $executed, int $executable): string => $executable === 0
    ? '-'
    : sprintf('%.1f%%', 100 * $executed / $executable);

echo sprintf("### PHP lines: %s\n\n", $percent($report->numberOfExecutedLines(), $report->numberOfExecutableLines()));
echo sprintf("Recorded from %d PHP processes.\n\n", count($files));
echo "<details><summary>By directory</summary>\n\n";
echo "| Directory | Lines | Coverage |\n";
echo "| --- | ---: | ---: |\n";

// Commands are listed by their subdirectory, other code by its top-level directory.
$rows = [];
$addRow = static function (Directory $dir, string $name) use (&$rows, $percent): void {
    $rows[$name] = sprintf("| `%s` | %d | %s |\n", $name, $dir->numberOfExecutableLines(), $percent($dir->numberOfExecutedLines(), $dir->numberOfExecutableLines()));
};
foreach ($report->directories() as $dir) {
    if ($dir->name() === 'Command') {
        foreach ($dir->directories() as $commandDir) {
            $addRow($commandDir, 'Command/' . $commandDir->name());
        }
    } else {
        $addRow($dir, $dir->name());
    }
}
ksort($rows);
echo implode('', $rows);
echo "\n</details>\n";

if ($htmlDir !== null) {
    (new HtmlReport())->process($coverage, $htmlDir);
}
