#!/bin/sh
# Builds the Claude Desktop one-click bundle (garmin-mcp.mcpb).
#
#   mcpb/build.sh <version> [output-dir]
#
# Cross-compiles the server for macOS (universal), Linux (amd64) and Windows
# (amd64), drops them next to manifest.json and zips the result. Requires only
# Go and zip; the macOS universal binary is produced by ./mcpb/makefat.
set -eu

version="${1:?usage: mcpb/build.sh <version> [output-dir]}"
version="${version#v}"
out="${2:-dist}"

root=$(cd "$(dirname "$0")/.." && pwd)
case "$out" in /*) ;; *) out="$root/$out" ;; esac
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/server" "$out"

ldflags="-s -w -X main.version=$version"
build() { # GOOS GOARCH output
  echo "building $1/$2"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -C "$root" -trimpath -ldflags "$ldflags" -o "$3" .
}

build darwin  amd64 "$stage/darwin-amd64"
build darwin  arm64 "$stage/darwin-arm64"
build linux   amd64 "$stage/server/garmin-mcp-linux"
build windows amd64 "$stage/server/garmin-mcp-windows.exe"

echo "creating macOS universal binary"
go run -C "$root" ./mcpb/makefat \
  "$stage/server/garmin-mcp-darwin" "$stage/darwin-amd64" "$stage/darwin-arm64"
rm "$stage/darwin-amd64" "$stage/darwin-arm64"
chmod 0755 "$stage"/server/*

sed "s/__VERSION__/$version/" "$root/mcpb/manifest.json" > "$stage/manifest.json"
cp "$root/LICENSE" "$stage/LICENSE"

bundle="$out/garmin-mcp.mcpb"
rm -f "$bundle"
(cd "$stage" && zip -q -r -X "$bundle" manifest.json LICENSE server)
echo "wrote $bundle"
