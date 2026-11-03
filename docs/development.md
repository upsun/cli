# Development

See also [CLAUDE.md](../CLAUDE.md) for the architecture and test commands.

## Building

List the available `make` targets:

```console
make
```

Build a single binary:

```console
make single
```

Build a snapshot:

```console
make snapshot
```

Build a snapshot for a vendor:

```console
# Download the config file at internal/config/embedded-config.yaml
make vendor-snapshot VENDOR_NAME='Vendor Name' VENDOR_BINARY='vendorcli'
```

## Testing a pull request

CI builds the CLI for each platform. For pull requests from branches in this
repository (not forks), a comment lists the builds, which can be downloaded with
a GitHub login for 7 days, e.g.:

```console
gh run download <run-id> -R upsun/cli -n cli-linux-amd64
chmod +x upsun
./upsun --version
```

On macOS, if you downloaded a build with a browser, remove the quarantine
attribute first: `xattr -d com.apple.quarantine upsun`.

## Releasing

Releases are automated via GitHub Actions. To create a new release:

1. Optionally, write the release notes in a draft release on GitHub. Its title
   must be exactly the new tag (e.g. `v5.0.0`), or the workflow won't find it.
   Save it as a draft: don't publish it. Releases are immutable, so assets can't
   be added once a release is published.

2. Create and push the tag:
   ```console
   git tag -m 'Release v5.0.0' 'v5.0.0'
   git push origin v5.0.0
   ```

3. The [Release workflow](../.github/workflows/release.yml) will automatically:
   - Build binaries for all platforms
   - Sign packages (APK, DEB, RPM)
   - Upload all artifacts to the draft release (or create one with generated
     notes), and publish it
   - Update package repositories at repositories.upsun.com
   - Build and push Docker image to ghcr.io/upsun/cli
   - Publish the npm packages
   - Submit the winget manifests to microsoft/winget-pkgs

Release notes can still be edited after publishing.
