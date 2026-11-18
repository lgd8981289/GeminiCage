#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
base_app=${GEMINI_CAGE_BASE_APP:-/Applications/Cage.app}
kit="$base_app/Contents"
if [[ ! -f "$kit/Info.plist" ]]; then
    echo "Set GEMINI_CAGE_BASE_APP to the Cage.app from the upstream v0.1.8 macOS arm64 disk image." >&2
    exit 1
fi
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CageVersion' "$kit/Info.plist")" = "$(cat VERSION)" ]]
codesign --verify --deep --strict "$kit/.."
mkdir -p _output
stage=$(mktemp -d "$PWD/_output/gemini-build.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
bundle="$stage/Gemini Cage.app"
contents="$bundle/Contents"
res="$contents/Resources"
mkdir -p "$res" "$contents/MacOS"
for component in mihomo guest-os licenses cloud-linux-amd64.tar cloud-linux-amd64.tar.sha256; do
    ditto "$kit/Resources/$component" "$res/$component"
done
# Rebuild the fork's guest window verifier from source. Other guest kit components match the pinned upstream release.
mv "$res/guest-os/Cage Guest Window.app" "$stage/upstream-window.app"
bash scripts/guest-os/build-macos-claude-window.sh "$res/guest-os/Cage Guest Window.app"
for script in claude-desktop-macos.sh prepare-macos-media.sh provision-macos-workbench.sh; do
    cp "scripts/guest-os/$script" "$res/guest-os/$script"
done
cp _output/bin/cage "$res/cage"
cp LICENSE "$res/LICENSE"
ditto gemini/previous "$res/previous"
swiftc -O -target arm64-apple-macosx14.0 -parse-as-library -framework Cocoa -framework WebKit -framework Security -framework LocalAuthentication desktop/{Cage,MacKeyboard,NativeInput,Updates,AccountDevice,MacGuestLogin}.swift -o "$contents/MacOS/GeminiCage"
swift desktop/Icon.swift logo.png "$stage/Gemini.iconset"
iconutil -c icns "$stage/Gemini.iconset" -o "$res/GeminiCage.icns"
cp gemini/Info.plist "$contents/Info.plist"
codesign -f -s - "$contents/MacOS/GeminiCage"
codesign -f -s - "$bundle"
codesign --verify --strict --deep "$bundle"
destination="$PWD/_output/Gemini Cage.app"
if [[ -e "$destination" ]]; then
    [[ ! -L "$destination" && "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$destination/Contents/Info.plist")" == local.sunday.gemini-cage* ]]
    mv "$destination" "$stage/previous.app"
fi
if ! mv "$bundle" "$destination"; then
    if [[ -d "$stage/previous.app" ]]; then mv "$stage/previous.app" "$destination"; fi
    exit 1
fi
