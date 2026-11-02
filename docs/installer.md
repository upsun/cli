# Installer script

[`installer.sh`](../installer.sh) installs the CLI on Linux, macOS, FreeBSD, and the Windows Subsystem for Linux (WSL):

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | bash
```

## Install method

On Linux, the script uses apt, yum, or apk, and on macOS, Homebrew, if available. Otherwise it uses the `raw` method: a binary download from the GitHub release. It also uses the `raw` method on FreeBSD, and on Linux in CI or when `VERSION` is set.

The `raw` method verifies the archive against the release's `checksums.txt` (SHA-256). It needs `sha256sum` or `shasum`, and fails if neither is available, if `checksums.txt` cannot be downloaded, or if the checksum does not match.

The `raw` method installs in `/usr/bin` (`/usr/local/bin` on macOS), or in `~/.global/bin` or `~/.local/bin` if either is in the `PATH`. It uses `sudo` if needed, or `~/.local/bin` if `sudo` is not available.

Outside CI, the `raw` method also installs shell completion for the current user, without editing shell configuration files: bash completion in `~/.local/share/bash-completion/completions` (loaded by bash-completion 2), and fish completion in `~/.config/fish/completions` when available. For zsh users (by `$SHELL`), it installs `~/.local/share/upsun/completion.zsh` and prints the line to add to `~/.zshrc`.

## Environment variables

* `INSTALL_LOG`: the install log file
* `INSTALL_METHOD`: force an install method: `apt`, `yum`, `apk`, `homebrew` (or `brew`), or `raw`
* `INSTALL_DIR`: the installation directory for the `raw` method; for example, `INSTALL_DIR=$HOME/.local/bin` for a single-user installation
* `VERSION`: the version of the CLI to install, if not the latest one
* `CONFIG_URL`: the URL of an alternative CLI configuration, installed after the CLI with `upsun config:install --force`. Its executable is put next to `upsun` if that directory is writable (except with Homebrew). As root, outside the home directory, the configuration is put in `/etc/upsun/platform-alt` so that other users can run it; only root updates it

## Examples

Use the `raw` method:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | INSTALL_METHOD=raw bash
```

Install a specific version:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | VERSION=5.0.0 bash
```

Install in a directory owned by the user:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | INSTALL_METHOD=raw INSTALL_DIR=$HOME/.local/bin bash
```
