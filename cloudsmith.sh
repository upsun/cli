#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${VERSION:-}" ]]; then
  echo "Error: VERSION environment variable is not set. Please set VERSION (e.g., '1.2.3') before running cloudsmith.sh." >&2
  exit 1
fi
# Upload Platform.sh packages
cloudsmith push deb platformsh/cli/any-distro/any-version dist/platformsh-cli_${VERSION}_linux_arm64.deb
cloudsmith push deb platformsh/cli/any-distro/any-version dist/platformsh-cli_${VERSION}_linux_amd64.deb

cloudsmith push alpine platformsh/cli/alpine/any-version dist/platformsh-cli_${VERSION}_linux_amd64.apk
cloudsmith push alpine platformsh/cli/alpine/any-version dist/platformsh-cli_${VERSION}_linux_arm64.apk

cloudsmith push rpm platformsh/cli/any-distro/any-version dist/platformsh-cli_${VERSION}_linux_amd64.rpm
cloudsmith push rpm platformsh/cli/any-distro/any-version dist/platformsh-cli_${VERSION}_linux_arm64.rpm

cloudsmith push raw platformsh/cli dist/platform_${VERSION}_linux_amd64.tar.gz --version ${VERSION}
cloudsmith push raw platformsh/cli dist/platform_${VERSION}_linux_arm64.tar.gz --version ${VERSION}

# Upload Upsun packages
cloudsmith push deb platformsh/upsun-cli/any-distro/any-version dist/upsun-cli_${VERSION}_linux_arm64.deb
cloudsmith push deb platformsh/upsun-cli/any-distro/any-version dist/upsun-cli_${VERSION}_linux_amd64.deb

cloudsmith push alpine platformsh/upsun-cli/alpine/any-version dist/upsun-cli_${VERSION}_linux_amd64.apk
cloudsmith push alpine platformsh/upsun-cli/alpine/any-version dist/upsun-cli_${VERSION}_linux_arm64.apk

cloudsmith push rpm platformsh/upsun-cli/any-distro/any-version dist/upsun-cli_${VERSION}_linux_amd64.rpm
cloudsmith push rpm platformsh/upsun-cli/any-distro/any-version dist/upsun-cli_${VERSION}_linux_arm64.rpm

cloudsmith push raw platformsh/upsun-cli dist/upsun_${VERSION}_linux_amd64.tar.gz --version ${VERSION}
cloudsmith push raw platformsh/upsun-cli dist/upsun_${VERSION}_linux_arm64.tar.gz --version ${VERSION}
