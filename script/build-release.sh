#!/usr/bin/env bash
# Release build override for cli/gh-extension-precompile@v2.
# The action passes the release tag as $1 and uploads every file in dist/*.
# We mirror the action's default platform matrix but inject the version via
# ldflags so the compiled binary reports the real tag instead of "dev".
set -euo pipefail

tag="${1:?release tag required as first argument}"

cd "$(dirname "$0")/.."

platforms=(
  darwin-amd64
  darwin-arm64
  freebsd-386
  freebsd-amd64
  freebsd-arm64
  linux-386
  linux-amd64
  linux-arm
  linux-arm64
  windows-386
  windows-amd64
  windows-arm64
)

mkdir -p dist

for p in "${platforms[@]}"; do
  goos="${p%%-*}"
  goarch="${p#*-}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  echo "building dist/${p}${ext}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${tag}" \
    -o "dist/${p}${ext}" .
done
