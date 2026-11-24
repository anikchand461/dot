#!/bin/sh
# Builds dot! for Linux. Run this ON Linux (or via build-linux-docker.sh).
#
# Build dependencies (Gio needs cgo + the window-system headers):
#   Debian/Ubuntu: sudo apt install gcc pkg-config libwayland-dev libx11-dev \
#       libx11-xcb-dev libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev \
#       libffi-dev libxcursor-dev libvulkan-dev
#   Fedora:        sudo dnf install gcc pkg-config wayland-devel libX11-devel \
#       libxkbcommon-x11-devel mesa-libGLES-devel mesa-libEGL-devel \
#       libXcursor-devel vulkan-headers
#   Arch:          sudo pacman -S gcc pkgconf wayland libx11 libxkbcommon-x11 \
#       mesa libxcursor vulkan-headers
#
# Output: build/dot-linux-<arch>.tar.gz containing the app, a menu entry,
# icons and install.sh (installs for the current user, no root).
set -eu
cd "$(dirname "$0")/.."
arch=$(go env GOARCH)
out=build/dot-linux-$arch
rm -rf "$out"
mkdir -p "$out/icons"
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$out/dot" ./cmd/dot
cp packaging/linux/dot.desktop packaging/linux/dot-mime.xml packaging/linux/install.sh "$out/"
for s in 16 32 48 64 128 256 512; do cp "assets/icon-$s.png" "$out/icons/"; done
tar -C build -czf "build/dot-linux-$arch.tar.gz" "dot-linux-$arch"
echo "Built build/dot-linux-$arch.tar.gz  (unpack, then run ./install.sh)"
