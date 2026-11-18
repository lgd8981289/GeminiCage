#!/bin/bash
# Download application media on the host. The guest's external network stays closed.
set -euo pipefail
export PATH=/usr/bin:/bin:/usr/sbin:/sbin
[[ $# == 1 && $1 == /* ]] || exit 1
media=$1
src=$(cd "$(dirname "$0")" && pwd)
. "$src/macos-guest-arch.sh"
mkdir -p "$media"
export CAGE_HOST_DOWNLOAD_CACHE="$media/.downloads"
fetch() {
    if [[ -n ${CAGE_HOST_DOWNLOADER:-} ]]; then
        if [[ -n ${5:-} ]]; then
            "$CAGE_HOST_DOWNLOADER" host-download "$1" "$2" "$3" "${4:-}" "$CAGE_HOST_DOWNLOAD_CACHE" "$5"
        else
            "$CAGE_HOST_DOWNLOADER" host-download "$1" "$2" "$3" "${4:-}"
        fi
    else
        # Standalone source builds can supply curl proxy environment variables.
        curl --disable -fsSL --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 15 --speed-time 60 --speed-limit 1 -H "Accept: ${4:-*/*}" "$1" -o "$2"
    fi
}
desktop_media() {
    local version=2.33.0
    local url="https://storage.googleapis.com/antigravity-public/antigravity-hub/2.33.0-4731201358004224/darwin-arm/Antigravity.dmg"
    [[ $CAGE_MACOS_NATIVE_ARCH == arm64 ]] || { echo "Gemini Cage 支持 Apple Silicon 客机" >&2; return 1; }
    download antigravity.dmg "$url" 01267a5a4c4b615110c08eb14c61a3505ee8708e7a64f85d2880c867c31baf17
    bash "$src/claude-desktop-macos.sh" verify-media "$media/antigravity.dmg" "$version"
}
download() {
    local name=$1 url=$2 expected=$3 api=${4:-}
    local target="$media/$name" temporary
    [[ ! -L $target && ( ! -e $target || -f $target ) ]] || exit 1
    if [[ -f $target && ! -L $target ]]; then
        if [[ -z $expected && ${CAGE_REFRESH_TOOLS:-} != 1 ]] || [[ -n $expected && $(shasum -a 256 "$target" | cut -d' ' -f1) == "$expected" ]]; then return; fi
    fi
    printf '下载 %s\n' "$name"
    temporary=$(mktemp "$media/.$name.XXXXXXXX")
    if ! fetch "$url" "$temporary" "$name" "" "$expected"; then
        if [[ -z $api ]] || ! fetch "$api" "$temporary" "$name" application/octet-stream "$expected"; then
            rm "$temporary"
            return 1
        fi
    fi
    if [[ -n $expected && $(shasum -a 256 "$temporary" | cut -d' ' -f1) != "$expected" ]]; then
        rm "$temporary"
        return 1
    fi
    mv "$temporary" "$target"
}
download editor.tgz "https://github.com/coder/code-server/releases/download/v4.145.0/code-server-4.145.0-macos-$CAGE_MACOS_RELEASE_ARCH.tar.gz" "$CAGE_MACOS_EDITOR_SHA256" "https://api.github.com/repos/coder/code-server/releases/assets/$CAGE_MACOS_EDITOR_ASSET"
download node.tgz "https://nodejs.org/dist/v24.22.2/node-v24.22.2-darwin-$CAGE_MACOS_NODE_ARCH.tar.gz" "$CAGE_MACOS_NODE_SHA256"
if [[ ${CAGE_REFRESH_TOOLS:-} != 1 ]]; then
    download macfuse.dmg https://github.com/macfuse/macfuse/releases/download/macfuse-5.5.0/macfuse-5.5.0.dmg 92c21ef2db4199bcf5bddc8e6efb57b23ecb9715f74ab38cba2c77ab226d3830
    if [[ ! -f $media/macfuse.pkg ]]; then bash "$src/macos-fuse-media.sh" "$media/macfuse.dmg" "$media/macfuse.pkg"; fi
    desktop_media
fi
download chrome.dmg https://dl.google.com/chrome/mac/universal/stable/GGRO/googlechrome.dmg ''
{
    fresh=$(mktemp -d "$media/.cli-repair.XXXXXXXX")
    if ! bash "$src/prepare-macos-cli-kit.sh" "$fresh/cli" "$media/cli"; then
        rmdir "$fresh"
        exit 1
    fi
    # Earlier builds cached a kit without Git. Preserve it until the full kit
    # is verified and the next install can resume after either rename.
    if [[ -d $media/cli ]]; then mv "$media/cli" "$fresh/previous"; fi
    mv "$fresh/cli" "$media/cli"
    rm -rf "$fresh"
}

# Cached archives may survive several application builds. Refresh first-party
# CLI source from this bundle without changing downloaded package bytes.
for name in install-macos-cli.sh macos-cli.py cli_channel.py cli_channel.resume.md claude_account_resume.md claude-account.py client_runtime.py; do
    if [[ -f $src/$name ]]; then
        [[ ! -L $media/cli/$name ]] || exit 1
        cp "$src/$name" "$media/cli/$name"
    fi
done
