# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Upsun CLI is a Go-based command-line interface for [Upsun](https://upsun.com). The CLI is a hybrid system that wraps a legacy PHP CLI (located in the `legacy/` subdirectory) while providing new Go-based commands. It supports multiple vendors through build tags and configuration files.

## Build and Test Commands

All targets are in the root `Makefile`, including those for the legacy PHP CLI. Run `make` to list them.

Build a single binary for your platform:
```bash
make single
```

Build a snapshot for all platforms:
```bash
make snapshot
```

Run tests (Go and PHP unit tests):
```bash
make test
# or separately:
make test-go
make test-php
```

Run linters (Go and PHP):
```bash
make lint
# or separately:
make lint-go
make lint-php
```

Remove the phar, command indexes, legacy dependencies and caches (`clean-all` also removes PHP binaries and `dist/`):
```bash
make clean
```

The phar is rebuilt when files in `legacy/` change. Building it installs `legacy/vendor` without dev dependencies; the PHP lint and test targets reinstall them.

Format code:
```bash
go fmt ./...
```

Tidy dependencies:
```bash
go mod tidy
```

Run a single test:
```bash
go test -v -run TestName ./path/to/package
```

## Architecture

### Hybrid CLI System

The CLI operates as a wrapper around a legacy PHP CLI:
- Go layer: Handles new commands (init, list, version, config:install, project:convert, lint) and core infrastructure
- PHP layer: Legacy commands are proxied through `internal/legacy/CLIWrapper`
- The PHP CLI (platform.phar) is embedded at build time via go:embed
- An index of legacy commands (`commands-<flavor>.json`, from `list --all --format=json` with the embedded config) is embedded too, so the Go layer can resolve abbreviations like `p:init` in the same way as Symfony Console, and answer shell completion (`_complete`) for command and option names; PHP is only started for argument and option values, or when a user config file or environment variable may change the commands

### Key Components

**Entry Point**: `cmd/platform/main.go`
- Loads configuration from YAML (embedded or external)
- Sets up Viper for environment variable handling
- Delegates to commands package

**Commands**: `commands/`
- `root.go`: Root command that sets up the Cobra CLI and delegates to legacy CLI when needed
- Native Go commands: init, list, version, config:install, project:convert, completion, lint, and with Go auth (below): auth:browser-login (login), auth:api-token-login, auth:logout (logout), auth:token
- `lint.go`: Native config linter, `lint` (also `validate`; registered under the namespaced name `app:config-validate`). Validates Flex (`.upsun`) and Fixed (`.platform`) config in `internal/lint`, reporting all errors at once
- Unrecognized commands are passed to the legacy PHP CLI

**Configuration**: `internal/config/`
- `schema.go`: Config struct definition with validation tags
- `version.go`: Uses runtime/debug to get version info from VCS
- Supports vendorization through embedded YAML configs:
  - `config_upsun.go`: Default (no build tags) - Upsun CLI
  - `config_vendor.go`: Requires `-tags vendor` - Custom vendor CLI
- Config can be loaded from external files for testing/development

**Legacy Integration**: `internal/legacy/` and `legacy/`
- `internal/legacy/legacy.go`: CLIWrapper that manages PHP binary and phar execution
- `legacy/`: The legacy PHP CLI codebase (subtree merged from legacy-cli 5.x)
- PHP binaries are embedded per platform via go:embed and build tags
- Uses file locking to prevent concurrent initialization
- Copies PHP binary and phar to cache directory on first run
- The legacy CLI caches API data under the current user's ID (`Api::userCache()`), read from the access token's claims (refreshing it first if it has expired) or from the API, so cached data is not shared between users

**API Client**: `internal/api/`
- HTTP client for interacting with Upsun API
- Handles authentication, organizations, and resource management

**Authentication**: `internal/auth/`
- Authentication is moving from PHP to Go, behind a feature flag: `<PREFIX>GO_AUTH=1` (an env var for us and support, read by both Go and PHP). Without it, the legacy PHP CLI handles auth as before, and Go gets tokens from it (`LegacyCLIClient`)
- With Go auth, Go is the only component that stores or refreshes credentials. `auth.Manager` resolves tokens (API tokens, `api.access_token`, stored sessions) and refreshes them under a per-session flock (`<writable dir>/auth/<id>.lock`), re-reading the store under the lock because refresh tokens rotate
- `internal/auth/store`: one entry per session ID, in the system keychain (go-keyring) or in `<writable dir>/auth/<id>.json`
- Auth settings are read by `config.Auth()` with the legacy CLI's precedence: embedded config, the user's `config.yaml`, env vars
- The legacy CLI gets tokens and auth state by running the hidden `auth:internal token|status` command (via `<PREFIX>WRAPPER_EXECUTABLE`), and Go runs the hidden PHP commands `auth:post-login` (SSH certificates and config) and `auth:export-sessions` (a one-time migration of the legacy storage, recorded in `auth/.migrated`)
- Integration tests run with legacy auth by default, and with Go auth if `TEST_CLI_GO_AUTH=1` is set. Tests of Go-only behavior use `newGoAuthCommandFactory`

