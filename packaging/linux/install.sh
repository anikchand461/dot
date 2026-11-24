#!/bin/sh
# Installs dot! for the current user (no root needed):
#   ~/.local/bin/dot, a menu entry, icons and .dott/.xopp file types.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
data=${XDG_DATA_HOME:-$HOME/.local/share}
mkdir -p "$HOME/.local/bin" "$data/applications" "$data/mime/packages"
install -m 755 "$here/dot" "$HOME/.local/bin/dot"
install -m 644 "$here/dot.desktop" "$data/applications/dot.desktop"
install -m 644 "$here/dot-mime.xml" "$data/mime/packages/dot.xml"
for s in 16 32 48 64 128 256 512; do
	mkdir -p "$data/icons/hicolor/${s}x${s}/apps"
	install -m 644 "$here/icons/icon-$s.png" "$data/icons/hicolor/${s}x${s}/apps/dot.png"
done
command -v update-mime-database >/dev/null && update-mime-database "$data/mime" || true
command -v update-desktop-database >/dev/null && update-desktop-database "$data/applications" || true
command -v gtk-update-icon-cache >/dev/null && gtk-update-icon-cache -q "$data/icons/hicolor" || true
echo "dot! installed. Launch it from your app menu, or run: dot"
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) echo "(add ~/.local/bin to your PATH to run 'dot' from a terminal)";; esac
