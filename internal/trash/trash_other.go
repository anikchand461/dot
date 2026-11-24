//go:build !windows && !(darwin && cgo)

package trash

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// move follows the freedesktop.org Trash spec (GNOME, KDE, ...): the file
// goes to ~/.local/share/Trash/files with a .trashinfo record, so the
// desktop's Trash can restore it.
func move(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		base = filepath.Join(home, ".local", "share")
	}
	files, info := filepath.Join(base, "Trash", "files"), filepath.Join(base, "Trash", "info")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	name := filepath.Base(abs)
	ext := filepath.Ext(name)
	stem := name[:len(name)-len(ext)]
	for i := 2; ; i++ { // a free name in the Trash
		if _, err := os.Stat(filepath.Join(files, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s %d%s", stem, i, ext)
	}
	record := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: abs}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
	if err := os.WriteFile(filepath.Join(info, name+".trashinfo"), []byte(record), 0o600); err != nil {
		return err
	}
	if err := os.Rename(abs, filepath.Join(files, name)); err != nil {
		os.Remove(filepath.Join(info, name+".trashinfo"))
		return err
	}
	return nil
}
