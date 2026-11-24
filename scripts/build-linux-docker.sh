#!/bin/sh
# Builds the Linux version from macOS or Windows using Docker.
#   ./scripts/build-linux-docker.sh          # x86-64 (most PCs)
#   ARCH=arm64 ./scripts/build-linux-docker.sh
set -eu
cd "$(dirname "$0")/.."
arch=${ARCH:-amd64}
docker run --rm --platform "linux/$arch" \
	-v "$PWD":/src -w /src \
	-v "$(go env GOMODCACHE)":/go/pkg/mod \
	golang:1.26-bookworm sh -c '
		apt-get update -qq &&
		apt-get install -y -qq pkg-config libwayland-dev libx11-dev libx11-xcb-dev \
			libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev \
			libxcursor-dev libvulkan-dev >/dev/null &&
		./scripts/build-linux.sh'
