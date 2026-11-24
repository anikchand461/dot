// Package trash moves files to the system Trash / Recycle Bin, so a
// deleted notebook can always be restored.
package trash

// Move puts path in the Trash.
func Move(path string) error { return move(path) }
