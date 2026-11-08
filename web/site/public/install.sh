#!/bin/sh
# Skiff installer — stands up the control plane (dashboard + edge router) on
# this server. Run on a fresh Ubuntu/Debian box as root:
#
#   curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com
#
# It installs Docker, the skiff binary, and systemd units for the router and
# the control panel, then prints your dashboard URL and first-login key.
# Re-running it updates the binary in place (your data and login key are kept).
#
# On a Mac, run it as your own user instead (no sudo). Docker must already be
# installed and running, and the router and panel run as LaunchAgents in your
# login session. `--uninstall` removes them again.
set -eu

REPO="nuelScript/skiff"
DOMAIN=""
SITE_APP="www"
UNINSTALL=""

while [ $# -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="$2"; shift 2 ;;
    --site-app) SITE_APP="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done
[ -n "$DOMAIN" ] || DOMAIN="${SKIFF_DOMAIN:-}"

fail() { echo "error: $1" >&2; exit 1; }

os="$(uname -s)"
[ -z "$UNINSTALL" ] || [ "$os" = "Darwin" ] || fail "--uninstall is only supported on macOS"
if [ "$os" = "Darwin" ]; then
  [ "$(id -u)" != "0" ] || fail "run as your own user, not root: on a Mac Skiff runs in your login session"
else
  [ "$(id -u)" = "0" ] || fail "run as root (try: sudo sh)"
fi
[ -n "$DOMAIN" ] || [ -n "$UNINSTALL" ] || fail "a domain is required: curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com"

arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "unsupported architecture: $arch" ;;
esac
# A terminal running under Rosetta reports x86_64 on an Apple Silicon Mac; the native binary is the one to run.
if [ "$os" = "Darwin" ] && [ "$arch" = "amd64" ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = "1" ]; then
  arch="arm64"
fi

# Download the latest release's binary and check it against the release's SHA256SUMS.
# Releases before 0.5.4 were published without one.
fetch_verified() {
  asset="$1"; out="$2"
  tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" || return 1
  tag="${tag##*/}"
  base="https://github.com/$REPO/releases/download/$tag"
  curl -fsSL "$base/$asset" -o "$out" || return 1
  sums="$(mktemp)"
  if curl -fsSL "$base/SHA256SUMS" -o "$sums" 2>/dev/null; then
    want="$(awk -v a="$asset" '$2 == a { print $1 }' "$sums")"
    rm -f "$sums"
    [ -n "$want" ] || { echo "error: SHA256SUMS for $tag has no entry for $asset" >&2; return 1; }
    if command -v sha256sum >/dev/null 2>&1; then
      got="$(sha256sum "$out" | cut -d' ' -f1)"
    elif command -v shasum >/dev/null 2>&1; then
      got="$(shasum -a 256 "$out" | cut -d' ' -f1)"
    else
      echo "error: need sha256sum or shasum to verify the download" >&2; return 1
    fi
    [ "$got" = "$want" ] || { echo "error: checksum mismatch for $asset ($tag): expected $want, got $got" >&2; return 1; }
    echo "✓ checksum verified"
  else
    rm -f "$sums"
    case "$tag" in
      v0.[0-4].* | v0.5.[0-3]) echo "  note: $tag predates checksums, so the download isn't verified" >&2 ;;
      *) echo "error: $tag has no SHA256SUMS, so the download can't be verified" >&2; return 1 ;;
    esac
  fi
}

