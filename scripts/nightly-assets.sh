#!/usr/bin/env bash
# Copies the archives of a GoReleaser snapshot to a directory, with "nightly" in place of the version in their names,
# so that their download URLs do not change. Writes checksums.txt, and commit.txt (the commit the snapshot is from).
# Usage: nightly-assets.sh <dist-dir> <output-dir>
set -euo pipefail

dist=$1
out=$2

version=$(jq -r .version "$dist/metadata.json")
commit=$(jq -r .commit "$dist/metadata.json")
mkdir -p "$out"
shopt -s nullglob
archives=("$dist"/*_"$version"_*.tar.gz "$dist"/*_"$version"_*.zip)
if [ ${#archives[@]} -eq 0 ]; then
    echo "No archives found for version $version in $dist" >&2
    exit 1
fi
for f in "${archives[@]}"; do
    name=$(basename "$f")
    cp "$f" "$out/${name/_${version}_/_nightly_}"
done
(cd "$out" && sha256sum -- *.tar.gz *.zip > checksums.txt)
echo "$commit" > "$out/commit.txt"

echo "Version: $version"
ls -l "$out"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "version=$version" >> "$GITHUB_OUTPUT"
fi
