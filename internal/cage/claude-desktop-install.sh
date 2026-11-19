#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$HOME/.config/claude-cage"
exec 9>"$HOME/.config/claude-cage/desktop-install.lock"
flock -n 9 || { echo 'Antigravity 正在安装，请等待安装完成。' >&2; exit 1; }
if systemctl is-active --quiet cage-claude-desktop.service; then
  echo '请先保存工作并关闭 Antigravity，再更新或修复安装。' >&2
  exit 1
fi
case "$(dpkg --print-architecture)" in arm64|amd64) ;; *) echo '当前处理器架构不支持安装 Antigravity 桌面版。' >&2; exit 1;; esac
printf '正在准备安装依赖…\n'
audio_pkg=libasound2; gtk_pkg=libgtk-3-0
if apt-cache show libasound2t64 2>/dev/null | grep '^Package:' >/dev/null; then audio_pkg=libasound2t64; gtk_pkg=libgtk-3-0t64; fi
sudo env DEBIAN_FRONTEND=noninteractive APT_CONFIG="${CAGE_APT_CONFIG:-/dev/null}" apt-get -o DPkg::Lock::Timeout=60 update </dev/null
sudo env DEBIAN_FRONTEND=noninteractive APT_CONFIG="${CAGE_APT_CONFIG:-/dev/null}" apt-get -o DPkg::Lock::Timeout=60 install -y --no-install-recommends ca-certificates curl gnupg dbus-x11 xauth x11-utils x11-xserver-utils tigervnc-standalone-server tigervnc-tools openbox gnome-keyring gcr bubblewrap socat openssh-client libglib2.0-bin ibus ibus-gtk3 gir1.2-ibus-1.0 gir1.2-gtk-3.0 python3-gi python3-pil python3-xlib xsettingsd libnss3 libgbm1 libxss1 libnotify4 "$gtk_pkg" "$audio_pkg" libsecret-1-0 </dev/null
version=2.33.0
build=2.33.0-4731201358004224
case "$(dpkg --print-architecture)" in
  arm64) architecture=arm; sum=fb321bd9832403cf766e3e9719c2063cee1747a320925d3aecd4f0ce67ca6013 ;;
  amd64) architecture=x64; sum=a73387b229c6b1f5288860731133c5b26e153340eaed96d865032cd8a8535050 ;;
esac
temporary=$(mktemp -d)
destination=/opt/google-antigravity
stage=''
cleanup() {
  rm -rf "$temporary"
  if [[ -n "$stage" ]]; then sudo rm -rf "$stage"; fi
}
trap cleanup EXIT
url="https://storage.googleapis.com/antigravity-public/antigravity-hub/$build/linux-$architecture/Antigravity.tar.gz"
cage_curl_download "$temporary/app.tgz" --proto '=https' --proto-redir '=https' -fSL --connect-timeout 20 "$url" -o "$temporary/app.tgz"
printf '%s  %s\n' "$sum" "$temporary/app.tgz" | sha256sum -c -
sudo install -d -m 755 "$destination"
stage=$(sudo mktemp -d "$destination/.install.XXXXXXXX")
sudo tar -xzf "$temporary/app.tgz" -C "$stage" --strip-components=1 --no-same-owner
sudo test -x "$stage/antigravity"
sudo chown root:root "$stage/chrome-sandbox"
sudo chmod 4755 "$stage/chrome-sandbox"
printf '%s\n' "$version" | sudo tee "$stage/.gemini-cage-version" >/dev/null
sudo chmod 755 "$stage"
# Publish only after all files have passed verification. Keep the previous version on disk.
release=$(basename "$stage" | sed 's/^\.install\./release-/')
sudo mv "$stage" "$destination/$release"
sudo ln -s "$release" "$stage"
sudo mv -Tf "$stage" "$destination/current"
printf 'Antigravity %s 已安装。\n' "$version"
