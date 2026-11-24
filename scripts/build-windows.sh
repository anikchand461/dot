#!/bin/sh
# Cross-compiles build/dot.exe from macOS/Linux. No cgo or C toolchain
# is needed: Gio and the pen hook use only syscalls.
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H windowsgui" -o build/dot.exe ./cmd/dot
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H windowsgui" -o build/dot-arm64.exe ./cmd/dot
echo "Built build/dot.exe and build/dot-arm64.exe"
