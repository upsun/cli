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
 *
 * Keys from the Accounts API are converted with fromLegacyData().
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
     * @param array<mixed> $data
     */
    public static function fromData(array $data): self
    {
        return new self(
            self::str($data, 'id'),
            self::str($data, 'sha256'),
            self::str($data, 'value'),
            self::str($data, 'label'),
            (bool) ($data['active'] ?? true),
            self::str($data, 'user_id'),
            self::str($data, 'created_at'),
            self::str($data, 'updated_at'),
        );
    }

    /**
     * Creates a key from the legacy Accounts API representation.
     *
     * Legacy keys have no active flag, so they are always active, and their
     * MD5 fingerprint is replaced with a SHA-256 one computed from the value.
     *
     * @param array<mixed> $data
     */
    public static function fromLegacyData(array $data): self
    {
        $value = self::str($data, 'value');
        $id = $data['key_id'] ?? '';

        return new self(
            is_int($id) || is_string($id) ? (string) $id : '',
            self::fingerprint($value) ?? '',
            $value,
            self::str($data, 'title'),
            true,
            '',
            '',
            '',
        );
    }

    /**
     * Returns the SHA-256 fingerprint of a public key, in OpenSSH format.
     *
     * The format is the one reported by `ssh-keygen -l`: "SHA256:" followed
     * by the unpadded base64 of the hash.
     *
     * @param string $publicKey The public key, in OpenSSH format.
     *
     * @return string|null The fingerprint, or null if the key is not valid.
     */
    public static function fingerprint(string $publicKey): ?string
    {
        if (!str_contains($publicKey, ' ')) {
            return null;
        }
        [, $keyB64] = \explode(' ', $publicKey, 3);
        $key = \base64_decode($keyB64, true);
        if ($key === false) {
            return null;
        }

        return 'SHA256:' . \rtrim(\base64_encode(\hash('sha256', $key, true)), '=');
    }

    /**
     * @param array<mixed> $data
     */
    private static function str(array $data, string $key): string
    {
        return isset($data[$key]) && is_string($data[$key]) ? $data[$key] : '';
    }
}
