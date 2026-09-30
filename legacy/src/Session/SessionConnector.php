<?php

declare(strict_types=1);

namespace Platformsh\Cli\Session;

use League\OAuth2\Client\Token\AccessTokenInterface;
use Platformsh\Client\Connection\Connector;

/**
 * A connector that runs a callback after saving a token.
 */
class SessionConnector extends Connector
{
    private ?\Closure $onTokenSaved = null;

    public function setOnTokenSaved(callable $callback): void
    {
        $this->onTokenSaved = \Closure::fromCallable($callback);
    }

    public function saveToken(AccessTokenInterface $token): void
    {
        try {
            parent::saveToken($token);
        } finally {
            if ($this->onTokenSaved !== null) {
                ($this->onTokenSaved)();
            }
        }
    }
}
