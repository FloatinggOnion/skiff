#!/bin/sh
# Skiff installer — stands up the control plane (dashboard + edge router) on
# this server. Run on a fresh Ubuntu/Debian box as root:
#
#   curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com
#
# It installs Docker, the skiff binary, and systemd units for the router and
# the control panel, then prints your dashboard URL and first-login key.
# Re-running it updates the binary in place (your data and login key are kept).
set -eu

REPO="nuelScript/skiff"
DOMAIN=""
SITE_APP="www"

while [ $# -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="$2"; shift 2 ;;
    --site-app) SITE_APP="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done
[ -n "$DOMAIN" ] || DOMAIN="${SKIFF_DOMAIN:-}"

fail() { echo "error: $1" >&2; exit 1; }

[ "$(id -u)" = "0" ] || fail "run as root (try: sudo sh)"
[ -n "$DOMAIN" ] || fail "a domain is required: curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com"

arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "unsupported architecture: $arch" ;;
esac

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

  Docs: https://useskiff.xyz/docs/self-hosting
EOF
