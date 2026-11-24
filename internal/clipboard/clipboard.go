// Package clipboard reads images and text from the system clipboard.
// Gio's clipboard only carries text, so images need native code.
package clipboard

// Image returns the clipboard image as PNG bytes, or nil if there is none
// (a screenshot, an image copied from a browser, or an image file copied
// in Finder / Explorer).
func Image() []byte { return readImage() }

// Text returns the clipboard text, or "".
func Text() string { return readText() }

// ChangeCount changes whenever anything is copied system-wide. It tells
// dot! whether the system clipboard is newer than its own ink copy.
func ChangeCount() int64 { return changeCount() }
