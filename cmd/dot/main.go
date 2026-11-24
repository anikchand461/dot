// Command dot (dot!) is an offline, Xournal++-compatible handwriting and
// note-taking app. Usage: dot [file.xopp | file.pdf ...]
//
// Every window is independent (its own file, undo and tools), like browser
// windows; the app quits when the last window closes.
package main

import (
	"log"
	"os"
	"strings"
	"sync"

	"gioui.org/app"
	"gioui.org/unit"

	"dot/internal/ui"
)

var windows sync.WaitGroup

func openWindow(path string) {
	windows.Add(1)
	go func() {
		defer windows.Done()
		w := new(app.Window)
		w.Option(app.Title("dot!"), app.Size(unit.Dp(1280), unit.Dp(860)))
		if err := ui.Run(w, path, ui.Options{NewWindow: openWindow}); err != nil {
			log.Print(err)
		}
	}()
}

func main() {
	var paths []string
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-psn") { // macOS Finder process serial number
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		paths = []string{""}
	}
	for _, p := range paths { // one window per file given
		openWindow(p)
	}
	go func() {
		windows.Wait()
		os.Exit(0)
	}()
	app.Main()
}
