#!/usr/bin/env bash
# Installs jira-tui from the GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/boutdecode/jira-tui/main/install.sh | bash
#
# Environment:
#   JIRA_TUI_VERSION   version to install, e.g. v1.2.3 (default: latest)
#   INSTALL_DIR        target directory (default: /usr/local/bin if writable, else ~/.local/bin)
#   JIRA_TUI_BASE_URL  release download URL override (mirrors, tests)
set -euo pipefail

REPO="boutdecode/jira-tui"
BIN="jira-tui"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- platform ---
case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS: $(uname -s) (Linux and macOS only)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

# --- tools ---
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q "$1" -O "$2"; }
else
  die "curl or wget is required"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "sha256sum or shasum is required to verify the download"
fi
command -v tar >/dev/null 2>&1 || die "tar is required"

# --- what to download ---
version="${JIRA_TUI_VERSION:-latest}"
if [ -n "${JIRA_TUI_BASE_URL:-}" ]; then
  base="${JIRA_TUI_BASE_URL%/}"
elif [ "$version" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$version"
fi
asset="${BIN}_${os}_${arch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading $asset ($version)…"
fetch "$base/$asset" "$tmp/$asset" || die "download failed: $base/$asset"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "download failed: $base/checksums.txt"

# --- verify ---
expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")"
[ -n "$expected" ] || die "$asset is not listed in checksums.txt"
actual="$(sha256 "$tmp/$asset")"
[ "$expected" = "$actual" ] || die "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp" "$BIN" || die "$asset does not contain $BIN"

# --- install ---
if [ -n "${INSTALL_DIR:-}" ]; then
  dir="$INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir" || die "cannot create $dir"
if [ -w "$dir" ]; then
  install -m 0755 "$tmp/$BIN" "$dir/$BIN"
elif command -v sudo >/dev/null 2>&1; then
  say "$dir is not writable, using sudo…"
  sudo install -m 0755 "$tmp/$BIN" "$dir/$BIN"
else
  die "$dir is not writable (set INSTALL_DIR to another directory)"
fi

say "Installed: $dir/$BIN ($("$dir/$BIN" --version 2>/dev/null || echo unknown version))"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not in your PATH. Add it, e.g.:  export PATH=\"$dir:\$PATH\"" ;;
esac
say "Run '$BIN' to get started (first launch asks for your Jira credentials), or '$BIN --demo' to try it."
