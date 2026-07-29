#!/bin/sh
# Test installer.sh across Linux distros using Docker.
# Requires Docker to be installed and running.
# macOS methods (homebrew, raw) are only tested in CI (GitHub Actions).

set -eu

cd "$(dirname "$0")/../.."

: "${VERSION:=}"

pass=0
fail=0
errors=""

# Run a test in a Docker container.
# Arguments: label, image, prereqs, [extra docker args...]
run_test() {
    label="$1"
    image="$2"
    prereqs="$3"
    shift 3

    echo ""
    echo "=== ${label} ==="

    if docker run --rm \
        -v "$(pwd)/installer.sh:/installer.sh:ro" \
        "$@" \
        "$image" \
        sh -c "${prereqs}sh /installer.sh && upsun --version"; then
        echo "--- PASS: ${label} ---"
        pass=$((pass + 1))
    else
        echo "--- FAIL: ${label} ---"
        fail=$((fail + 1))
        errors="${errors}  ${label}\n"
    fi
}

echo "installer.sh test suite"
echo "======================="

# Default flow tests: let the installer auto-detect the install method.

run_test "debian:bookworm (default)" "debian:bookworm" \
    "apt-get update -qq && apt-get install -y -qq curl ca-certificates && "

run_test "ubuntu:24.04 (default)" "ubuntu:24.04" \
    "apt-get update -qq && apt-get install -y -qq curl ca-certificates && "

run_test "fedora:41 (default)" "fedora:41" \
    "yum install -y -q curl && "

run_test "alpine:3.21 (default)" "alpine:3.21" \
    "apk add -q --no-cache curl ca-certificates && "

# Raw install test: setting VERSION triggers the raw method on Linux.
if [ -n "$VERSION" ]; then
    run_test "debian:bookworm (raw, VERSION=$VERSION)" "debian:bookworm" \
        "apt-get update -qq && apt-get install -y -qq curl ca-certificates gzip && " \
        -e "VERSION=${VERSION}"
else
    echo ""
    echo "Skipping raw install test (set VERSION to enable)"
fi

echo ""
echo "======================="
echo "Results: ${pass} passed, ${fail} failed"

if [ "$fail" -gt 0 ]; then
    printf "\nFailed:\n%b" "$errors"
    exit 1
fi
