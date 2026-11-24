#!/bin/sh
# Regenerates every icon from assets/logo.png (black mark, transparent
# background). On macOS it also builds assets/AppIcon.icns.
set -eu
cd "$(dirname "$0")/.."
go run ./tools/icons
mkdir -p docs/images && cp assets/icon-256.png docs/images/logo.png # README logo
if command -v iconutil >/dev/null 2>&1; then
	set=$(mktemp -d)/AppIcon.iconset
	mkdir -p "$set"
	for s in 16 32 128 256 512; do
		sips -z $s $s assets/icon-1024.png --out "$set/icon_${s}x${s}.png" >/dev/null
		d=$((s * 2))
		sips -z $d $d assets/icon-1024.png --out "$set/icon_${s}x${s}@2x.png" >/dev/null
	done
	iconutil -c icns "$set" -o assets/AppIcon.icns
	echo "assets/AppIcon.icns written"
fi
