package canvas

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"dot/internal/document"
	inp "dot/internal/input"
)

// addPageCanvas is a canvas with ink on its only page and a frame
// function that feeds it events through a real router.
func addPageCanvas(t *testing.T) (*Canvas, func(evs ...pointer.Event)) {
	doc := document.New()
	doc.Pages[0].Layers[0].Items = append(doc.Pages[0].Layers[0].Items,
		&document.Stroke{Width: 1, Points: []f32.Point{{X: 10, Y: 10}, {X: 50, Y: 50}}})
	hist := &document.History{}
	th := material.NewTheme()
	c := New(doc, hist, inp.NewProvider(), th)
	add := func() {
		hist.Do(doc, &document.InsertPage{Index: len(doc.Pages), Page: document.NewPage(100, 100, document.StylePlain, document.White)})
	}
	c.OnScrollPastEnd, c.OnAddPage = add, add
	var r input.Router
	now := time.Now()
	frame := func(evs ...pointer.Event) {
		for _, e := range evs {
			r.Queue(e)
		}
		var ops op.Ops
		now = now.Add(16 * time.Millisecond)
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: now,
			Constraints: layout.Exact(image.Pt(1000, 1600)), Source: r.Source()}
		c.Layout(gtx)
		r.Frame(&ops)
	}
	frame()
	return c, frame
}

func TestScrollPastEndAddsPage(t *testing.T) {
	c, frame := addPageCanvas(t)
	at := f32.Pt(500, 800)
	for i := 0; i < 200 && len(c.Doc.Pages) == 1; i++ {
		// Trackpads mix in events with no vertical movement.
		frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 40)},
			pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(2, 0)})
	}
	if len(c.Doc.Pages) != 2 {
		t.Fatalf("scrolling past the end: %d pages", len(c.Doc.Pages))
	}
	frame()
}

func TestClickPlusAddsPage(t *testing.T) {
	c, frame := addPageCanvas(t)
	for i := 0; i < 100; i++ { // scroll to the end
		frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(500, 800), Scroll: f32.Pt(0, 40)})
		if c.addRadius > 0 && c.addCenter.Y+c.addRadius < float32(c.view.Y) {
			break
		}
	}
	if c.addRadius == 0 {
		t.Fatal("+ never on screen")
	}
	n := len(c.Doc.Pages)
	at := c.addCenter
	frame(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
	frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
	frame()
	if len(c.Doc.Pages) != n+1 {
		t.Fatalf("click: %d pages, want %d", len(c.Doc.Pages), n+1)
	}
}

func TestDragPastEndAddsPage(t *testing.T) {
	c, frame := addPageCanvas(t)
	c.pan.Y = 1e6 // at the end, without scrolling past it
	frame()
	// With the pen, a drag that starts below the last page moves the view.
	for swipe := 0; swipe < 5 && len(c.Doc.Pages) == 1; swipe++ {
		frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(300, 1550)})
		for y := float32(1550); y > 1300; y -= 20 {
			frame(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(300, y)})
		}
		frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(300, 1300)})
	}
	if len(c.Doc.Pages) != 2 {
		t.Fatalf("dragging past the end: %d pages", len(c.Doc.Pages))
	}
	if items := len(c.Doc.Pages[0].Layers[0].Items); items != 1 {
		t.Fatalf("the drag drew: %d items", items)
	}
	frame()
}
