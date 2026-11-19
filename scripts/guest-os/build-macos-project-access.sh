#!/bin/sh
# Build the scoped project-access application for the selected native guest.
set -eu
. "$(dirname "$0")/macos-guest-arch.sh"
[ "$#" = 1 ] || { echo "用法：$0 /path/to/CageProjectAccess.app" >&2; exit 2; }
bundle=$1
[ ! -e "$bundle" ] || { echo "请指定一个尚未使用的输出路径。" >&2; exit 1; }
mkdir -p "$bundle/Contents/MacOS"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleDevelopmentRegion</key><string>zh-Hans</string>
<key>CFBundleLocalizations</key><array><string>zh-Hans</string></array>
<key>CFBundleIdentifier</key><string>dev.cage.guest.project-access</string>
<key>CFBundleDisplayName</key><string>Cage Project Access</string>
<key>CFBundleName</key><string>Cage Project Access</string>
<key>CFBundleExecutable</key><string>CageProjectAccess</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>LSUIElement</key><true/>
<key>NSNetworkVolumesUsageDescription</key><string>访问你在 Gemini Cage 中选择的项目文件夹。</string>
</dict></plist>
PLIST
xcrun clang -arch "$CAGE_MACOS_NATIVE_ARCH" -mmacosx-version-min=14.0 -fobjc-arc -O2 -framework AppKit \
  "$(dirname "$0")/macos-project-access.m" -o "$bundle/Contents/MacOS/CageProjectAccess"
codesign --force --sign "${CAGE_SIGNING_IDENTITY:--}" "$bundle"
