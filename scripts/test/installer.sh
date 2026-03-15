#!/bin/sh
# Test installer.sh across Linux distros using Docker.
# Requires Docker to be installed and running.

set -eu

cd "$(dirname "$0")/../.."

pass=0
fail=0
errors=""

run_test() {
    image="$1"
    method="$2"
    prereqs="$3"

    label="${image} (${method})"
    printf "Testing %-35s " "$label"

    if docker run --rm \
        -v "$(pwd)/installer.sh:/installer.sh:ro" \
        -e INSTALL_METHOD="$method" \
        "$image" \
        sh -c "${prereqs} sh /installer.sh && upsun --version" \
        >/dev/null 2>&1; then
        printf "PASS\n"
        pass=$((pass + 1))
    else
        printf "FAIL\n"
        fail=$((fail + 1))
        errors="${errors}  ${label}\n"
    fi
}

echo "installer.sh test suite"
echo "======================="
echo ""

run_test "debian:bookworm" "apt" \
    "apt-get update && apt-get install -y curl ca-certificates && "

run_test "ubuntu:24.04" "apt" \
    "apt-get update && apt-get install -y curl ca-certificates && "

run_test "fedora:41" "yum" \
    "yum install -y curl && "

run_test "alpine:3.21" "apk" \
    "apk add --no-cache curl ca-certificates && "

run_test "debian:bookworm" "raw" \
    "apt-get update && apt-get install -y curl ca-certificates gzip && "

echo ""
echo "Results: ${pass} passed, ${fail} failed"

if [ "$fail" -gt 0 ]; then
    printf "\nFailed:\n%b" "$errors"
    exit 1
fi
