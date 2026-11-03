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

## Nightly builds

The [Nightly workflow](../.github/workflows/nightly.yml) builds `main` every day, if it has changed, and replaces the assets of the `nightly` prerelease in [upsun/cli-nightly](https://github.com/upsun/cli-nightly/releases/tag/nightly). It uses a separate repository because releases here are immutable. Its version is the next patch version, e.g. `5.15.3-nightly.20261103+abc1234`, and its asset names have `nightly` in place of the version.

Install it with `VERSION=nightly` (see [install.md](install.md#examples)), or download it from the release. It only publishes archives and checksums: not packages, Homebrew, Scoop, npm, winget or Docker. Its update check reads stable releases in this repository as usual, so it only shows a notice for a newer stable release.

Run the workflow manually to build again (`force`), or to build without publishing (`dry_run`, the assets are kept as a workflow artifact). A run on another branch publishes that branch, until the next scheduled run.

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
