#!/usr/bin/env bash
# Assembles npm packages from GoReleaser archives.
#
# Inputs (env vars, all optional):
#   DIST_DIR  Directory containing GoReleaser archives. Default: <repo>/dist
#   VERSION   Package version. Default: derived from the first matching archive name.
#   OUT_DIR   Where to write per-package working dirs and tarballs. Default: npm/dist
#
# Produces:
#   upsun                  (wrapper, with the four platforms below as optionalDependencies)
#   @upsun/cli-linux-x64
#   @upsun/cli-linux-arm64
#   @upsun/cli-darwin      (universal binary; covers x64 and arm64)
#   @upsun/cli-win32-x64

set -euo pipefail

NPM_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${NPM_DIR}/.." && pwd)"

DIST_DIR="${DIST_DIR:-${REPO_ROOT}/dist}"
OUT_DIR="${OUT_DIR:-${NPM_DIR}/dist}"

if [ ! -d "${DIST_DIR}" ]; then
  echo "build.sh: DIST_DIR not found: ${DIST_DIR}" >&2
  echo "Run 'goreleaser release --snapshot --clean' first, or point DIST_DIR at the archives." >&2
  exit 1
fi

# Per-suffix metadata. Implemented as case statements rather than
# associative arrays so the script works on macOS's default Bash 3.2.
# The darwin entry has a permissive cpu list because macOS ships a
# single universal binary that runs on both Apple Silicon and Intel.
PLATFORMS=(linux-x64 linux-arm64 darwin win32-x64)

archive_glob_for() {
  case "$1" in
    linux-x64)   echo "upsun_*_linux_amd64.tar.gz" ;;
    linux-arm64) echo "upsun_*_linux_arm64.tar.gz" ;;
    darwin)      echo "upsun_*_darwin_all.tar.gz" ;;
    win32-x64)   echo "upsun_*_windows_amd64.zip" ;;
    *) echo "build.sh: unsupported platform suffix: $1" >&2; exit 1 ;;
  esac
}

bin_name_for() {
  case "$1" in
    linux-x64|linux-arm64|darwin) echo "upsun" ;;
    win32-x64) echo "upsun.exe" ;;
    *) echo "build.sh: unsupported platform suffix: $1" >&2; exit 1 ;;
  esac
}

os_json_for() {
  case "$1" in
    linux-x64|linux-arm64) echo '["linux"]' ;;
    darwin)    echo '["darwin"]' ;;
    win32-x64) echo '["win32"]' ;;
    *) echo "build.sh: unsupported platform suffix: $1" >&2; exit 1 ;;
  esac
}

cpu_json_for() {
  case "$1" in
    linux-x64) echo '["x64"]' ;;
    linux-arm64) echo '["arm64"]' ;;
    darwin)    echo '["x64","arm64"]' ;;
    win32-x64) echo '["x64"]' ;;
    *) echo "build.sh: unsupported platform suffix: $1" >&2; exit 1 ;;
  esac
}

description_for() {
  case "$1" in
    linux-x64)   echo "Upsun CLI binary for Linux x64" ;;
    linux-arm64) echo "Upsun CLI binary for Linux arm64" ;;
    darwin)      echo "Upsun CLI binary for macOS (universal)" ;;
    win32-x64)   echo "Upsun CLI binary for Windows x64" ;;
    *) echo "build.sh: unsupported platform suffix: $1" >&2; exit 1 ;;
  esac
}

if [ -z "${VERSION:-}" ]; then
  shopt -s nullglob
  matches=("${DIST_DIR}"/upsun_*_linux_amd64.tar.gz)
  shopt -u nullglob
  if [ ${#matches[@]} -eq 0 ]; then
    echo "build.sh: no upsun_*_linux_amd64.tar.gz in ${DIST_DIR}; set VERSION explicitly" >&2
    exit 1
  fi
  base="$(basename "${matches[0]}")"
  # upsun_X.Y.Z_linux_amd64.tar.gz -> X.Y.Z
  VERSION="${base#upsun_}"
  VERSION="${VERSION%_linux_amd64.tar.gz}"
fi

echo "build.sh: VERSION=${VERSION}"

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

build_platform_pkg() {
  local suffix="$1"
  local glob; glob="$(archive_glob_for "$suffix")"
  local bin; bin="$(bin_name_for "$suffix")"
  local name="@upsun/cli-${suffix}"

  shopt -s nullglob
  # shellcheck disable=SC2206 # intentional glob expansion
  local archives=("${DIST_DIR}"/${glob})
  shopt -u nullglob
  if [ ${#archives[@]} -eq 0 ]; then
    echo "build.sh: no archive matching ${glob} in ${DIST_DIR}" >&2
    exit 1
  fi
  local archive="${archives[0]}"

  local pkg_dir="${OUT_DIR}/${suffix}"
  mkdir -p "${pkg_dir}/bin"

  case "${archive}" in
    *.tar.gz) tar -xzf "${archive}" -C "${pkg_dir}/bin" "${bin}" ;;
    *.zip)    unzip -p "${archive}" "${bin}" > "${pkg_dir}/bin/${bin}" ;;
    *) echo "build.sh: unsupported archive: ${archive}" >&2; exit 1 ;;
  esac
  # The exec bit is meaningless on the Windows binary, so a chmod failure
  # there is benign; on Unix targets a failure means the binary won't run.
  if [ "${suffix}" = "win32-x64" ]; then
    chmod +x "${pkg_dir}/bin/${bin}" || true
  else
    chmod +x "${pkg_dir}/bin/${bin}"
  fi

  sed \
    -e "s|__PKG_NAME__|${name}|g" \
    -e "s|__VERSION__|${VERSION}|g" \
    -e "s|__DESCRIPTION__|$(description_for "$suffix")|g" \
    -e "s|__OS__|$(os_json_for "$suffix")|g" \
    -e "s|__CPU__|$(cpu_json_for "$suffix")|g" \
    "${NPM_DIR}/platform-template/package.json.tmpl" > "${pkg_dir}/package.json"

  sed -e "s|__PKG_NAME__|${name}|g" \
    "${NPM_DIR}/platform-template/README.md.tmpl" > "${pkg_dir}/README.md"

  (cd "${pkg_dir}" && npm pack --pack-destination "${OUT_DIR}" >/dev/null)
  echo "  packed ${name}@${VERSION}"
}

build_wrapper_pkg() {
  local pkg_dir="${OUT_DIR}/wrapper"
  mkdir -p "${pkg_dir}/bin"

  sed -e "s|__VERSION__|${VERSION}|g" \
    "${NPM_DIR}/wrapper/package.json.tmpl" > "${pkg_dir}/package.json"

  cp "${NPM_DIR}/wrapper/bin/upsun.js" "${pkg_dir}/bin/upsun.js"
  chmod +x "${pkg_dir}/bin/upsun.js"

  cp "${NPM_DIR}/wrapper/bin/postinstall.js" "${pkg_dir}/bin/postinstall.js"
  chmod +x "${pkg_dir}/bin/postinstall.js"

  cp "${NPM_DIR}/wrapper/README.md" "${pkg_dir}/README.md"

  (cd "${pkg_dir}" && npm pack --pack-destination "${OUT_DIR}" >/dev/null)
  echo "  packed upsun@${VERSION}"
}

echo "build.sh: building platform packages"
for suffix in "${PLATFORMS[@]}"; do
  build_platform_pkg "$suffix"
done

echo "build.sh: building wrapper package"
build_wrapper_pkg

echo "build.sh: done. Tarballs in ${OUT_DIR}:"
ls -1 "${OUT_DIR}"/*.tgz
