#!/bin/sh
set -eu
cd "$(dirname "$0")"
app="../Gemini Cage.app/Contents"
mkdir -p "$app/MacOS" "$app/Resources"
clang -fobjc-arc -O2 -Wall -framework Foundation Main.m -o "$app/MacOS/GeminiCage"
cp Info.plist "$app/Info.plist"
cp guest/relay.py run.py power.py "$app/Resources/"
codesign --force --sign - "$app/.."
