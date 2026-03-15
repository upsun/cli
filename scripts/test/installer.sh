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

run_test() {
    image="$1"
    method="$2"
    prereqs="$3"

    label="${image} (${method})"
    echo ""
    echo "=== ${label} ==="

    version_flag=""
    if [ -n "$VERSION" ]; then
        version_flag="-e VERSION=$VERSION"
    fi

    if docker run --rm \
        -v "$(pwd)/installer.sh:/installer.sh:ro" \
        -e INSTALL_METHOD="$method" \
        $version_flag \
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

run_test "debian:bookworm" "apt" \
    "apt-get update -qq && apt-get install -y -qq curl ca-certificates && "

run_test "ubuntu:24.04" "apt" \
    "apt-get update -qq && apt-get install -y -qq curl ca-certificates && "

run_test "fedora:41" "yum" \
    "yum install -y -q curl && "

run_test "alpine:3.21" "apk" \
    "apk add -q --no-cache curl ca-certificates && "

run_test "debian:bookworm" "raw" \
    "apt-get update -qq && apt-get install -y -qq curl ca-certificates gzip && "

echo ""
echo "======================="
echo "Results: ${pass} passed, ${fail} failed"

if [ "$fail" -gt 0 ]; then
    printf "\nFailed:\n%b" "$errors"
    exit 1
fi
