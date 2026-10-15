#!/usr/bin/env bash
# Updates the pinned PHP build (in the Makefile and the checksums file) to the latest cli-php-builds release for the
# current PHP minor version, and the pinned CA bundle to the latest from curl.se.
# Writes a pull request title and body to the files given as arguments, if anything changed.
# Requires: gh (authenticated), jq, curl, sha256sum.
set -euo pipefail

title_file=${1:?Usage: $0 TITLE_FILE BODY_FILE}
body_file=${2:?Usage: $0 TITLE_FILE BODY_FILE}

repo=upsun/cli-php-builds
sums_file=internal/legacy/php_sha256sums.txt

makefile_var() {
  sed -n "s/^$1 = //p" Makefile
}

set_makefile_var() {
  sed -i.bak "s/^$1 = .*/$1 = $2/" Makefile && rm Makefile.bak
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

php_version=$(makefile_var PHP_VERSION)
php_build=$(makefile_var PHP_BUILD)
cacert_date=$(makefile_var CACERT_DATE)
old_tag="php-$php_version-$php_build"
minor=$(cut -d. -f1,2 <<< "$php_version")

new_tag=$(gh release list --repo "$repo" --exclude-drafts --exclude-pre-releases --limit 100 \
  --json tagName --jq '.[].tagName' \
  | grep -E "^php-${minor//./\\.}\.[0-9]+-[0-9]+$" | sort -V | tail -n1 || true)
if [[ -z "$new_tag" ]]; then
  echo "No release found for PHP $minor" >&2
  exit 1
fi

new_cacert_date=$(curl -fsSL https://curl.se/docs/caextract.html \
  | grep -oE 'cacert-[0-9]{4}-[0-9]{2}-[0-9]{2}\.pem' | sed 's/^cacert-//; s/\.pem$//' | sort | tail -n1)
if [[ -z "$new_cacert_date" ]]; then
  echo "No CA bundle found" >&2
  exit 1
fi

titles=()
body=""

if [[ "$new_tag" != "$old_tag" ]]; then
  if [[ "$(sort -V <<< "$old_tag"$'\n'"$new_tag" | tail -n1)" != "$new_tag" ]]; then
    echo "The latest release $new_tag is older than $old_tag" >&2
    exit 1
  fi
  new_version=$(sed -E 's/^php-(.*)-[0-9]+$/\1/' <<< "$new_tag")
  new_build=${new_tag##*-}

  gh release download "$new_tag" --repo "$repo" --dir "$tmp/new" --pattern SHA256SUMS.txt --pattern sources.json
  if ! gh release download "$old_tag" --repo "$repo" --dir "$tmp/old" --pattern sources.json; then
    mkdir -p "$tmp/old" && echo '{}' > "$tmp/old/sources.json"
  fi

  cp "$tmp/new/SHA256SUMS.txt" "$sums_file"
  set_makefile_var PHP_VERSION "$new_version"
  set_makefile_var PHP_BUILD "$new_build"

  if [[ "$new_version" == "$php_version" ]]; then
    titles+=("rebuild PHP $new_version (build $new_build)")
  else
    titles+=("upgrade PHP to $new_version (build $new_build)")
  fi

  # Lists changed source archives, grouped by platform.
  changes=$(jq -rn --slurpfile old "$tmp/old/sources.json" --slurpfile new "$tmp/new/sources.json" '
    $old[0] as $o | $new[0] as $n
    | [($o + $n | keys[]) as $p | (($o[$p] // {}) + ($n[$p] // {}) | keys[]) as $lib
      | {p: $p, lib: $lib, old: ($o[$p][$lib] // "-"), new: ($n[$p][$lib] // "-")}
      | select(.old != .new)]
    | group_by([.lib, .old, .new])
    | map("| \(.[0].lib) | \(.[0].old) | \(.[0].new) | \(map(.p) | join(", ")) |")
    | .[]')
  body+="Updates the embedded PHP binaries from [$old_tag](https://github.com/$repo/releases/tag/$old_tag)"
  body+=" to [$new_tag](https://github.com/$repo/releases/tag/$new_tag)."$'\n\n'
  if [[ -n "$changes" ]]; then
    body+="| Source | Old | New | Platforms |"$'\n'"|---|---|---|---|"$'\n'"$changes"$'\n\n'
  else
    body+="No source archives changed."$'\n\n'
  fi
fi

if [[ "$new_cacert_date" > "$cacert_date" ]]; then
  file="cacert-$new_cacert_date.pem"
  curl -fsSL "https://curl.se/ca/$file" -o "$tmp/$file"
  curl -fsSL "https://curl.se/ca/$file.sha256" -o "$tmp/$file.sha256"
  (cd "$tmp" && sha256sum -c "$file.sha256")
  set_makefile_var CACERT_DATE "$new_cacert_date"
  set_makefile_var CACERT_SHA256 "$(cut -d' ' -f1 "$tmp/$file.sha256")"
  titles+=("update the CA bundle to $new_cacert_date")
  body+="Updates the CA bundle (embedded for PHP on Windows) from $cacert_date to $new_cacert_date."
  body+=" See: https://curl.se/docs/caextract.html"$'\n\n'
fi

if [[ ${#titles[@]} -eq 0 ]]; then
  echo "Up to date: $old_tag, CA bundle $cacert_date"
  exit 0
fi

title="build: ${titles[0]}"
if [[ ${#titles[@]} -gt 1 ]]; then
  title+=" and ${titles[1]}"
fi
echo "$title" > "$title_file"
printf '%s' "$body" > "$body_file"
echo "$title"
