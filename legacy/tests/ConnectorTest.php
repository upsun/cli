<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Connector;
use Platformsh\Client\Session\Session;

class ConnectorTest extends TestCase
{
    use HttpServerTrait;

    /**
     * The curl options apply to API requests and to OAuth 2.0 token requests.
     */
    public function testCurlOptions(): void
    {
        $connector = new Connector([
            'api_url' => self::$httpServerUrl,
            'api_token' => 'test',
            'api_token_type' => 'access',
            'curl' => [CURLOPT_COOKIE => 'test=curl-option'],
        ], new Session());

        $response = $connector->getClient()->request('GET', self::$httpServerUrl . '/cookie', ['auth' => false]);
        $this->assertEquals('test=curl-option', (string) $response->getBody());

        $response = $connector->getOAuth2Provider()->getHttpClient()->request('GET', self::$httpServerUrl . '/cookie');
        $this->assertEquals('test=curl-option', (string) $response->getBody());
    }
}