# macOS: the same control plane, run as LaunchAgents in the logged-in user's session (no root, no systemd). Everything lives under ~/.skiff.
if [ "$os" = "Darwin" ]; then
  uid="$(id -u)"
  skiff_dir="$HOME/.skiff"
  bin="$skiff_dir/bin/skiff"
  agents="$HOME/Library/LaunchAgents"
  router_label="xyz.useskiff.router"
  panel_label="xyz.useskiff.panel.7070"

  agent_loaded() { launchctl print "gui/$uid/$1" >/dev/null 2>&1; }
  agent_unload() {
    agent_loaded "$1" || return 0
    launchctl bootout "gui/$uid/$1" >/dev/null 2>&1 || true
    n=0
    while agent_loaded "$1" && [ "$n" -lt 10 ]; do sleep 1; n=$((n + 1)); done
  }
  # Paths can hold & or < (a home directory is whatever the user called it), so anything that isn't fixed text is escaped for the plist.
  xml() { printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }

  if [ -n "$UNINSTALL" ]; then
    for label in "$router_label" "$panel_label"; do
      agent_unload "$label"
      rm -f "$agents/$label.plist"
    done
    rm -f "$bin" "$bin.prev"
    echo "✓ Skiff's services and binary are removed. Your apps keep running in Docker."
    echo "  Your data stays in $skiff_dir — delete it too with: rm -rf $skiff_dir"
    exit 0
  fi

  case "$DOMAIN" in *[!A-Za-z0-9.,-]*) fail "that doesn't look like a domain: $DOMAIN" ;; esac
  case "$SITE_APP" in *[!A-Za-z0-9_-]*) fail "that doesn't look like an app name: $SITE_APP" ;; esac

  echo "→ Skiff — installing the control plane for $DOMAIN (macOS)"

  xcode-select -p >/dev/null 2>&1 || fail "Skiff needs git, which comes with the Xcode command line tools: run xcode-select --install, then run this again"
  command -v docker >/dev/null 2>&1 || fail "Docker isn't installed. Install Docker Desktop (brew install --cask docker), OrbStack or Colima, start it, then run this again"
  docker info >/dev/null 2>&1 || fail "Docker is installed but isn't running. Start it, wait until it's ready, then run this again"
  echo "✓ Docker is running"
  launchctl print "gui/$uid" >/dev/null 2>&1 || fail "$(id -un) has no login session. Log in to this Mac's desktop (or turn on automatic login), then run this again"

  echo "→ downloading skiff ($arch)"
  mkdir -p "$skiff_dir/bin" "$skiff_dir/certs" "$skiff_dir/logs" "$agents"
  tmp="$(mktemp)"
  fetch_verified "skiff-darwin-$arch" "$tmp" \
    || { rm -f "$tmp"; fail "could not download and verify the skiff binary — is a release published yet?"; }
  chmod 0755 "$tmp"
  # Releases before macOS support put certificates in /var/lib, which a regular user can't write to.
  "$tmp" router --help 2>&1 | grep -q -- '--certs-dir' \
    || { rm -f "$tmp"; fail "the latest release can't run the server on macOS yet — a newer release will"; }
  install -m 0755 "$tmp" "$bin.new"
  rm -f "$tmp"
  [ ! -f "$bin" ] || cp "$bin" "$bin.prev"
  mv -f "$bin.new" "$bin"
  echo "✓ installed $("$bin" version 2>/dev/null || echo skiff)"

  [ -f "$skiff_dir/panel.addr" ] || echo "127.0.0.1:7070" >"$skiff_dir/panel.addr"

  if [ -f "$skiff_dir/panel.env" ]; then
    echo "✓ keeping the existing setup key"
    SETUP_KEY="$(grep '^SKIFF_PANEL_PASSWORD=' "$skiff_dir/panel.env" | cut -d= -f2)"
  else
    SETUP_KEY="$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | cut -c1-24)"
    printf 'SKIFF_PANEL_PASSWORD=%s\n' "$SETUP_KEY" >"$skiff_dir/panel.env"
    chmod 600 "$skiff_dir/panel.env"
  fi
  if [ ! -f "$skiff_dir/skiff.env" ]; then
    echo '# SKIFF_SMTP_HOST= SKIFF_SMTP_PORT= SKIFF_SMTP_USER= SKIFF_SMTP_PASS= SKIFF_SMTP_FROM=' >"$skiff_dir/skiff.env"
    chmod 600 "$skiff_dir/skiff.env"
  fi

  # launchd starts agents with a bare PATH, so the one Docker was found on is passed along.
  docker_dir="$(dirname "$(command -v docker)")"
  agent_path="$docker_dir:$HOME/.docker/bin:/opt/homebrew/bin:/usr/local/bin:/Applications/Docker.app/Contents/Resources/bin:/usr/bin:/bin:/usr/sbin:/sbin"

  # launchd has no EnvironmentFile, so each agent sources the same env files the systemd units read, and waits for Docker, which starts at login.
  wait_docker='n=0; while ! docker info >/dev/null 2>&1 && [ "$n" -lt 60 ]; do n=$((n + 1)); sleep 2; done'
  router_cmd="$wait_docker"'; set -a; [ -f "$HOME/.skiff/skiff.env" ] && . "$HOME/.skiff/skiff.env"; set +a; exec "$SKIFF_BIN" router --domain '"$DOMAIN"' --panel 127.0.0.1:7070 --site-app '"$SITE_APP"
  panel_cmd="$wait_docker"'; set -a; [ -f "$HOME/.skiff/panel.env" ] && . "$HOME/.skiff/panel.env"; [ -f "$HOME/.skiff/skiff.env" ] && . "$HOME/.skiff/skiff.env"; set +a; exec "$SKIFF_BIN" panel --addr 127.0.0.1:7070 --domain '"$DOMAIN"

  write_agent() {
    cat >"$agents/$1.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$1</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-c</string>
    <string>$(xml "$2")</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>$(xml "$agent_path")</string>
    <key>SKIFF_BIN</key>
    <string>$(xml "$bin")</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>2</integer>
  <key>StandardOutPath</key>
  <string>$(xml "$skiff_dir/logs/$3.log")</string>
  <key>StandardErrorPath</key>
  <string>$(xml "$skiff_dir/logs/$3.log")</string>
</dict>
</plist>
EOF
  }
  write_agent "$router_label" "$router_cmd" router
  write_agent "$panel_label" "$panel_cmd" panel

  for label in "$router_label" "$panel_label"; do
    agent_unload "$label"
    launchctl enable "gui/$uid/$label" >/dev/null 2>&1 || true
    launchctl bootstrap "gui/$uid" "$agents/$label.plist" || fail "launchd wouldn't load $label — see: launchctl print gui/$uid/$label"
  done

  echo "→ waiting for the panel"
  n=0
  until curl -fsS -o /dev/null "http://127.0.0.1:7070/api/me" 2>/dev/null; do
    n=$((n + 1))
    if [ "$n" -ge 45 ]; then
      tail -n 15 "$skiff_dir/logs/panel.log" 2>/dev/null >&2 || true
      fail "the panel didn't come up — the log is $skiff_dir/logs/panel.log"
    fi
    sleep 2
  done
  echo "✓ the panel is up"

  ip="$(curl -fsSL https://api.ipify.org 2>/dev/null || echo YOUR_PUBLIC_IP)"

  cat <<EOF

  ✓ Skiff is running.

  1. Point DNS at this Mac's public address ($ip):
       A   *.$DOMAIN     $ip
       A   $DOMAIN       $ip     (optional — for the apex/marketing site)

     MX  $DOMAIN       10 mx.$DOMAIN  (optional — for email forwarding)

  2. Forward ports 80 and 443 on your router to this Mac. (Behind home
     broadband that is how the internet reaches it.)

  3. Open your dashboard once DNS resolves + the cert issues:
       https://dash.$DOMAIN

  4. First login — you'll be asked for this setup key:
       $SETUP_KEY

  Keep this Mac awake (System Settings → Energy) and logged in: Skiff and
  Docker run in your login session, so after a restart log in again, or turn
  on automatic login.

  Logs:   $skiff_dir/logs
  Docs:   https://docs.useskiff.xyz/self-hosting
