#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
seed=/mnt/gemini
hostnamectl set-hostname gemini-cage
systemctl disable --now ssh.service >/dev/null 2>&1 || true
log() { echo "GEMINI-CAGE $*" | tee /dev/hvc0; }
trap 'log "install failed at line $LINENO"' ERR
mkdir -p "$seed"
mountpoint -q "$seed" || mount -o ro /dev/disk/by-label/CIDATA "$seed"
install -m 755 "$seed/relay.py" /usr/local/bin/gemini-relay
cat >/etc/systemd/system/gemini-relay.service <<'UNIT'
[Unit]
Description=Gemini Cage virtio-serial proxy
[Service]
ExecStart=/usr/bin/python3 /usr/local/bin/gemini-relay
ProtectSystem=strict
PrivateTmp=yes
Restart=always
RestartSec=2
StandardError=journal+console
NoNewPrivileges=yes
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable gemini-relay
systemctl restart gemini-relay
for attempt in $(seq 1 30); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1',3128),1).close()" 2>/dev/null; then break; fi
  sleep 1
done
proxy=http://127.0.0.1:3128
export http_proxy="$proxy" https_proxy="$proxy" HTTP_PROXY="$proxy" HTTPS_PROXY="$proxy" NO_PROXY=localhost,127.0.0.1,::1 no_proxy=localhost,127.0.0.1,::1
cat >/etc/apt/apt.conf.d/85gemini <<APT
Acquire::https::Proxy "$proxy";
Acquire::Retries "3";
APT
cat >/etc/environment <<ENV
http_proxy=$proxy
https_proxy=$proxy
HTTP_PROXY=$proxy
HTTPS_PROXY=$proxy
no_proxy=localhost,127.0.0.1,::1
NO_PROXY=localhost,127.0.0.1,::1
ENV
# All mirror requests must use TLS, including the security archive.
for list in /etc/apt/sources.list /etc/apt/sources.list.d/*.sources; do
  if [ -f "$list" ]; then sed -i 's|http://|https://|g' "$list"; fi
done
if ! id gemini >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash --groups audio,video,render gemini
fi
# Keep the mirror choice and TLS host deterministic.
rm -f /etc/apt/sources.list.d/debian.sources
cat >/etc/apt/sources.list <<'SOURCES'
deb https://deb.debian.org/debian trixie main
deb https://deb.debian.org/debian trixie-updates main
deb https://deb.debian.org/debian-security trixie-security main
SOURCES
log 'installing guest desktop'
apt-get -o APT::Update::Error-Mode=any update
apt-get install -y --no-install-recommends libpam-systemd xserver-xorg-core xserver-xorg-input-libinput xinit openbox dbus-x11 xterm xdg-utils xdg-user-dirs firefox-esr gnome-keyring libsecret-1-0 libnss3 libasound2t64 libgbm1 libgtk-3-0 libnotify4 libxss1 libdrm2 libgl1-mesa-dri fonts-dejavu-core fonts-noto-cjk ca-certificates curl x11-xserver-utils xdotool x11-utils
mkdir -p /opt/antigravity
tar -xzf "$seed/app.tgz" --strip-components=1 -C /opt/antigravity
chown root:root /opt/antigravity/chrome-sandbox
chmod 4755 /opt/antigravity/chrome-sandbox
cat >/usr/local/bin/gemini-browser <<'BROWSER'
#!/bin/sh
exec /usr/bin/firefox-esr --profile "$HOME/.mozilla/gemini-cage" "$@"
BROWSER
cat >/usr/local/bin/gemini-antigravity <<'APP'
#!/bin/sh
exec /opt/antigravity/antigravity --disable-gpu --disable-quic --proxy-server=http://127.0.0.1:3128 --password-store=gnome-libsecret "$@"
APP
chmod 755 /usr/local/bin/gemini-{browser,antigravity}
cat >/usr/share/applications/gemini-browser.desktop <<'BROWSER'
[Desktop Entry]
Name=Firefox ESR
Type=Application
Exec=/usr/local/bin/gemini-browser %U
MimeType=text/html;x-scheme-handler/http;x-scheme-handler/https;
BROWSER
cat >/usr/share/applications/gemini-antigravity.desktop <<'APP'
[Desktop Entry]
Name=Antigravity
Type=Application
Exec=/usr/local/bin/gemini-antigravity %U
MimeType=x-scheme-handler/antigravity;
APP
me=/home/gemini
install -d -o gemini -g gemini "$me/.config" "$me/Projects"
install -d -m 700 -o gemini -g gemini "$me/.mozilla" "$me/.mozilla/gemini-cage"
cat >"$me/.mozilla/gemini-cage/user.js" <<'PREFS'
user_pref("network.proxy.type", 1);
user_pref("network.proxy.http", "127.0.0.1");
user_pref("network.proxy.http_port", 3128);
user_pref("network.proxy.ssl", "127.0.0.1");
user_pref("network.proxy.ssl_port", 3128);
user_pref("network.proxy.no_proxies_on", "localhost, 127.0.0.1, ::1");
user_pref("network.http.http3.enable", false);
user_pref("browser.aboutwelcome.enabled", false);
user_pref("startup.homepage_welcome_url", "");
user_pref("startup.homepage_welcome_url.additional", "");
user_pref("datareporting.policy.firstRunURL", "");
PREFS
chown gemini:gemini "$me/.mozilla/gemini-cage/user.js"
cat >"$me/.config/mimeapps.list" <<'MIME'
[Default Applications]
text/html=gemini-browser.desktop
x-scheme-handler/http=gemini-browser.desktop
x-scheme-handler/https=gemini-browser.desktop
x-scheme-handler/antigravity=gemini-antigravity.desktop
MIME
cat >"$me/.bash_profile" <<'PROFILE'
if [ "$(tty)" = /dev/tty1 ]; then exec startx -- :0 vt1 -keeptty -nolisten tcp; fi
[ ! -f ~/.bashrc ] || . ~/.bashrc
PROFILE
cat >"$me/.xinitrc" <<'SESSION'
#!/bin/sh
exec dbus-run-session /usr/local/bin/gemini-session
SESSION
cat >/usr/local/bin/gemini-session <<'SESSION'
#!/bin/sh
export BROWSER=/usr/local/bin/gemini-browser
export XDG_CURRENT_DESKTOP=Openbox
# VM autologin has no PAM password. The keyring lives on its private disk.
eval "$(printf '\n' | gnome-keyring-daemon --unlock --components=secrets)"
openbox &
xset s off
xset -dpms
cd "$HOME/Projects"
while true; do
  /usr/local/bin/gemini-antigravity >>"$HOME/.cache/antigravity-session.log" 2>&1
  sleep 2
done
SESSION
chmod 755 /usr/local/bin/gemini-session "$me/.xinitrc"
install -d -o gemini -g gemini "$me/.cache"
chown gemini:gemini "$me/.config/mimeapps.list" "$me/.bash_profile" "$me/.xinitrc"
mkdir -p /etc/systemd/system/getty@tty1.service.d
cat >/etc/systemd/system/getty@tty1.service.d/login.conf <<'UNIT'
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin gemini --noclear %I linux
UNIT
systemctl daemon-reload
systemctl enable getty@tty1
systemctl restart getty@tty1
log "GUI started"
# Boot diagnostics report counts, response codes and the public egress only.
log "interfaces $(ls /sys/class/net | xargs)"
log "google status $(curl --max-time 20 -sSL -o /dev/null -w '%{http_code}' https://accounts.google.com/)"
log "exit $(curl -fsS --max-time 20 https://www.cloudflare.com/cdn-cgi/trace | sed -n 's/^ip=//p')"
log "direct $(curl --noproxy '*' --max-time 3 -sS https://1.1.1.1 -o /dev/null 2>/dev/null && echo reachable || echo blocked)"
log "host-target $(curl --noproxy '' --max-time 4 -sS -o /dev/null -w '%{http_connect}' https://192.168.1.1 2>/dev/null || :)"
for sec in $(seq 1 60); do
  if sudo -u gemini env HOME="$me" DISPLAY=:0 XAUTHORITY="$me/.Xauthority" xdotool search --onlyvisible --class 'antigravity' >/dev/null 2>&1; then
    log "Antigravity window visible"
    touch /var/lib/gemini-ready
    exit 0
  fi
  sleep 1
done
tail -12 "$me/.cache/antigravity-session.log" >/dev/hvc0 || :
tail -12 "$me/.local/share/xorg/Xorg.0.log" >/dev/hvc0 || :
log 'desktop window check incomplete'
exit 1
