#!/usr/bin/env bash
set -euo pipefail
ref=$(git rev-parse --verify "${1:-HEAD}^{commit}")
out=${2:?provide output .tar.gz}
# git archive fails on unmatched pathspecs; use the tracked files from the pinned tree.
git ls-tree -r --name-only "$ref" -- cmd internal desktop ui mobile lib docs/guide README.md |
  awk '/^(README\.md|docs\/guide\/[^\/]+\.md)$/ || (/^(cmd|internal|desktop|ui|mobile|lib)\// && /\.(go|swift|cs|ts|tsx|js|sh|kt)$/ && !/[Tt]est/) {print}' > "$out.paths"
trap 'rm -f "$out.paths"' EXIT
files=()
while IFS= read -r file; do files+=("$file"); done < "$out.paths"
git archive --format=tar.gz --prefix=source/ -o "$out" "$ref" -- "${files[@]}"
printf '%s\n' "$ref" > "$out.rev"
