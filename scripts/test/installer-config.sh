#!/bin/sh
# Test installer.sh with CONFIG_URL (a vendor installer) in Docker containers.
# Requires Docker, Python 3, and a binary built by "make single".
# The raw tests install that binary; the apt and apk tests install the latest package.

set -eu

cd "$(dirname "$0")/../.."

: "${BINARY:=$(find dist -type f -name upsun -perm -u+x | head -n 1)}"
: "${PORT:=8765}"

if [ -z "$BINARY" ] || [ ! -x "$BINARY" ]; then
    echo "No binary found: run \"make single\" or set BINARY" >&2
    exit 1
fi

version="9.9.9"
serve_dir=$(mktemp -d)
trap 'kill "$server_pid" 2>/dev/null || true; rm -rf "$serve_dir"' EXIT

# Serve a release tarball and the test config, in the layout installer.sh expects.
mkdir -p "$serve_dir/v$version" "$serve_dir/build"
cp "$BINARY" "$serve_dir/build/upsun"
tar -C "$serve_dir/build" -czf "$serve_dir/v$version/upsun_${version}_linux_amd64.tar.gz" upsun
cp internal/config/alt/test-config.yaml "$serve_dir/config.yaml"
python3 -m http.server -b 127.0.0.1 -d "$serve_dir" "$PORT" >/dev/null 2>&1 &
server_pid=$!
sleep 1

base_url="http://127.0.0.1:$PORT"
exe="platform-test"

pass=0
fail=0
errors=""

# Run a script in a container, with the installer at /installer.sh.
# Arguments: label, image, script
run_test() {
    echo ""
    echo "=== $1 ==="
    if docker run --rm --network host \
        -v "$(pwd)/installer.sh:/installer.sh:ro" \
        -e CONFIG_URL="$base_url/config.yaml" \
        -e URL="$base_url" -e VERSION="$version" \
        -e EXE="$exe" \
        "$2" sh -euc "$3"; then
        echo "--- PASS: $1 ---"
        pass=$((pass + 1))
    else
        echo "--- FAIL: $1 ---"
        fail=$((fail + 1))
        errors="${errors}  $1\n"
    fi
}

debian_prereqs="apt-get update -qq && apt-get install -y -qq curl ca-certificates gzip >/dev/null"
alpine_prereqs="apk add -q --no-cache curl ca-certificates"

# Checks the installer's output names the vendor command, and runs it.
# The output is in /tmp/out; $1 is the command expected in the output.
# shellcheck disable=SC2016 # Expanded in the container.
check_output='
check() {
    grep -F "To use the CLI, run: $1" /tmp/out
    "$1" --version
}
# Checks that a root install is shared with other users.
check_shared() {
    test -f "/etc/upsun/platform-alt/$1.yaml"
    adduser -D other 2>/dev/null || useradd -m other
    su other -c "$1 --version"
}'

run_test "debian (apt, root)" debian:bookworm "$debian_prereqs
$check_output
env -u URL -u VERSION CI= sh /installer.sh | tee /tmp/out
test \"\$(command -v \$EXE)\" = /usr/bin/\$EXE
check \$EXE
check_shared \$EXE"

run_test "alpine (apk, root)" alpine:3.21 "$alpine_prereqs
$check_output
env -u URL -u VERSION CI= sh /installer.sh | tee /tmp/out
test \"\$(command -v \$EXE)\" = /usr/bin/\$EXE
check \$EXE
check_shared \$EXE"

run_test "debian (raw, CI, root)" debian:bookworm "$debian_prereqs
$check_output
CI=1 sh /installer.sh | tee /tmp/out
test \"\$(command -v \$EXE)\" = /usr/bin/\$EXE
check \$EXE
check_shared \$EXE"

run_test "debian (raw, root, umask 077, symlinked INSTALL_DIR)" debian:bookworm "$debian_prereqs
$check_output
mkdir -p /opt/real && ln -s /opt/real /opt/bin
umask 077
INSTALL_DIR=/opt/bin sh /installer.sh | tee /tmp/out
test -x /opt/real/\$EXE
PATH=/opt/bin:\$PATH check_shared \$EXE"

run_test "debian (raw, root, existing shim on PATH)" debian:bookworm "$debian_prereqs
$check_output
printf '#!/bin/sh\necho old\n' > /usr/local/bin/\$EXE && chmod +x /usr/local/bin/\$EXE
sh /installer.sh | tee /tmp/out
test -x /usr/bin/\$EXE
grep -F \"/usr/local/bin/\$EXE\" /tmp/out"

run_test "debian (raw, non-root, no sudo)" debian:bookworm "$debian_prereqs
useradd -m -s /bin/sh user
su user -c 'sh /installer.sh' | tee /tmp/out
test -x /home/user/.local/bin/upsun
test -x /home/user/.local/bin/\$EXE
grep -F \"To use the CLI, run: /home/user/.local/bin/\$EXE\" /tmp/out
su user -c \"/home/user/.local/bin/\$EXE --version\""

run_test "debian (raw, non-root, sudo)" debian:bookworm "$debian_prereqs
apt-get install -y -qq sudo >/dev/null
useradd -m -s /bin/sh user
echo 'user ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/user
su user -c 'sh /installer.sh' | tee /tmp/out
grep -F 're-trying with sudo' /tmp/out
test -x /usr/bin/upsun
test -x /home/user/.platform-alt/bin/\$EXE
grep -F \"To use the CLI, run: /home/user/.platform-alt/bin/\$EXE\" /tmp/out
su user -c \"/home/user/.platform-alt/bin/\$EXE --version\""

echo ""
echo "======================="
echo "Results: ${pass} passed, ${fail} failed"

if [ "$fail" -gt 0 ]; then
    printf "\nFailed:\n%b" "$errors"
    exit 1
fi