**Table Output**: `internal/table/`
- Renders tables with the same `--format`, `--columns` and `--no-header` options as the legacy CLI's `Table` service
- The `csv`, `tsv` and `plain` formats match the legacy output, with ANSI escape sequences removed
- The `table` format wraps cells to the terminal width (a port of the legacy `AdaptiveTable`), and supports ANSI-styled cells

**Project Initialization**: `internal/init/`
- AI-powered project configuration generation
- Integrates with whatsun library for codebase analysis

### Build System

**Multi-Vendor Support**:
- Uses Go build tags to compile different binaries:
  - Default (no tags): Upsun CLI
  - `-tags vendor`: Custom vendor CLI
- Configuration is embedded at compile time
- GoReleaser builds multiple variants (upsun, platform, vendor-specific)

**PHP Binary Handling**:
- PHP binaries are downloaded from [upsun/cli-php-builds](https://github.com/upsun/cli-php-builds) releases
- All platforms use static binaries built with [static-php-cli](https://github.com/crazywhalecc/static-php-cli)
- Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Extensions included: curl, filter, openssl, pcntl (Unix), phar, posix (Unix), zlib
- Windows requires `cacert.pem` for OpenSSL (embedded separately)

**Downloading PHP Binaries**:
```bash
# Download PHP for current platform only (for development)
make php

# Download all PHP binaries (for release builds)
make download-php
```

**Upgrading PHP Version**:
1. Trigger the build workflow at [upsun/cli-php-builds](https://github.com/upsun/cli-php-builds/actions) with the new PHP version
2. Update `PHP_VERSION` in the Makefile
3. Run `make php` (or `make download-php`) to download the new binaries
4. Test and release

## Development Notes

### Testing

Tests use github.com/stretchr/testify for assertions. Table-driven tests are preferred with a "cases" slice containing simple test case structs.

Integration tests (`integration-tests/`) call `t.Parallel()`, so they must not share state: no `t.Setenv` or unsynchronized package variables, and use `cmdFactory.useConfig` to change the CLI config file. Write output with `t.Log` (e.g. `mockapi.RequestLogger`), not to stdout or stderr: it is then attributed to its test, and shown only on failure or with `-v`. Tests of environment operations use `setUpProject`, and tests of Git commands (push, clone, fetch) use `setUpGitProject`, which also serves a bare repository over the mock SSH server.

### Configuration

The CLI uses Viper for configuration. Environment variables use the prefix defined in the config (UPSUN_CLI_ or PLATFORM_CLI_). The prefix is set in the config YAML.

### Legacy CLI Interaction

When the root command receives arguments it doesn't recognize, it passes them to the legacy PHP CLI via CLIWrapper.Exec(). The PHP binary and phar are extracted to a cache directory on first use.

### Vendorization

To build a vendor-specific CLI:
```bash
make vendor-snapshot VENDOR_NAME='Vendor Name' VENDOR_BINARY='vendorcli'
make vendor-release VENDOR_NAME='Vendor Name' VENDOR_BINARY='vendorcli'
```

This requires a config file at `internal/config/embedded-config.yaml` (downloaded at build time).

### Version Information

Version information is obtained from Go's runtime/debug package (VCS info embedded at build time):
- `internal/config.Version`: Git tag/version (from VCS or module version)
- `internal/config.Commit`: Git commit hash (from vcs.revision)
- `internal/config.Date`: Build date (from vcs.time)

PHP version is still injected via ldflags:
- `internal/legacy.PHPVersion`: PHP version embedded

### Update Checks

The CLI checks for updates from GitHub releases (when Wrapper.GitHubRepo is set in config). The network check runs in a background goroutine and caches the latest known version in `state.json`; the notice is shown before the command on a later run (see `internal/update.go`). When a check is running, the CLI waits up to 1 second for it before exiting.

Install-method detection (`internal/install.go`) tailors or suppresses the notice:
- System package managers (apt, yum/dnf, apk) are detected via a marker file installed by the nfpm packages (`packaging/install-source` → `/usr/share/<slug>/install-source`). Before printing a notice, older package installs without the marker are also detected by querying dpkg/rpm/apk. The notice is suppressed because the OS handles updates.
- Homebrew, Scoop, npm, and the bash installer get a tailored upgrade command, built from config fields (`Wrapper.HomebrewTap`, `Wrapper.NpmPackage`, `Wrapper.InstallerURL`, `Application.Executable`).
- The notice is throttled to once a week (`LastNotified` in state) and only shown in an interactive terminal.
- `<PREFIX>INSTALL_METHOD` forces the method; `<PREFIX>UPDATES_CHECK=0` disables checks entirely.
- With `CLI_CONFIG_FILE` (e.g. a `config:install` shim), the `wrapper.*` keys and the application name, executable and slug come from the embedded config (`Config.Embedded()`), as the binary is the same. The notice is only shown if there is an upgrade command for the install method, as the binary may be a private copy.

See `docs/design/update-message-install-detection.md` for the full design, including the planned Phase 2 (opt-in self-update).
