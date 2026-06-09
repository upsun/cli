#!/usr/bin/env bash
# Publishes the npm tarballs produced by build.sh.
#
# Inputs (env vars):
#   OUT_DIR      Where build.sh wrote tarballs. Default: npm/dist
#   NPM_TAG      dist-tag, e.g. "latest" or "next". Default: "latest"
#   DRY_RUN      1 to run npm publish --dry-run. Default: 0
#
# Auth: in CI this runs under npm OIDC trusted publishing (no token; npm
# exchanges a GitHub Actions OIDC token automatically). Run by hand, it uses
# the standard npm mechanism: ~/.npmrc with a //registry.npmjs.org/:_authToken,
# or `npm login`. Note `npm login` needs an interactive terminal (passkey MFA).
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
# the one named "upsun"; everything else is a platform package. Names
# and versions for platform tarballs are cached in parallel arrays so
# the propagation wait does not re-open the tarball. Parallel arrays
# rather than associative arrays so this works on macOS's default Bash 3.2.
platform_tarballs=()
platform_names=()
platform_versions=()
wrapper_tarballs=()
for t in "${all_tarballs[@]}"; do
  pkg_json=$(tar -xzOf "$t" package/package.json)
  name=$(awk -F'"' '/"name":/ { print $4; exit }' <<<"$pkg_json")
  version=$(awk -F'"' '/"version":/ { print $4; exit }' <<<"$pkg_json")
  if [ "$name" = "upsun" ]; then
    wrapper_tarballs+=("$t")
  else
    platform_tarballs+=("$t")
    platform_names+=("$name")
    platform_versions+=("$version")
  fi
done

publish_one() {
  local tarball="$1"
  # Re-derive name+version from the tarball so this stays self-contained.
  local pkg_json name version
  pkg_json=$(tar -xzOf "$tarball" package/package.json)
  name=$(awk -F'"' '/"name":/ { print $4; exit }' <<<"$pkg_json")
  version=$(awk -F'"' '/"version":/ { print $4; exit }' <<<"$pkg_json")

  # Idempotent re-run: skip versions already on the registry. A publish can
  # fail partway (e.g. a transient Sigstore transparency-log error during
  # provenance signing) after some packages are already up; re-running then
  # finishes the rest instead of erroring on "cannot publish over existing".
  if npm view "${name}@${version}" version >/dev/null 2>&1; then
    echo "  ${name}@${version} already published; skipping"
    return 0
  fi

  local args=(publish "$tarball" --access public --tag "${NPM_TAG}")
  if [ "${DRY_RUN}" = "1" ]; then args+=(--dry-run); fi

  # Retry transient failures. npm provenance signing hits Sigstore's public
  # Rekor log, which intermittently returns errors; each attempt regenerates a
  # fresh signature, so a retry clears them.
  local attempt
  for attempt in 1 2 3; do
    echo "  npm ${args[*]} (attempt ${attempt})"
    if npm "${args[@]}"; then return 0; fi
    if [ "${attempt}" -lt 3 ]; then
      echo "  publish of ${name}@${version} failed; retrying in $((attempt * 10))s" >&2
      sleep $((attempt * 10))
    fi
  done
  echo "publish.sh: giving up on ${name}@${version} after 3 attempts" >&2
  return 1
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
  for i in "${!platform_tarballs[@]}"; do
    wait_visible "${platform_names[$i]}" "${platform_versions[$i]}"
  done
fi

echo "publish.sh: publishing wrapper"
for t in "${wrapper_tarballs[@]}"; do publish_one "$t"; done

echo "publish.sh: done"
