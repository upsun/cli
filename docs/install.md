# Installation

## Installer script

[`installer.sh`](../installer.sh) installs the CLI on Linux, macOS, FreeBSD, and the Windows Subsystem for Linux (WSL):

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | bash
```

### Install method

On Linux, the script uses apt, yum, or apk, and on macOS, Homebrew, if available. Otherwise it uses the `raw` method: a binary download from the GitHub release. It also uses the `raw` method on FreeBSD, on Linux in CI or when `VERSION` is set, and for `VERSION=nightly`.

The `raw` method verifies the archive against the release's `checksums.txt` (SHA-256). It needs `sha256sum` or `shasum`, and fails if neither is available, if `checksums.txt` cannot be downloaded, or if the checksum does not match.

The `raw` method installs in `/usr/bin` (`/usr/local/bin` on macOS), or in `~/.global/bin` or `~/.local/bin` if either is in the `PATH`. It uses `sudo` if needed, or `~/.local/bin` if `sudo` is not available.

Outside CI, the `raw` method also installs shell completion for the current user, without editing shell configuration files: bash completion in `~/.local/share/bash-completion/completions` (loaded by bash-completion 2), and fish completion in `~/.config/fish/completions` when available. For zsh users (by `$SHELL`), it installs `~/.local/share/upsun/completion.zsh` and prints the line to add to `~/.zshrc`.

### Environment variables

* `INSTALL_LOG`: the install log file
* `INSTALL_METHOD`: force an install method: `apt`, `yum`, `apk`, `homebrew` (or `brew`), or `raw`
* `INSTALL_DIR`: the installation directory for the `raw` method; for example, `INSTALL_DIR=$HOME/.local/bin` for a single-user installation
* `VERSION`: the version of the CLI to install, if not the latest one, or `nightly` for the [nightly build](development.md#nightly-builds)
* `CONFIG_URL`: the URL of an alternative CLI configuration, installed after the CLI with `upsun config:install --force`. Its executable is put next to `upsun` if that directory is writable (except with Homebrew). As root, outside the home directory, the configuration is put in `/etc/upsun/platform-alt` so that other users can run it; only root updates it

### Examples

Use the `raw` method:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | INSTALL_METHOD=raw bash
```

Install a specific version:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | VERSION=5.0.0 bash
```

Install the nightly build, of unreleased code:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | VERSION=nightly bash
```

Install in a directory owned by the user:

```console
curl -fsSL https://raw.githubusercontent.com/upsun/cli/main/installer.sh | INSTALL_METHOD=raw INSTALL_DIR=$HOME/.local/bin bash
```

## Package repositories

The installer script adds these repositories itself. To add them by hand:

### Alpine

```console
# Add the signing key and repository
sudo mkdir -p /etc/apk/keys
sudo curl -fsSL -o /etc/apk/keys/repositories-upsun-com.rsa.pub https://repositories.upsun.com/alpine/repositories-upsun-com.rsa.pub
echo "https://repositories.upsun.com/alpine" | sudo tee -a /etc/apk/repositories

# Install the CLI
sudo apk add upsun-cli
```

To upgrade:

```console
sudo apk update && sudo apk upgrade upsun-cli
```

### Ubuntu/Debian

```console
# Add the signing key and repository
sudo mkdir -p /etc/apt/keyrings
sudo curl -fsSL https://repositories.upsun.com/gpg.key -o /etc/apt/keyrings/upsun.asc
echo "deb [signed-by=/etc/apt/keyrings/upsun.asc] https://repositories.upsun.com/debian stable main" | sudo tee /etc/apt/sources.list.d/upsun.list

# Install the CLI
sudo apt-get update
sudo apt-get install -y upsun-cli
```

To upgrade:

```console
sudo apt-get update && sudo apt-get upgrade upsun-cli
```

### RPM (Fedora, RHEL, CentOS, AlmaLinux, Rocky)

```console
# Add the repository
sudo tee /etc/yum.repos.d/upsun.repo << 'EOF'
[upsun]
name=Upsun CLI
baseurl=https://repositories.upsun.com/rpm/$basearch
enabled=1
gpgcheck=1
gpgkey=https://repositories.upsun.com/gpg.key
EOF

# Install the CLI (use yum on older systems)
sudo dnf install -y upsun-cli
```

To upgrade:

```console
sudo dnf upgrade -y upsun-cli
```

## npm

The npm package needs Node.js 18 or later. It has builds for Linux (x64 and arm64), macOS, and Windows (x64), so not for 32-bit Linux or Windows on ARM.
