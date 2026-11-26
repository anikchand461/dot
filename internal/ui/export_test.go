package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/input"
)

func exportTestApp(pages int) *App {
	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc := document.New()
	for len(doc.Pages) < pages {
		doc.Pages = append(doc.Pages, doc.Pages[0].Clone())
	}
	a := &App{th: th, doc: doc, cv: canvas.New(doc, &document.History{}, input.NewProvider(), th),
		results: make(chan func(), 8)}
	a.exportPDF()
	return a
}

func TestExportPages(t *testing.T) {
	a := exportTestApp(5)
	s := &a.exportUI
	if got, _ := a.exportPages(); len(got) != 5 {
		t.Fatalf("all: got %v", got)
	}
	s.mode = exportRange
	s.from.SetText("4")
	s.to.SetText("2")
	if got, _ := a.exportPages(); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("range 4..2: got %v", got)
	}
	s.to.SetText("9")
	if got, msg := a.exportPages(); got != nil || msg == "" {
		t.Fatalf("out of range: got %v %q", got, msg)
	}
	s.mode = exportAll
	s.blank.Value = true
	if got, msg := a.exportPages(); got != nil || msg == "" {
		t.Fatalf("all blank: got %v %q", got, msg)
	}
	a.doc.Pages[2].Layers[0].Items = append(a.doc.Pages[2].Layers[0].Items, &document.Stroke{})
	if got, _ := a.exportPages(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("skip blank: got %v", got)
	}
	// Turning the ruling off changes only the exported copy.
	s.ruling.Value = false
	d := a.exportDoc([]int{2})
	if d.Pages[0].Background.Style != document.StylePlain || a.doc.Pages[2].Background.Style == document.StylePlain {
		t.Fatalf("ruling: export %q, doc %q", d.Pages[0].Background.Style, a.doc.Pages[2].Background.Style)
	}
}

// UI_SNAP=/dir go test ./internal/ui -run ExportDialogSnapshot
func TestExportDialogSnapshot(t *testing.T) {
	dir := os.Getenv("UI_SNAP")
	if dir == "" {
		t.Skip("UI_SNAP not set")
	}
	a := exportTestApp(3)
	a.exportUI.mode = exportRange
	a.doc.Pages[0].Layers[0].Items = append(a.doc.Pages[0].Layers[0].Items, &document.Stroke{})
	w, h := 1000, 1600
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip(err)
	}
	frame := func(name string, draw func(gtx C)) {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
			Constraints: layout.Exact(image.Pt(w, h))}
		draw(gtx)
		win.Frame(&ops)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		win.Screenshot(img)
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, img)
		f.Close()
	}
	a.cv.OnAddPage = func() {}
	a.cv.Zoom = 0.2
	a.w = new(app.Window)
	draw := func(gtx C) { a.cv.Layout(gtx); a.exportDialog(gtx) }
	frame("export.png", draw)
	(<-a.results)() // the size estimate
	frame("export.png", draw)
	a.exportUI.on = false
	frame("addpage.png", func(gtx C) { a.cv.Layout(gtx) })
}
