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

# --- application menu entry ---
# The icon is embedded so that "curl | bash" needs no other file.
write_icon_svg() {
  cat > "$1" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#8f6dff"/>
      <stop offset="1" stop-color="#5a3bd6"/>
    </linearGradient>
  </defs>
  <rect width="256" height="256" rx="56" fill="url(#bg)"/>
  <rect x="28" y="28" width="200" height="200" rx="30" fill="#15121f"/>
  <g fill="#3a3358">
    <rect x="44" y="44" width="52" height="168" rx="12"/>
    <rect x="102" y="44" width="52" height="168" rx="12"/>
    <rect x="160" y="44" width="52" height="168" rx="12"/>
  </g>
  <rect x="52" y="56" width="36" height="30" rx="7" fill="#e6e1f0"/>
  <rect x="52" y="94" width="36" height="30" rx="7" fill="#e6e1f0"/>
  <rect x="52" y="132" width="36" height="30" rx="7" fill="#e6e1f0"/>
  <rect x="110" y="56" width="36" height="30" rx="7" fill="#ff5f87"/>
  <rect x="110" y="94" width="36" height="30" rx="7" fill="#e6e1f0"/>
  <rect x="168" y="56" width="36" height="30" rx="7" fill="#04b575"/>
  <path d="M60 188l10 -8l-10 -8" fill="none" stroke="#ecfd66" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/>
  <rect x="78" y="186" width="22" height="6" rx="3" fill="#ecfd66"/>
</svg>
SVG
}

# macOS only: turns the SVG into an AppIcon.icns with the tools shipped with the system.
# Best effort: without an icon the app simply gets the default one.
make_icns() {
  local svg="$1" out="$2" work
  work="$(mktemp -d)" || return 1
  if qlmanage -t -s 1024 -o "$work" "$svg" >/dev/null 2>&1 && [ -f "$work/$(basename "$svg").png" ]; then
    local png set sz
    png="$work/$(basename "$svg").png"
    set="$work/AppIcon.iconset"
    mkdir -p "$set"
    for sz in 16 32 128 256 512; do
      sips -z "$sz" "$sz" "$png" --out "$set/icon_${sz}x${sz}.png" >/dev/null 2>&1 || true
      sips -z "$((sz * 2))" "$((sz * 2))" "$png" --out "$set/icon_${sz}x${sz}@2x.png" >/dev/null 2>&1 || true
    done
    iconutil -c icns "$set" -o "$out" >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
  [ -f "$out" ]
}
# Linux: a .desktop launcher (GNOME, KDE, XFCE… application menus). macOS: a small app in
# ~/Applications (Launchpad, Spotlight) that opens jira-tui in Terminal. Never fatal.
add_menu_entry() {
  local bin="$dir/$BIN"
  case "$os" in
    linux)
      local apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
      local file="$apps/$BIN.desktop"
      local icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/scalable/apps"
      local icon=utilities-terminal
      mkdir -p "$apps" || return 1
      # an absolute path works without refreshing the icon cache
      if mkdir -p "$icons" && write_icon_svg "$icons/$BIN.svg" 2>/dev/null; then
        icon="$icons/$BIN.svg"
      fi
      # Exec: quote the path so that a directory with spaces still works
      cat > "$file" <<DESKTOP || return 1
[Desktop Entry]
Type=Application
Name=jira-tui
GenericName=Jira client
Comment=Terminal UI for Jira Cloud
Exec="$bin"
Icon=$icon
Terminal=true
Categories=Development;Utility;ConsoleOnly;
Keywords=jira;tickets;kanban;tempo;
DESKTOP
      chmod 0644 "$file"
      if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database "$apps" >/dev/null 2>&1 || true
      fi
      MENU_ENTRY="$file"
      ;;
    darwin)
      local app="$HOME/Applications/jira-tui.app"
      mkdir -p "$app/Contents/MacOS" || return 1
      cat > "$app/Contents/Info.plist" <<'PLIST' || return 1
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>jira-tui</string>
  <key>CFBundleDisplayName</key><string>jira-tui</string>
  <key>CFBundleIdentifier</key><string>io.github.boutdecode.jira-tui</string>
  <key>CFBundleExecutable</key><string>jira-tui-launcher</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleVersion</key><string>1</string>
</dict>
</plist>
PLIST
      mkdir -p "$app/Contents/Resources"
      local tmpsvg
      tmpsvg="$(mktemp -d)/jira-tui.svg"
      if write_icon_svg "$tmpsvg" 2>/dev/null; then
        make_icns "$tmpsvg" "$app/Contents/Resources/AppIcon.icns" || true
      fi
      rm -rf "$(dirname "$tmpsvg")"
      # AppleScript string: escape backslashes and double quotes in the path
      local esc="${bin//\\/\\\\}"
      esc="${esc//\"/\\\"}"
      cat > "$app/Contents/MacOS/jira-tui-launcher" <<LAUNCHER || return 1
#!/bin/sh
exec /usr/bin/osascript \\
  -e 'tell application "Terminal" to do script "exec \\"$esc\\""' \\
  -e 'tell application "Terminal" to activate'
LAUNCHER
      chmod 0755 "$app/Contents/MacOS/jira-tui-launcher"
      MENU_ENTRY="$app"
      ;;
  esac
}

MENU_ENTRY=""
if [ "${JIRA_TUI_NO_MENU:-}" = "1" ]; then
  :
elif add_menu_entry 2>/dev/null && [ -n "$MENU_ENTRY" ]; then
  say "Menu entry: $MENU_ENTRY"
else
  say "Note: could not add an application menu entry (skipped)."
fi
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not in your PATH. Add it, e.g.:  export PATH=\"$dir:\$PATH\"" ;;
esac
say "Run '$BIN' to get started (first launch asks for your Jira credentials), or '$BIN --demo' to try it."
