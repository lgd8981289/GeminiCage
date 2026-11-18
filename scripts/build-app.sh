#!/bin/bash
# Build only this fork. The public Cage app is a read-only kit source.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ $# != 0 ]]; then echo 'Usage: scripts/build-app.sh' >&2; exit 2; fi
base_app=${GEMINI_CAGE_BASE_APP:-/Applications/Cage.app}
kit="$base_app/Contents"
if [[ ! -f "$kit/Info.plist" ]]; then
    echo "Set GEMINI_CAGE_BASE_APP to the Cage.app from the upstream v0.1.8 macOS arm64 disk image." >&2
    exit 1
fi
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CageVersion' "$kit/Info.plist")" = "$(cat VERSION)" ]]
codesign --verify --deep --strict "$kit/.."
mkdir -p _output
npm --prefix ui ci --no-audit --no-fund
npm --prefix ui run build
make -j2 assets tailnet-assets
bash gemini/build-core.sh
bash gemini/package.sh
