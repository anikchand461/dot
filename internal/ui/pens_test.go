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
	"dot/internal/ink"
	"dot/internal/input"
)

// UI_SNAP=/dir go test ./internal/ui -run PenCards renders the cards.
func TestPenCardsSnapshot(t *testing.T) {
	dir := os.Getenv("UI_SNAP")
	if dir == "" {
		t.Skip("UI_SNAP not set")
	}
	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc := document.New()
	doc.Pages[0].Background.Color = document.Black
	a := &App{th: th, doc: doc, cv: canvas.New(doc, &document.History{}, input.NewProvider(), th)}
	a.cv.PenColor = penPalette[8]
	a.cv.PenWidth = 1.6
	a.cv.PenStyle = ink.Fountain
	w, h := 600, 460
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip(err)
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
		Constraints: layout.Exact(image.Pt(w, h))}
	paint.FillShape(&ops, cSurface, clip.Rect{Max: image.Pt(w, h)}.Op())
	layout.UniformInset(10).Layout(gtx, a.penCards)
	win.Frame(&ops)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	win.Screenshot(img)
	f, _ := os.Create(filepath.Join(dir, "pencards.png"))
	png.Encode(f, img)
	f.Close()
}
