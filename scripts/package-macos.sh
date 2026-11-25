#!/bin/sh
# Builds two macOS apps, one per processor:
#   build/macos-apple-silicon/dot.app   (M1, M2, M3, M4 … Macs)
#   build/macos-intel/dot.app           (Intel Macs)
# plus a drag-to-Applications disk image for each: build/dot-macos-*.dmg
# Each contains only its own machine code, so it is about half the size
# of a universal app. Build just one with: ./scripts/package-macos.sh arm64
set -eu
cd "$(dirname "$0")/.."
VERSION=${VERSION:-0.4.0}
targets=${1:-"arm64 amd64"}

for arch in $targets; do
	case $arch in
	arm64) dir=build/macos-apple-silicon; name=macos-apple-silicon; min=12.0 ;; # Go 1.25+ needs macOS 12
	amd64) dir=build/macos-intel; name=macos-intel; min=12.0 ;;
	*) echo "unknown arch $arch (use arm64 or amd64)"; exit 1 ;;
	esac
	APP=$dir/dot.app
	rm -rf "$APP"
	mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
	CGO_ENABLED=1 GOARCH=$arch MACOSX_DEPLOYMENT_TARGET=$min \
		CGO_CFLAGS="-mmacosx-version-min=$min" CGO_LDFLAGS="-mmacosx-version-min=$min" \
		go build -trimpath -ldflags="-s -w" -o "$APP/Contents/MacOS/dot" ./cmd/dot
	cp assets/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"
	cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>dot!</string>
	<key>CFBundleDisplayName</key><string>dot!</string>
	<key>CFBundleIdentifier</key><string>local.dot.app</string>
	<key>CFBundleExecutable</key><string>dot</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleVersion</key><string>${VERSION}</string>
	<key>CFBundleShortVersionString</key><string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key><string>${min}</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>CFBundleIconFile</key><string>AppIcon</string>
</dict>
</plist>
PLIST
	# Ad-hoc signature so Gatekeeper on this Mac runs it; use a Developer ID
	# identity + notarisation to distribute to other Macs.
	codesign --force --deep --sign - "$APP"
	echo "Built $APP ($(du -sh "$APP" | cut -f1), $arch)"

	# Disk image for downloading: open it and drag dot! to Applications.
	DMG="build/dot-$name.dmg"
	STAGE=$(mktemp -d)
	cp -R "$APP" "$STAGE/"
	ln -s /Applications "$STAGE/Applications"
	rm -f "$DMG"
	hdiutil create -quiet -volname "dot!" -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$DMG"
	rm -rf "$STAGE"
	echo "Built $DMG ($(du -h "$DMG" | cut -f1))"
done
