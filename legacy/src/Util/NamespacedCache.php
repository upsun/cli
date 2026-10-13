<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

use Doctrine\Common\Cache\CacheProvider;

/**
 * A namespace within another cache, sharing its storage.
 */
class NamespacedCache extends CacheProvider
{
    public function __construct(private readonly CacheProvider $inner, string $namespace)
    {
        $this->setNamespace($namespace);
    }

    protected function doFetch($id): mixed
    {
        return $this->inner->fetch($id);
    }

    protected function doContains($id): bool
    {
        return $this->inner->contains($id);
    }

    protected function doSave($id, $data, $lifeTime = 0): bool
    {
        return $this->inner->save($id, $data, $lifeTime);
    }

    protected function doDelete($id): bool
    {
        return $this->inner->delete($id);
    }

    protected function doFlush(): bool
    {
        return $this->inner->flushAll();
    }

    /**
     * @return array<mixed>|null
     */
    protected function doGetStats(): ?array
    {
        return $this->inner->getStats();
    }
}
