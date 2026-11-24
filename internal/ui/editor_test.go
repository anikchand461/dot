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
	"gioui.org/unit"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/input"
	"dot/internal/settings"
)

// UI_SNAP=/out NOTE=/x.xopp go test ./internal/ui -run EditorSnapshot
// renders the writing screen with corners shown (hover) and hidden.
func TestEditorSnapshot(t *testing.T) {
	dir, note := os.Getenv("UI_SNAP"), os.Getenv("NOTE")
	if dir == "" || note == "" {
		t.Skip("UI_SNAP / NOTE not set")
	}
	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc, err := document.OpenFile(note)
	if err != nil {
		t.Fatal(err)
	}
	hist := &document.History{}
	in := input.NewProvider()
	a := &App{th: th, doc: doc, hist: hist, in: in, cv: canvas.New(doc, hist, in, th),
		cfg: settings.Default(), penPicker: newColorPicker(), paperPicker: newColorPicker(), fillPicker: newColorPicker()}
	w, h := 2400, 1500
	for _, tc := range []struct {
		name string
		at   image.Point
	}{{"editor-idle", image.Pt(1200, 700)}, {"editor-hover", image.Pt(60, 1450)}} {
		win, err := headless.NewWindow(w, h)
		if err != nil {
			t.Skip(err)
		}
		a.pointerAt = tc.at
		a.hoverSeen = nil
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
			Constraints: layout.Exact(image.Pt(w, h))}
		a.canvasArea(gtx)
		win.Frame(&ops)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		win.Screenshot(img)
		f, _ := os.Create(filepath.Join(dir, tc.name+".png"))
		png.Encode(f, img)
		f.Close()
		win.Release()
	}
}
