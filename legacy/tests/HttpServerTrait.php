<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests;

use PHPUnit\Framework\Attributes\AfterClass;
use PHPUnit\Framework\Attributes\BeforeClass;
use Platformsh\Cli\Util\PortUtil;
use Symfony\Component\Process\Process;

/**
 * Runs the PHP built-in server with tests/data/http-server.php, for HTTP client tests.
 */
trait HttpServerTrait
{
    private static ?Process $httpServer = null;
    private static string $httpServerUrl = '';

    #[BeforeClass]
    public static function startHttpServer(): void
    {
        $port = PortUtil::getPort(8000, '127.0.0.1');
        self::$httpServer = new Process([PHP_BINARY, '-d', 'output_buffering=0', '-S', '127.0.0.1:' . $port, __DIR__ . '/data/http-server.php']);
        self::$httpServer->start();
        self::$httpServerUrl = 'http://127.0.0.1:' . $port;
        $deadline = microtime(true) + 5;
        while (!PortUtil::isPortInUse($port, '127.0.0.1')) {
            if (microtime(true) > $deadline || !self::$httpServer->isRunning()) {
                throw new \RuntimeException('The test HTTP server did not start: ' . self::$httpServer->getErrorOutput());
            }
            usleep(50000);
        }
    }

    #[AfterClass]
    public static function stopHttpServer(): void
    {
        self::$httpServer?->stop(0);
        self::$httpServer = null;
    }
}