EOF
  exit 0
fi

echo "→ Skiff — installing the control plane for $DOMAIN"

# 1. Docker
if command -v docker >/dev/null 2>&1; then
  echo "✓ Docker already installed"
else
  echo "→ installing Docker"
  curl -fsSL https://get.docker.com | sh >/dev/null
  echo "✓ Docker installed"
fi

# 2. skiff binary
echo "→ downloading skiff ($arch)"
tmp="$(mktemp)"
fetch_verified "skiff-linux-$arch" "$tmp" \
  || { rm -f "$tmp"; fail "could not download and verify the skiff binary — is a release published yet?"; }
install -m 0755 "$tmp" /usr/local/bin/skiff
rm -f "$tmp"
echo "✓ installed $(/usr/local/bin/skiff version 2>/dev/null || echo skiff)"

# 3. Directories + router upstream pointer
mkdir -p /root/.skiff /var/lib/skiff/certs /etc/skiff
[ -f /root/.skiff/panel.addr ] || echo "127.0.0.1:7070" >/root/.skiff/panel.addr

# 4. First-login key (kept across re-runs so updates don't lock you out)
if [ -f /etc/skiff/panel.env ]; then
  echo "✓ keeping the existing setup key"
  SETUP_KEY="$(grep '^SKIFF_PANEL_PASSWORD=' /etc/skiff/panel.env | cut -d= -f2)"
