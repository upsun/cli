<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model;

/**
 * An SSH public key belonging to a user.
 *
 * This models the Auth API's representation, which replaced the older
 * Accounts one. Notable differences: the ID is an opaque string (a ULID)
 * rather than an integer, the label was called "title", and the fingerprint
 * is a SHA-256 hash in OpenSSH format rather than an MD5 hash.
 */
readonly class SshKey
{
    public function __construct(
        public string $id,
        public string $sha256,
        public string $value,
        public string $label,
        public bool $active,
        public string $userId,
        public string $createdAt,
        public string $updatedAt,
    ) {}

    /**
     * @param array<string, mixed> $data
     */
    public static function fromData(array $data): self
    {
        return new self(
            (string) ($data['id'] ?? ''),
            (string) ($data['sha256'] ?? ''),
            (string) ($data['value'] ?? ''),
            (string) ($data['label'] ?? ''),
            (bool) ($data['active'] ?? true),
            (string) ($data['user_id'] ?? ''),
            (string) ($data['created_at'] ?? ''),
            (string) ($data['updated_at'] ?? ''),
        );
    }
}
