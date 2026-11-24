package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/export"
	"dot/internal/input"
	"dot/internal/settings"
)

// UI_SNAP=/out HOME_NOTES=/dir go test ./internal/ui -run HomeSnapshot
func TestHomeSnapshot(t *testing.T) {
	dir, notesDir := os.Getenv("UI_SNAP"), os.Getenv("HOME_NOTES")
	if dir == "" || notesDir == "" {
		t.Skip("UI_SNAP / HOME_NOTES not set")
	}
	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc := document.New()
	a := &App{th: th, doc: doc, cv: canvas.New(doc, &document.History{}, input.NewProvider(), th),
		cfg: settings.Settings{SaveDir: notesDir}, home: true}
	files, _ := filepath.Glob(filepath.Join(notesDir, "*.*"))
	for i, p := range files {
		d, err := document.OpenFile(p)
		if err != nil {
			continue
		}
		img, err := export.Thumbnail(d.Pages[0], 300, 420)
		if err != nil {
			t.Fatal(err)
		}
		n := &noteInfo{path: p, name: strings_TrimExt(filepath.Base(p)), mod: time.Now().Add(-time.Duration(i*7) * time.Hour),
			pages: len(d.Pages), pdf: d.Pages[0].Background.Type == "pdf", loaded: true,
			thumb: paint.NewImageOp(img), thumbSize: img.Bounds().Size()}
		a.homeUI.notes = append(a.homeUI.notes, n)
	}
	w, h := 2600, 1900
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip(err)
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
		Constraints: layout.Exact(image.Pt(w, h))}
	a.homeView(gtx)
	win.Frame(&ops)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	win.Screenshot(img)
	f, _ := os.Create(filepath.Join(dir, "home.png"))
	png.Encode(f, img)
	f.Close()
}

func strings_TrimExt(s string) string { return s[:len(s)-len(filepath.Ext(s))] }
