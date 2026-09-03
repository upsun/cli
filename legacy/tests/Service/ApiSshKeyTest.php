<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use GuzzleHttp\Client;
use GuzzleHttp\ClientInterface;
use GuzzleHttp\Exception\BadResponseException;
use GuzzleHttp\Handler\MockHandler;
use GuzzleHttp\HandlerStack;
use GuzzleHttp\Psr7\Response;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Symfony\Component\Console\Output\BufferedOutput;

class ApiSshKeyTest extends TestCase
{
    public function testListsAllPagesAndCachesTheResult(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse([
                'items' => [$this->keyData('key-1')],
                '_links' => ['next' => ['href' => '?page=2']],
            ]),
            $this->jsonResponse(['items' => [$this->keyData('key-2')]]),
        ]);
        $api = $this->createApi($handler);

        $this->assertSame(['key-1', 'key-2'], array_map(fn($key) => $key->id, $api->getSshKeys()));
        $this->assertSame(['key-1', 'key-2'], array_map(fn($key) => $key->id, $api->getSshKeys()));
        $this->assertCount(0, $handler);
    }

    public function testRejectsCircularPagination(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse([
                'items' => [],
                '_links' => ['next' => ['href' => '/api/users/user-id/ssh-keys']],
            ]),
        ]);

        $this->expectException(\RuntimeException::class);
        $this->expectExceptionMessage('circular pagination link');
        $this->createApi($handler)->getSshKeys();
    }

    public function testGetReturnsNullForNotFound(): void
    {
        $api = $this->createApi(new MockHandler([new Response(404)]));

        $this->assertNull($api->getSshKey('missing'));
    }

    public function testApiErrorsIncludeResponseDetails(): void
    {
        $api = $this->createApi(new MockHandler([
            $this->jsonResponse(['detail' => 'The service is unavailable.'], 503),
        ]));

        try {
            $api->getSshKeys();
            $this->fail('Expected an HTTP error.');
        } catch (BadResponseException $e) {
            $this->assertStringContainsString('[detail] The service is unavailable.', $e->getMessage());
        }
    }

    public function testMutationsClearTheCachedList(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse(['items' => [$this->keyData('old')]]),
            $this->jsonResponse($this->keyData('new'), 201),
            $this->jsonResponse(['items' => [$this->keyData('old'), $this->keyData('new')]]),
            new Response(204),
            $this->jsonResponse(['items' => [$this->keyData('old')]]),
        ]);
        $api = $this->createApi($handler);

        $this->assertCount(1, $api->getSshKeys());
        $api->addSshKey('ssh-ed25519 AAAA', 'new');
        $this->assertCount(2, $api->getSshKeys());
        $api->deleteSshKey('new');
        $this->assertCount(1, $api->getSshKeys());
        $this->assertCount(0, $handler);
    }

    private function createApi(MockHandler $handler): Api
    {
        $client = new Client(['handler' => HandlerStack::create($handler)]);
        $config = new Config([
            'PLATFORMSH_CLI_API_URL' => 'https://api.example.test/api',
            'PLATFORMSH_CLI_SESSION_ID' => 'ssh-key-test',
        ]);
        $this->assertSame('https://api.example.test/api', $config->getApiUrl());
        $this->assertSame('ssh-key-test', $config->getSessionId());

        return new class ($client, $config) extends Api {
            public function __construct(private ClientInterface $httpClient, Config $config)
            {
                parent::__construct($config, new ArrayCache(), new BufferedOutput());
            }

            public function getHttpClient(): ClientInterface
            {
                return $this->httpClient;
            }

            public function getMyUserId(bool $reset = false): string
            {
                return 'user-id';
            }
        };
    }

    /** @param array<string, mixed> $data */
    private function jsonResponse(array $data, int $status = 200): Response
    {
        return new Response($status, ['Content-Type' => 'application/json'], json_encode($data, JSON_THROW_ON_ERROR));
    }

    /** @return array<string, mixed> */
    private function keyData(string $id): array
    {
        return [
            'id' => $id,
            'sha256' => 'SHA256:' . $id,
            'value' => 'ssh-ed25519 AAAA',
            'label' => $id,
            'active' => true,
            'user_id' => 'user-id',
            'created_at' => '2026-01-01T00:00:00Z',
            'updated_at' => '2026-01-01T00:00:00Z',
        ];
    }
}
