#!/usr/bin/env bash
set -euo pipefail
test "$#" -eq 0
IFS= read -r -n 8193 address
test "${#address}" -le 8192
if [[ "$address" =~ [[:cntrl:]] ]]; then exit 2; fi
case "$address" in antigravity://*) ;; *) exit 2;; esac
systemctl is-active --quiet cage-claude-desktop.service || exit 1
export DISPLAY=:78 XAUTHORITY=/run/cage-desktop/Xauthority XDG_RUNTIME_DIR=/run/cage-desktop
export DBUS_SESSION_BUS_ADDRESS="$(cat /run/cage-desktop/bus-address)"
exec /opt/google-antigravity/current/antigravity "$address"
