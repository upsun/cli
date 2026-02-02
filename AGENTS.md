# AGENTS.md

This file provides guidance to AI coding agents (GitHub Copilot, Claude, Cursor, etc.) when working with code in this repository.

## Project Overview

The Upsun CLI is a Go-based command-line interface for [Upsun](https://upsun.com). It's a **hybrid system** that wraps a legacy PHP CLI while providing new Go-based commands. The CLI supports multiple vendors through build tags and configuration files.

### Repository Structure

```
├── cmd/platform/          # Go CLI entry point
├── commands/              # Go command implementations (Cobra)
├── internal/              # Go internal packages
│   ├── api/               # HTTP API client
│   ├── auth/              # OAuth2 authentication
│   ├── config/            # Configuration loading and schema
│   │   └── alt/           # Alternative CLI configuration management
│   ├── convert/           # Platform.sh to Upsun config conversion
│   ├── file/              # File utilities (atomic writes)
│   ├── init/              # AI-powered project initialization
│   ├── legacy/            # PHP CLI wrapper and embedding
│   ├── state/             # Persistent state (JSON)
│   └── version/           # Version comparison utilities
├── integration-tests/     # Integration tests (run against mock API)
├── legacy/                # Legacy PHP CLI (subtree from platformsh/legacy-cli)
│   ├── src/               # PHP source code
│   ├── tests/             # PHP unit tests
│   └── bin/platform       # PHP CLI entry point
├── pkg/
│   ├── mockapi/           # Mock HTTP API server for testing
│   └── mockssh/           # Mock SSH server for testing
└── scripts/               # Build and utility scripts
```

---

## Go CLI (Wrapper)

### Build Commands

**Prerequisites:** Before building, download the PHP binary and build the legacy CLI phar:

```bash
make php internal/legacy/archives/platform.phar
```

Then build:

```bash
# Build a single binary for your platform
make single

# Build a snapshot for all platforms
make snapshot

# Download all PHP binaries (for release builds)
make download-php
```

### Test Commands

**Prerequisites:** Before running tests, ensure the PHP binary and phar are built:

```bash
make php internal/legacy/archives/platform.phar
```

Then run tests:

```bash
# Run unit tests (excludes integration tests)
make test
# Or directly:
GOEXPERIMENT=jsonv2 go test -v -race -cover -count=1 $(go list ./... | grep -v /integration-tests)

# Run a single test
go test -v -run TestName ./path/to/package

# Run integration tests (requires built CLI in dist/)
make single
make integration-test
```

### Lint Commands

```bash
# Run all linters
make lint

# Individual linters
make lint-gomod      # go mod tidy -diff
make lint-golangci   # golangci-lint
```

### Format Code

```bash
go fmt ./...
go mod tidy
```

### Architecture

#### Hybrid CLI System

The CLI operates as a wrapper around a legacy PHP CLI:

- **Go layer**: Handles new commands (`init`, `list`, `version`, `config:install`, `project:convert`, `app:config-validate`) and core infrastructure
- **PHP layer**: Legacy commands are proxied through `internal/legacy/CLIWrapper`
- The PHP CLI (`platform.phar`) is embedded at build time via `go:embed`

When the root command receives arguments it doesn't recognize, it passes them to the legacy PHP CLI via `CLIWrapper.Exec()`.

#### Entry Point: `cmd/platform/main.go`

1. Loads configuration from YAML (embedded or external via `CLI_CONFIG_FILE` env var)
2. Sets up Viper for environment variable handling
3. Delegates to `commands.Execute()`

#### Commands Package: `commands/`

- `root.go`: Root Cobra command, delegates unrecognized commands to legacy CLI
- Native Go commands: `init.go`, `list.go`, `version.go`, `config_install.go`, `project_convert.go`, `completion.go`
- `list_models.go`: Defines `Command` struct for generating help pages compatible with legacy CLI format

#### Configuration: `internal/config/`

- `schema.go`: Config struct with validation tags (`go-playground/validator`)
- `config.go`: Loading from YAML, context helpers (`ToContext`/`FromContext`)
- `version.go`: Uses `runtime/debug` to get VCS version info
- Vendorization via embedded YAML configs:
  - `config_upsun.go`: Default (no build tags) - Upsun CLI
  - `config_platformsh.go`: Requires `-tags platformsh` - Platform.sh CLI
  - `config_vendor.go`: Requires `-tags vendor` - Custom vendor CLI

#### Legacy Integration: `internal/legacy/`

- `legacy.go`: `CLIWrapper` manages PHP binary and phar execution
- PHP binaries are embedded per platform via `go:embed` and build tags
- Uses file locking (`gofrs/flock`) to prevent concurrent initialization
- Copies PHP binary and phar to cache directory on first run

### Code Patterns and Conventions

#### Error Handling

Use `fmt.Errorf` with `%w` for error wrapping:

```go
if err != nil {
    return fmt.Errorf("could not load config: %w", err)
}
```

Use `errors.Is` and `errors.As` for error checking:

```go
if errors.Is(err, fs.ErrNotExist) {
    // Handle missing file
}
```

#### Context Usage

Configuration is stored in context:

```go
// Store config
ctx = config.ToContext(ctx, cnf)

// Retrieve config
cnf := config.FromContext(ctx)
```

#### Command Flags

Use Viper for flag binding:

```go
cmd.Flags().String("format", "txt", "Output format")
viper.BindPFlags(cmd.Flags()) //nolint:errcheck
```

Access flags via Viper:

```go
format := viper.GetString("format")
if viper.GetBool("no-interaction") {
    // Non-interactive mode
}
```

#### Interactive Prompts

Use `AlecAivazis/survey/v2` for interactive prompts:

```go
var confirm bool
prompt := &survey.Confirm{Message: "Continue?", Default: true}
if err := survey.AskOne(prompt, &confirm); err != nil {
    return err
}
```

#### Colored Output

Use `fatih/color` for colored output:

```go
fmt.Fprintln(stderr, color.YellowString("Warning:"), "message")
fmt.Fprintln(stderr, "Created:", color.GreenString(filePath))
```

#### Package Aliasing

When a package name conflicts with a variable or is a reserved word:

```go
import (
    _init "github.com/upsun/cli/internal/init"
)
```

#### Testing

Tests use `github.com/stretchr/testify` for assertions. Prefer table-driven tests:

```go
func TestSomething(t *testing.T) {
    cases := []struct {
        name     string
        input    string
        expected string
    }{
        {"empty", "", ""},
        {"basic", "foo", "FOO"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            result := Transform(tc.input)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

#### Integration Tests

Integration tests in `integration-tests/` run the CLI as a shell command:

- Tests require a built CLI binary (run `make single` first)
- Use `pkg/mockapi` for mocking the HTTP API
- Use `pkg/mockssh` for mocking SSH servers
- Tests use a separate `integration-tests/config.yaml`
- Set `TEST_CLI_PATH` env var to override CLI binary location

```go
func TestSomeCommand(t *testing.T) {
    authServer := mockapi.NewAuthServer(t)
    defer authServer.Close()

    apiHandler := mockapi.NewHandler(t)
    apiServer := httptest.NewServer(apiHandler)
    defer apiServer.Close()

    f := newCommandFactory(t, apiServer.URL, authServer.URL)
    output := f.Run("some-command", "--flag")
    assert.Contains(t, output, "expected")
}
```

### Adding a New Go Command

1. Create command file in `commands/` (e.g., `commands/mycommand.go`)
2. Define the Cobra command with `newMyCommand(cnf *config.Config) *cobra.Command`
3. Optionally define `innerMyCommand(cnf *config.Config) Command` for legacy-compatible help
4. Add command to `newRootCommand()` in `commands/root.go`
5. Add tests in same package or `integration-tests/`

### Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration and flag binding |
| `github.com/stretchr/testify` | Test assertions |
| `github.com/fatih/color` | Colored terminal output |
| `github.com/AlecAivazis/survey/v2` | Interactive prompts |
| `github.com/gofrs/flock` | File locking |
| `github.com/upsun/whatsun` | AI digest for project init |
| `github.com/upsun/lib-sun` | Config conversion utilities |
| `github.com/platformsh/platformify` | Project initialization scaffolding |
| `github.com/go-chi/chi/v5` | HTTP router (for mock API) |

---

## Legacy PHP CLI

The legacy PHP CLI lives in the `legacy/` subdirectory. It's a Symfony Console application that handles most CLI commands.

### Build Commands

```bash
cd legacy

# Install dependencies
composer install

# Run CLI from source
./bin/platform

# Build phar (from repo root)
make internal/legacy/archives/platform.phar
```

### Test Commands

```bash
cd legacy

# Run unit tests (excludes slow tests)
./vendor/bin/phpunit --exclude-group slow

# Run all tests
./vendor/bin/phpunit

# Run specific test
./vendor/bin/phpunit --filter testMethodName
```

### Lint Commands

```bash
cd legacy

# Run all linters
make lint

# Individual linters
make lint-phpstan         # Static analysis
make lint-php-cs-fixer    # Code style
```

### Architecture

#### Entry Point: `legacy/bin/platform`

Bootstrap script that loads the Symfony Console application.

#### Application: `legacy/src/Application.php`

- Extends Symfony Console Application
- Uses dependency injection container (`services.yaml`)
- Lazy-loads commands via Symfony's command loader
- Sets up event subscriber for Console events

#### Commands: `legacy/src/Command/`

All commands extend `CommandBase`:

- `CommandBase.php`: Base class with common functionality
- Commands are organized by namespace (e.g., `Project/`, `Environment/`, `Auth/`)
- Uses `#[AsCommand]` attribute for command metadata

#### Key Patterns

**Command Stability Levels:**

```php
protected string $stability = self::STABILITY_STABLE;  // or STABILITY_BETA, STABILITY_DEPRECATED
```

**Hidden Commands:**

```php
protected bool $hiddenInList = true;
```

**Service Injection:**

Commands use Symfony's `#[Required]` attribute for setter injection:

```php
#[Required]
public function setConfig(Config $config): void
{
    $this->config = $config;
}
```

#### Services: `legacy/src/Service/`

Business logic is in service classes, injected into commands.

#### Configuration: `legacy/config.yaml`

Main configuration file defining API endpoints, branding, and feature flags.

### Adding a New PHP Command

1. Create command class in `legacy/src/Command/` (use appropriate subdirectory)
2. Extend `CommandBase` or appropriate parent class
3. Use `#[AsCommand]` attribute:

```php
#[AsCommand(name: 'namespace:command', description: 'Description')]
class MyCommand extends CommandBase
{
    protected function configure(): void
    {
        $this->addArgument('name', InputArgument::REQUIRED, 'Description');
        $this->addOption('flag', 'f', InputOption::VALUE_NONE, 'Description');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        // Implementation
        return Command::SUCCESS;
    }
}
```

4. Tag the command in `legacy/config/services.yaml` if not auto-wired
5. Add tests in `legacy/tests/Command/`

### PHP Requirements

- PHP 8.2+
- Extensions: curl, filter, openssl, pcntl (Unix), phar, posix (Unix), zlib
- Composer for dependency management

---

## Multi-Vendor Support

The CLI supports building for different vendors (Upsun, Platform.sh, custom).

### Build Tags

```bash
# Upsun (default)
go build ./cmd/platform

# Platform.sh
go build -tags platformsh ./cmd/platform

# Custom vendor
go build -tags vendor ./cmd/platform
```

### Vendor Releases

```bash
# Build vendor snapshot
make vendor-snapshot VENDOR_NAME='Vendor Name' VENDOR_BINARY='vendorcli'

# Create vendor release
make vendor-release VENDOR_NAME='Vendor Name' VENDOR_BINARY='vendorcli'
```

Requires `internal/config/embedded-config.yaml` for custom vendor configuration.

---

## Version Information

Version info is obtained from Go's `runtime/debug` package (VCS info embedded at build time):

- `internal/config.Version`: Git tag/version
- `internal/config.Commit`: Git commit hash (from `vcs.revision`)
- `internal/config.Date`: Build date (from `vcs.time`)

PHP version is injected via ldflags:

- `internal/legacy.PHPVersion`: PHP version embedded in binary

---

## PHP Binary Management

PHP binaries are downloaded from [upsun/cli-php-builds](https://github.com/upsun/cli-php-builds) releases.

### Supported Platforms

- linux/amd64, linux/arm64
- darwin/amd64, darwin/arm64
- windows/amd64 (requires `cacert.pem` for OpenSSL)

### Upgrading PHP Version

1. Trigger the build workflow at [upsun/cli-php-builds](https://github.com/upsun/cli-php-builds/actions) with new PHP version
2. Update `PHP_VERSION` in `Makefile`
3. Run `make php` to download the new binary
4. Test and release

---

## Common Issues

### Lock File Issues

If the CLI hangs during initialization, check for stale lock files in the temp directory:

```bash
ls -la $(dirname $(mktemp -u))/*/legacy-*/.lock
```

### PHP Binary Not Found

Ensure PHP binary is downloaded before building:

```bash
make php
make single
```

### Integration Tests Skipped

Integration tests require a built CLI:

```bash
make single
make integration-test
```

### Config Validation Errors

Config files are validated using `go-playground/validator`. Check `internal/config/schema.go` for required fields and validation rules.
