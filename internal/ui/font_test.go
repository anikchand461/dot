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
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/input"
)

// UI_SNAP=/dir go test ./internal/ui -run FontDropdown
func TestFontDropdownSnapshot(t *testing.T) {
	dir := os.Getenv("UI_SNAP")
	if dir == "" {
		t.Skip("UI_SNAP not set")
	}
	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc := document.New()
	a := &App{th: th, doc: doc, cv: canvas.New(doc, &document.History{}, input.NewProvider(), th)}
	a.cv.TextFont = "Caveat"
	a.fmtFont.open = true
	w, h := 900, 900
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip(err)
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
		Constraints: layout.Exact(image.Pt(w, h))}
	paint.FillShape(&ops, cSurface, clip.Rect{Max: image.Pt(w, h)}.Op())
	layout.UniformInset(10).Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return pill(gtx, a.fontRow(&a.fmtFont, &a.fmtBold, &a.fmtItalic))
	})
	win.Frame(&ops)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	win.Screenshot(img)
	f, _ := os.Create(filepath.Join(dir, "fontdrop.png"))
	png.Encode(f, img)
	f.Close()
}
