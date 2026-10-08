<?php

declare(strict_types=1);

namespace Platformsh\Cli\Exception;

use Platformsh\Cli\Event\LoginRequiredEvent;

/**
 * Reports that the Go wrapper requires a login, e.g. because there is no session or it has expired.
 */
class GoLoginRequiredException extends \RuntimeException
{
    /**
     * @param string[] $authMethods
     */
    public function __construct(
        public readonly string $notice = '',
        public readonly array $authMethods = [],
        public readonly ?int $maxAge = null,
        public readonly bool $hasApiToken = false,
    ) {
        parent::__construct('Authentication is required.');
    }

    /**
     * @param array<mixed> $data
     */
    public static function fromData(array $data): self
    {
        $notice = isset($data['notice']) && is_string($data['notice']) ? $data['notice'] : '';
        $amr = isset($data['amr']) && is_array($data['amr']) ? array_values(array_filter($data['amr'], 'is_string')) : [];
        $maxAge = isset($data['max_age']) && is_int($data['max_age']) ? $data['max_age'] : null;

        return new self($notice, $amr, $maxAge, !empty($data['has_api_token']));
    }

    public function toEvent(): LoginRequiredEvent
    {
        return new LoginRequiredEvent($this->authMethods, $this->maxAge, $this->hasApiToken);
    }
}
