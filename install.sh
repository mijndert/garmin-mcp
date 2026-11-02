#!/bin/sh
# Installs garmin-mcp on macOS or Linux and shows how to add it to Claude Code.
#
#   curl -fsSL https://raw.githubusercontent.com/mijndert/garmin-mcp/main/install.sh | sh
#
# Environment overrides:
#   GARMIN_MCP_VERSION      release tag to install (default: latest), e.g. v0.1.0
#   GARMIN_MCP_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#   GARMIN_PERSONAL_API_KEY if set and `claude` is installed, the server is
#                           registered with Claude Code automatically.
set -eu

REPO="mijndert/garmin-mcp"
BIN="garmin-mcp"
RELEASE_BASE="${GARMIN_MCP_RELEASE_BASE:-https://github.com/$REPO/releases}"
INSTALL_DIR="${GARMIN_MCP_INSTALL_DIR:-$HOME/.local/bin}"

say()  { printf '%s\n' "$*"; }
fail() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed"; }

need curl
need tar

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) fail "unsupported OS '$os'. On Windows, download the zip from https://github.com/$REPO/releases" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported CPU architecture '$arch'" ;;
esac

version="${GARMIN_MCP_VERSION:-}"
if [ -z "$version" ]; then
  # Follow the /releases/latest redirect to learn the tag without using the API.
  version=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASE_BASE/latest" | sed 's#.*/tag/##')
  [ -n "$version" ] || fail "could not determine the latest release"
fi
bare=${version#v}

archive="${BIN}_${bare}_${os}_${arch}.tar.gz"
url="$RELEASE_BASE/download/$version/$archive"
sums_url="$RELEASE_BASE/download/$version/checksums.txt"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading $BIN $version for $os/$arch..."
curl -fsSL -o "$tmp/$archive" "$url" || fail "download failed: $url"
curl -fsSL -o "$tmp/checksums.txt" "$sums_url" || fail "download failed: $sums_url"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || fail "no checksum for $archive in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
say "Installed $INSTALL_DIR/$BIN"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say "Note: $INSTALL_DIR is not on your PATH. Add it to your shell profile:"
     say "  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

say ""
if command -v claude >/dev/null 2>&1; then
  if [ -n "${GARMIN_PERSONAL_API_KEY:-}" ]; then
    say "Registering with Claude Code (user scope)..."
    claude mcp remove -s user garmin >/dev/null 2>&1 || true
    claude mcp add garmin -s user -e "GARMIN_PERSONAL_API_KEY=$GARMIN_PERSONAL_API_KEY" -- "$INSTALL_DIR/$BIN"
    say "Done. Start claude and ask: \"how did I sleep last night?\""
  else
    say "Next: create a Personal API Key at https://connect.garmin.com (Settings), then run:"
    say "  claude mcp add garmin -s user -e GARMIN_PERSONAL_API_KEY=your-key -- $INSTALL_DIR/$BIN"
  fi
else
  say "Next: create a Personal API Key at https://connect.garmin.com (Settings) and add"
  say "$INSTALL_DIR/$BIN to your MCP client with the GARMIN_PERSONAL_API_KEY environment"
  say "variable set. See https://github.com/$REPO#install"
fi
