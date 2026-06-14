#!/usr/bin/env bash
# Build the gh-copilot-config binary for the current platform. Used by
# `gh extension install .` and as a local build helper. The release workflow
# cross-compiles via cli/gh-extension-precompile instead.
set -euo pipefail

version="${1:-$(git describe --tags 2>/dev/null || echo dev)}"

cd "$(dirname "$0")/.."
go build -trimpath -ldflags "-s -w -X main.version=${version}" -o gh-copilot-config .
