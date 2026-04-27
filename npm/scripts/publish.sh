#!/usr/bin/env bash
# Publishes the npm tarballs produced by build.sh.
#
# Inputs (env vars):
#   OUT_DIR      Where build.sh wrote tarballs. Default: npm/dist
#   NPM_TAG      dist-tag, e.g. "latest" or "next". Default: "latest"
#   DRY_RUN      1 to run npm publish --dry-run. Default: 0
#
# Auth: requires ~/.npmrc to have a working //registry.npmjs.org/:_authToken,
# or NODE_AUTH_TOKEN set with a registry-url-configured ~/.npmrc (the
# setup-node action handles this in CI).
#
# Order: platform packages first, then wait for them to become visible
# in the public registry, then publish the wrapper. The wait matters:
# npm publish returns success before the new package is queryable via
# `npm view`. If a user runs `npx upsun` in that window, npm fails to
# resolve the wrapper's optionalDependencies, treats them as failed
# (which is silent for optional deps), and caches a broken install in
# ~/.npm/_npx that will not self-heal on retry.

set -euo pipefail

NPM_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-${NPM_DIR}/dist}"
NPM_TAG="${NPM_TAG:-latest}"
DRY_RUN="${DRY_RUN:-0}"

if [ ! -d "${OUT_DIR}" ]; then
  echo "publish.sh: OUT_DIR not found: ${OUT_DIR}. Run build.sh first." >&2
  exit 1
fi

shopt -s nullglob
all_tarballs=("${OUT_DIR}"/*.tgz)
shopt -u nullglob

if [ ${#all_tarballs[@]} -eq 0 ]; then
  echo "publish.sh: no tarballs in ${OUT_DIR}" >&2
  exit 1
fi

# Classify each tarball by reading its package.json once: the wrapper is
# the one named "upsun"; everything else is a platform package. Cache
# name and version so the propagation wait does not re-open the tarball.
declare -A NAME_OF VERSION_OF
platform_tarballs=()
wrapper_tarballs=()
for t in "${all_tarballs[@]}"; do
  pkg_json=$(tar -xzOf "$t" package/package.json)
  NAME_OF["$t"]=$(awk -F'"' '/"name":/ { print $4; exit }' <<<"$pkg_json")
  VERSION_OF["$t"]=$(awk -F'"' '/"version":/ { print $4; exit }' <<<"$pkg_json")
  if [ "${NAME_OF[$t]}" = "upsun" ]; then
    wrapper_tarballs+=("$t")
  else
    platform_tarballs+=("$t")
  fi
done

publish_one() {
  local tarball="$1"
  local args=(publish "$tarball" --access public --tag "${NPM_TAG}")
  if [ "${DRY_RUN}" = "1" ]; then args+=(--dry-run); fi
  echo "  npm ${args[*]}"
  npm "${args[@]}"
}

wait_visible() {
  local pkg="$1"
  local version="$2"
  local deadline=$(($(date +%s) + 300))
  while ! npm view "${pkg}@${version}" version >/dev/null 2>&1; do
    if [ "$(date +%s)" -gt "$deadline" ]; then
      echo "publish.sh: timed out waiting for ${pkg}@${version} to propagate" >&2
      exit 1
    fi
    echo "  waiting for ${pkg}@${version}..."
    sleep 5
  done
  echo "  ${pkg}@${version} visible"
}

echo "publish.sh: publishing platform packages"
for t in "${platform_tarballs[@]}"; do publish_one "$t"; done

if [ "${DRY_RUN}" != "1" ]; then
  echo "publish.sh: waiting for platform packages to propagate"
  for t in "${platform_tarballs[@]}"; do
    wait_visible "${NAME_OF[$t]}" "${VERSION_OF[$t]}"
  done
fi

echo "publish.sh: publishing wrapper"
for t in "${wrapper_tarballs[@]}"; do publish_one "$t"; done

echo "publish.sh: done"