else
  SETUP_KEY="$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | cut -c1-24)"
  printf 'SKIFF_PANEL_PASSWORD=%s\n' "$SETUP_KEY" >/etc/skiff/panel.env
  chmod 600 /etc/skiff/panel.env
fi

# Shared settings for both services (SMTP relay for alerts + email forwarding).
if [ ! -f /etc/skiff/skiff.env ]; then
  echo '# SKIFF_SMTP_HOST= SKIFF_SMTP_PORT= SKIFF_SMTP_USER= SKIFF_SMTP_PASS= SKIFF_SMTP_FROM=' >/etc/skiff/skiff.env
  chmod 600 /etc/skiff/skiff.env
fi

# 5. systemd units — the edge router (:80/:443, auto-HTTPS) and a blue-green panel
cat >/etc/systemd/system/skiff-router.service <<EOF
[Unit]
Description=Skiff edge router
After=docker.service network-online.target
Wants=network-online.target
[Service]
EnvironmentFile=-/etc/skiff/skiff.env
Environment=HOME=/root
ExecStart=/usr/local/bin/skiff router --domain $DOMAIN --panel 127.0.0.1:7070 --site-app $SITE_APP
Restart=always
RestartSec=2
[Install]
WantedBy=multi-user.target
EOF

cat >/etc/systemd/system/skiff-panel@.service <<EOF
[Unit]
Description=Skiff control panel on :%i
After=docker.service
[Service]
EnvironmentFile=/etc/skiff/panel.env
EnvironmentFile=-/etc/skiff/skiff.env
Environment=HOME=/root
ExecStart=/usr/local/bin/skiff panel --addr 127.0.0.1:%i --domain $DOMAIN
Restart=always
RestartSec=2
KillMode=process
[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now skiff-router >/dev/null 2>&1 || systemctl restart skiff-router
systemctl enable skiff-panel@7070 >/dev/null 2>&1 || true
systemctl restart skiff-panel@7070

ip="$(curl -fsSL https://api.ipify.org 2>/dev/null || echo YOUR_SERVER_IP)"

cat <<EOF

  ✓ Skiff is running.

  1. Point DNS at this server ($ip):
       A   *.$DOMAIN     $ip
       A   $DOMAIN       $ip     (optional — for the apex/marketing site)

     MX  $DOMAIN       10 mx.$DOMAIN  (optional — for email forwarding)

  2. Open your dashboard once DNS resolves + the cert issues:
       https://dash.$DOMAIN

  3. First login — you'll be asked for this setup key:
       $SETUP_KEY

  Docs: https://docs.useskiff.xyz/self-hosting
EOF
