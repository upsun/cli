<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use GuzzleHttp\Client;
use GuzzleHttp\ClientInterface;
use GuzzleHttp\Exception\BadResponseException;
use GuzzleHttp\Handler\MockHandler;
use GuzzleHttp\HandlerStack;
use GuzzleHttp\Middleware;
use GuzzleHttp\Psr7\Response;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Psr\Http\Message\RequestInterface;
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

        $ids = fn(): array => array_map(fn($key) => $key->id, $api->getSshKeys());
        $first = $ids();
        $second = $ids();
        $this->assertSame(['key-1', 'key-2'], $first);
        $this->assertSame($first, $second);
        $this->assertCount(0, $handler);
    }

    public function testStopsAtANextLinkToTheCurrentPage(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse([
                'items' => [$this->keyData('key-1')],
                '_links' => ['next' => ['href' => '/api/users/user-id/ssh-keys']],
            ]),
        ]);

        $keys = $this->createApi($handler)->getSshKeys();
        $this->assertSame(['key-1'], array_map(fn($key) => $key->id, $keys));
    }

    public function testRejectsCircularPagination(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse([
                'items' => [],
                '_links' => ['next' => ['href' => '?page=2']],
            ]),
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

    public function testFetchesAndCachesTheSource(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse(['source' => 'auth']),
            $this->jsonResponse(['source' => 'accounts']),
        ]);
        /** @var \ArrayObject<int, RequestInterface> $history */
        $history = new \ArrayObject();
        $api = $this->createApi($handler, null, $history);

        $sources = [$api->getSshKeySource(), $api->getSshKeySource(), $api->getSshKeySource(true)];
        $this->assertSame(['auth', 'auth', 'accounts'], $sources);
        $this->assertCount(0, $handler);
        $this->assertSame([
            'GET https://api.example.test/api/ssh-key-source',
            'GET https://api.example.test/api/ssh-key-source',
        ], $this->requestLines($history));
    }

    public function testTreatsAMissingSourceEndpointAsAccounts(): void
    {
        $api = $this->createApi(new MockHandler([new Response(404)]), null);

        $this->assertSame('accounts', $api->getSshKeySource());
    }

    public function testRejectsAnUnknownSource(): void
    {
        $api = $this->createApi(new MockHandler([$this->jsonResponse(['source' => 'other'])]), null);

        $this->expectException(\RuntimeException::class);
        $this->expectExceptionMessage('unknown SSH key source');
        $api->getSshKeySource();
    }

    public function testSourceErrorsArePropagated(): void
    {
        $api = $this->createApi(new MockHandler([new Response(503)]), null);

        $this->expectException(BadResponseException::class);
        $api->getSshKeySource();
    }

    public function testResettingTheListRefetchesTheSource(): void
    {
        $handler = new MockHandler([
            $this->jsonResponse(['source' => 'auth']),
            $this->jsonResponse(['items' => [$this->keyData('key-1')]]),
            $this->jsonResponse(['source' => 'accounts']),
            $this->jsonResponse(['ssh_keys' => [$this->legacyKeyData(42)]]),
        ]);
        $api = $this->createApi($handler, null);

        $this->assertSame(['key-1'], array_map(fn($key) => $key->id, $api->getSshKeys()));
        $this->assertSame(['42'], array_map(fn($key) => $key->id, $api->getSshKeys(true)));
        $this->assertCount(0, $handler);
    }

    public function testListsLegacyKeys(): void
    {
        /** @var \ArrayObject<int, RequestInterface> $history */
        $history = new \ArrayObject();
        $api = $this->createApi(new MockHandler([
            $this->jsonResponse(['ssh_keys' => [$this->legacyKeyData(42)]]),
        ]), 'accounts', $history);

        $keys = $api->getSshKeys();
        $this->assertCount(1, $keys);
        $this->assertSame('42', $keys[0]->id);
        $this->assertSame('legacy', $keys[0]->label);
        $this->assertSame('SHA256:cJ6AyISHokEeHuTfufIqhhSS0gxHZRUMDHlKvXD4FHw', $keys[0]->sha256);
        $this->assertTrue($keys[0]->active);
        $this->assertSame(['GET https://api.example.test/api/me'], $this->requestLines($history));
    }

    public function testLegacyMutationsUseTheLegacyApi(): void
    {
        /** @var \ArrayObject<int, RequestInterface> $history */
        $history = new \ArrayObject();
        $api = $this->createApi(new MockHandler([
            $this->jsonResponse($this->legacyKeyData(43), 201),
            new Response(204),
            new Response(404),
        ]), 'accounts', $history);

        $this->assertSame('43', $api->addSshKey('ssh-ed25519 AAAA', 'legacy')->id);
        $api->deleteSshKey('43');
        $this->assertNull($api->getSshKey('43'));

        $this->assertSame([
            'POST https://api.example.test/api/ssh_keys',
            'DELETE https://api.example.test/api/ssh_keys/43',
            'GET https://api.example.test/api/ssh_keys/43',
        ], $this->requestLines($history));
        $post = $history->getArrayCopy()[0];
        $this->assertSame(['value' => 'ssh-ed25519 AAAA', 'title' => 'legacy'], json_decode((string) $post->getBody(), true));
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

    /**
     * @param string|null $source The SSH key source to pre-cache, or null to fetch it.
     * @param \ArrayObject<int, RequestInterface>|null $history Filled with the requests made.
     */
    private function createApi(MockHandler $handler, ?string $source = 'auth', ?\ArrayObject $history = null): Api
    {
        $stack = HandlerStack::create($handler);
        if ($history !== null) {
            $stack->push(Middleware::mapRequest(function (RequestInterface $request) use ($history): RequestInterface {
                $history[] = $request;
                return $request;
            }));
        }
        $client = new Client(['handler' => $stack]);
        $config = new Config([
            'PLATFORMSH_CLI_API_URL' => 'https://api.example.test/api',
            'PLATFORMSH_CLI_SESSION_ID' => 'ssh-key-test',
        ]);
        $this->assertSame('https://api.example.test/api', $config->getApiUrl());
        $this->assertSame('ssh-key-test', $config->getSessionId());

        $cache = new ArrayCache();
        if ($source !== null) {
            $cache->save('ssh-key-test:ssh-key-source', $source);
        }

        return new class ($client, $config, $cache) extends Api {
            public function __construct(private ClientInterface $httpClient, Config $config, ArrayCache $cache)
            {
                parent::__construct($config, $cache, new BufferedOutput());
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

    /**
     * @param \ArrayObject<int, RequestInterface> $history
     *
     * @return string[]
     */
    private function requestLines(\ArrayObject $history): array
    {
        return array_map(fn(RequestInterface $r): string => $r->getMethod() . ' ' . $r->getUri(), $history->getArrayCopy());
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

    /** @return array<string, mixed> */
    private function legacyKeyData(int $id): array
    {
        return [
            'key_id' => $id,
            'title' => 'legacy',
            'value' => 'ssh-ed25519 AAAA',
            'fingerprint' => 'md5-fingerprint',
        ];
    }
}
