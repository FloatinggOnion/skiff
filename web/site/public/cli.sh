#!/bin/sh
# Skiff CLI installer — puts the `skiff` command on your machine (macOS or Linux):
#
#   curl -fsSL https://useskiff.xyz/cli | sh
#
# Prefer Go (any platform)?  go install github.com/nuelScript/skiff@latest
#
# This installs the CLI you use from your laptop. To stand up the Skiff platform
# on a server, use https://useskiff.xyz/install instead.
set -eu

REPO="nuelScript/skiff"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  linux | darwin) ;;
  *) echo "error: unsupported OS '$os' — try: go install github.com/$REPO@latest" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) echo "error: unsupported architecture '$arch'" >&2; exit 1 ;;
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

asset="skiff-$os-$arch"
echo "→ downloading skiff ($os/$arch)"
tmp="$(mktemp)"
fetch_verified "$asset" "$tmp" \
  || { echo "error: download or verification failed — is a release published yet?" >&2; rm -f "$tmp"; exit 1; }
chmod +x "$tmp"

# Install into ~/.local/bin by default — no sudo, and no clash with a Homebrew
# skiff (which lives in /usr/local/bin on Intel or /opt/homebrew/bin on Apple
# Silicon). Override the location with SKIFF_INSTALL_DIR if you'd rather.
dest="${SKIFF_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dest"
mv "$tmp" "$dest/skiff"

echo "✓ installed $("$dest/skiff" version 2>/dev/null || echo skiff) to $dest/skiff"

# If a different skiff already shadows this one (e.g. one from Homebrew), say so.
existing="$(command -v skiff 2>/dev/null || true)"
if [ -n "$existing" ] && [ "$existing" != "$dest/skiff" ]; then
  echo "  note: another skiff on your PATH ($existing) may take precedence." >&2
fi

case ":$PATH:" in
  *":$dest:"*) echo "  run: skiff --help" ;;
  *) echo "  add it to your PATH:  export PATH=\"$dest:\$PATH\"" ;;
esac
