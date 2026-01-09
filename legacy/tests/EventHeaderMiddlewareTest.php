<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests;

use GuzzleHttp\Psr7\Request;
use GuzzleHttp\Psr7\Response;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\EventHeaderMiddleware;
use Platformsh\Cli\Service\Config;

class EventHeaderMiddlewareTest extends TestCase
{
    private string $configFile;

    protected function setUp(): void
    {
        $this->configFile = __DIR__ . '/data/mock-cli-config.yaml';
    }

    public function testMiddlewareAddsEventHeaderInteractive(): void
    {
        putenv('MOCK_CLI_EVENT_NAME=backup:restore');
        putenv('MOCK_CLI_NO_INTERACTION');
        try {
            $config = new Config([], $this->configFile);
            $middleware = new EventHeaderMiddleware($config);

            $request = new Request('GET', 'https://api.example.com/test');

            $capturedRequest = null;
            $handler = function (Request $req, array $options) use (&$capturedRequest) {
                $capturedRequest = $req;
                return new Response(200);
            };

            $wrappedHandler = $middleware($handler);
            $wrappedHandler($request, []);

            $this->assertNotNull($capturedRequest);
            $this->assertEquals(
                'command=backup:restore; interactive=true',
                $capturedRequest->getHeaderLine('X-CLI-Event')
            );
        } finally {
            putenv('MOCK_CLI_EVENT_NAME');
            putenv('MOCK_CLI_NO_INTERACTION');
        }
    }

    public function testMiddlewareAddsEventHeaderNonInteractive(): void
    {
        putenv('MOCK_CLI_EVENT_NAME=project:info');
        putenv('MOCK_CLI_NO_INTERACTION=1');
        try {
            $config = new Config([], $this->configFile);
            $middleware = new EventHeaderMiddleware($config);

            $request = new Request('GET', 'https://api.example.com/test');

            $capturedRequest = null;
            $handler = function (Request $req, array $options) use (&$capturedRequest) {
                $capturedRequest = $req;
                return new Response(200);
            };

            $wrappedHandler = $middleware($handler);
            $wrappedHandler($request, []);

            $this->assertNotNull($capturedRequest);
            $this->assertEquals(
                'command=project:info; interactive=false',
                $capturedRequest->getHeaderLine('X-CLI-Event')
            );
        } finally {
            putenv('MOCK_CLI_EVENT_NAME');
            putenv('MOCK_CLI_NO_INTERACTION');
        }
    }

    public function testMiddlewareDoesNotAddHeaderWhenEventNameIsEmpty(): void
    {
        putenv('MOCK_CLI_EVENT_NAME');

        $config = new Config([], $this->configFile);
        $middleware = new EventHeaderMiddleware($config);

        $request = new Request('GET', 'https://api.example.com/test');

        $capturedRequest = null;
        $handler = function (Request $req, array $options) use (&$capturedRequest) {
            $capturedRequest = $req;
            return new Response(200);
        };

        $wrappedHandler = $middleware($handler);
        $wrappedHandler($request, []);

        $this->assertNotNull($capturedRequest);
        $this->assertFalse($capturedRequest->hasHeader('X-CLI-Event'));
    }

    public function testMiddlewarePreservesExistingHeaders(): void
    {
        putenv('MOCK_CLI_EVENT_NAME=project:info');
        putenv('MOCK_CLI_NO_INTERACTION');
        try {
            $config = new Config([], $this->configFile);
            $middleware = new EventHeaderMiddleware($config);

            $request = new Request('GET', 'https://api.example.com/test', [
                'Authorization' => 'Bearer token123',
                'Content-Type' => 'application/json',
            ]);

            $capturedRequest = null;
            $handler = function (Request $req, array $options) use (&$capturedRequest) {
                $capturedRequest = $req;
                return new Response(200);
            };

            $wrappedHandler = $middleware($handler);
            $wrappedHandler($request, []);

            $this->assertNotNull($capturedRequest);
            $this->assertEquals(
                'command=project:info; interactive=true',
                $capturedRequest->getHeaderLine('X-CLI-Event')
            );
            $this->assertEquals('Bearer token123', $capturedRequest->getHeaderLine('Authorization'));
            $this->assertEquals('application/json', $capturedRequest->getHeaderLine('Content-Type'));
        } finally {
            putenv('MOCK_CLI_EVENT_NAME');
            putenv('MOCK_CLI_NO_INTERACTION');
        }
    }

    public function testMiddlewareRespectsDoNotTrack(): void
    {
        putenv('MOCK_CLI_EVENT_NAME=backup:restore');
        putenv('DO_NOT_TRACK=1');
        try {
            $config = new Config([], $this->configFile);
            $middleware = new EventHeaderMiddleware($config);

            $request = new Request('GET', 'https://api.example.com/test');

            $capturedRequest = null;
            $handler = function (Request $req, array $options) use (&$capturedRequest) {
                $capturedRequest = $req;
                return new Response(200);
            };

            $wrappedHandler = $middleware($handler);
            $wrappedHandler($request, []);

            $this->assertNotNull($capturedRequest);
            $this->assertFalse($capturedRequest->hasHeader('X-CLI-Event'));
        } finally {
            putenv('MOCK_CLI_EVENT_NAME');
            putenv('DO_NOT_TRACK');
        }
    }

    public function testMiddlewareRespectsDisableTelemetry(): void
    {
        putenv('MOCK_CLI_EVENT_NAME=backup:restore');
        putenv('MOCK_CLI_DISABLE_TELEMETRY=1');
        try {
            $config = new Config([], $this->configFile);
            $middleware = new EventHeaderMiddleware($config);

            $request = new Request('GET', 'https://api.example.com/test');

            $capturedRequest = null;
            $handler = function (Request $req, array $options) use (&$capturedRequest) {
                $capturedRequest = $req;
                return new Response(200);
            };

            $wrappedHandler = $middleware($handler);
            $wrappedHandler($request, []);

            $this->assertNotNull($capturedRequest);
            $this->assertFalse($capturedRequest->hasHeader('X-CLI-Event'));
        } finally {
            putenv('MOCK_CLI_EVENT_NAME');
            putenv('MOCK_CLI_DISABLE_TELEMETRY');
        }
    }
}
