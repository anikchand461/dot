//go:build linux

package clipboard

import (
	"crypto/sha1"
	"os/exec"
	"strings"
	"time"
)

// Linux desktops have no single clipboard API; use the standard tools:
// wl-paste (Wayland, package wl-clipboard), then xclip or xsel (X11).

func run(name string, args ...string) []byte {
	if _, err := exec.LookPath(name); err != nil {
		return nil
	}
	cmd := exec.Command(name, args...)
	done := make(chan []byte, 1)
	go func() {
		out, err := cmd.Output()
		if err != nil {
			out = nil
		}
		done <- out
	}()
	select {
	case out := <-done:
		return out
	case <-time.After(2 * time.Second): // never hang the UI on a stuck tool
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return nil
	}
}

func readImage() []byte {
	for _, typ := range []string{"image/png", "image/jpeg"} {
		if b := run("wl-paste", "--no-newline", "--type", typ); len(b) > 0 {
			return b
		}
		if b := run("xclip", "-selection", "clipboard", "-t", typ, "-o"); len(b) > 0 {
			return b
		}
	}
	return nil
}

func readText() string {
	if b := run("wl-paste", "--no-newline", "--type", "text/plain"); len(b) > 0 {
		return string(b)
	}
	if b := run("xclip", "-selection", "clipboard", "-o"); len(b) > 0 {
		return string(b)
	}
	return string(run("xsel", "--clipboard", "--output"))
}

// changeCount has no cheap native source here; a hash of the clipboard's
// advertised types and text stands in for it.
func changeCount() int64 {
	sig := run("wl-paste", "--list-types")
	if len(sig) == 0 {
		sig = run("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	}
	sig = append(sig, []byte(strings.TrimSpace(readText()))...)
	h := sha1.Sum(sig)
	var n int64
	for _, b := range h[:8] {
		n = n<<8 | int64(b)
	}
	return n
}
