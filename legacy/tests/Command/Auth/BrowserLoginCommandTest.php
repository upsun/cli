<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\Auth;

use GuzzleHttp\ClientInterface;
use GuzzleHttp\Psr7\Response;
use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\Auth\BrowserLoginCommand;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\Login;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Cli\Service\Url;
use Psr\Http\Message\RequestInterface;

#[Group('commands')]
class BrowserLoginCommandTest extends TestCase
{
    /**
     * Tests that the access token request goes through the shared HTTP client.
     *
     * The shared client applies the detected CA bundle and the configured
     * proxy. A client built here instead would use the default certificate
     * verification, which fails behind a TLS-inspecting proxy.
     */
    public function testAccessTokenRequestUsesSharedHttpClient(): void
    {
        $tokenUrl = 'https://auth.example.com/oauth2/token';

        $sentRequest = null;
        $httpClient = $this->createMock(ClientInterface::class);
        $httpClient->expects($this->once())
            ->method('send')
            ->willReturnCallback(function (RequestInterface $request) use (&$sentRequest): Response {
                $sentRequest = $request;
                return new Response(200, [], '{"access_token": "test-token", "token_type": "bearer"}');
            });

        $api = $this->createMock(Api::class);
        $api->expects($this->once())
            ->method('getExternalHttpClient')
            ->willReturn($httpClient);

        $values = [
            'api.oauth2_token_url' => $tokenUrl,
            'api.oauth2_client_id' => 'test-client-id',
        ];
        $config = $this->createMock(Config::class);
        $config->method('getStr')->willReturnCallback(fn(string $key): string => $values[$key] ?? '');
        $config->method('get')->willReturnCallback(fn(string $key): string => $values[$key] ?? '');

        $command = new BrowserLoginCommand(
            $api,
            $config,
            $this->createMock(Io::class),
            $this->createMock(Login::class),
            $this->createMock(QuestionHelper::class),
            $this->createMock(Url::class),
        );

        $method = new \ReflectionMethod($command, 'getAccessToken');
        $token = $method->invoke($command, 'test-code', 'test-verifier', 'http://127.0.0.1:5000');

        $this->assertSame('test-token', $token['access_token']);
        $this->assertInstanceOf(RequestInterface::class, $sentRequest);
        $this->assertSame($tokenUrl, (string) $sentRequest->getUri());
    }
}
