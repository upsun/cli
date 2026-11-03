# Upsun CLI

The command-line interface for [Upsun](https://upsun.com).

Use it to manage Upsun projects from the terminal: generate and validate project configuration, create and branch environments, deploy with Git, SSH into apps, view logs, open tunnels to services, and manage backups, variables, domains and users.

## Install

### Installer script

On Linux, macOS, or the Windows Subsystem for Linux (WSL), run:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | bash
```

The script uses apt, yum, or apk on Linux, or Homebrew on macOS, if available, or else downloads the binary. For its options, and to add the package repositories yourself, see [docs/install.md](docs/install.md).

### Windows

On Windows, install the CLI in WSL with the installer script above.

To install it on Windows itself, use [Scoop](https://scoop.sh/). Scoop needs Git to add a bucket:

```console
scoop install git
scoop bucket add upsun https://github.com/upsun/homebrew-tap.git
scoop install upsun
```

### Homebrew

```console
brew install upsun/tap/upsun-cli
```

### npm

```console
npm install -g upsun
```

npm installs don't set up shell completion. For the package's requirements, see [docs/install.md](docs/install.md#npm).

### Nix

```console
nix profile install nixpkgs#upsun
```

### Docker

```console
docker run --rm -it ghcr.io/upsun/cli
```

### Manual installation

For manual installation, you can also [download the latest binaries](https://github.com/upsun/cli/releases/latest).

To try unreleased changes, install the [nightly build](docs/development.md#nightly-builds) with `VERSION=nightly` (see [docs/install.md](docs/install.md#examples)).

## Getting started

```console
upsun login            # Log in, in a browser
upsun projects         # List your projects
upsun get <project>    # Clone a project's repository
upsun list             # List all commands
```

For more, see the [CLI documentation](https://developer.upsun.com/cli).

## Upgrade

Upgrade with the tool you installed the CLI with:

| Method | Command |
|---|---|
| Installer script | `curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh \| bash` |
| Scoop | `scoop update upsun` |
| Homebrew | `brew update && brew upgrade upsun/tap/upsun-cli` |
| npm | `npm install -g upsun@latest` |
| apt, dnf/yum, apk | Your system's package manager: see [docs/install.md](docs/install.md#package-repositories) |

### Update notifications

When a newer release is available, the CLI prints the upgrade command for your
install method, at most once a week, in an interactive terminal. It prints
nothing for system packages, as the OS updates them.

- `UPSUN_CLI_UPDATES_CHECK=0` disables update checks.
- `UPSUN_CLI_INSTALL_METHOD=<method>` overrides the detected install method:
  `homebrew`, `scoop`, `npm`, `winget`, `package`, or `script`.

## Configuration

You can override configuration in the user config file,
`~/.upsun-cli/config.yaml`. The available keys are in
[legacy/config-defaults.yaml](legacy/config-defaults.yaml). For environment
variables, see [docs/configuration.md](docs/configuration.md).

## Development

To build or release the CLI, see [docs/development.md](docs/development.md).

## Licenses

This product includes PHP software, freely available from [the PHP website](https://www.php.net/software). The CLI redistributes PHP in binary form, under the [PHP License](https://www.php.net/license/3_01.txt).
