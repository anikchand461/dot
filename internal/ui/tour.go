package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/settings"
)

// The first-run tour: a few cards that spotlight the floating controls,
// shown once, the first time a notebook is opened. "Skip all" (or Esc)
// ends it for good, as does finishing it. The page stays live underneath:
// you can draw and pick tools while it runs. The spotlight and card glide
// from control to control and a hand-drawn arrow points the way.

type tourState struct {
	on               bool
	step, tool       int // tool: the one lit up on the "Your tools" step
	skip, back, next widget.Clickable
	// Where the controls were last drawn.
	dock, style, menu, actions, zoom image.Rectangle
	// Animation: what is on screen now, easing towards the step's targets.
	start, stepAt, last       time.Time
	holeMin, holeMax, cardPos f32.Point
	ring                      float32 // spotlight ring opacity
}

type tourStep struct {
	title, body string
	target      func(a *App) image.Rectangle // nil: a centred card
	tools       bool                         // walk the spotlight across each tool
}

var tourSteps = []tourStep{
	{"Welcome to dot!", "A quick look around — it takes half a minute. The page stays live: go ahead and scribble while we look.", nil, false},
	{"Your tools", "Each has a key. Tap the active tool again for its options.",
		func(a *App) image.Rectangle { return a.tourUI.dock }, true},
	{"Colour & thickness", "Pick colours, pen width, pen type and pressure feel here. [ and ] make the line thinner or thicker.",
		func(a *App) image.Rectangle { return a.tourUI.style }, false},
	{"Undo, pages & setup", "Undo and redo, the pages panel, page size and paper, and every shortcut. This corner hides until you move the pointer near it.",
		func(a *App) image.Rectangle { return a.tourUI.actions }, false},
	{"Menu", "Your notebooks, new, open, annotate a PDF, insert images and export. Notebooks save themselves every few seconds.",
		func(a *App) image.Rectangle { return a.tourUI.menu }, false},
	{"Zoom & pressure", "Pinch or ⌘-scroll to zoom, two fingers to move around. With a pen tablet, pressure is picked up automatically.",
		func(a *App) image.Rectangle { return a.tourUI.zoom }, false},
	{"You're set", "Draw and hold still to snap a shape. Press F for focus mode and ? for all shortcuts. Happy writing!", nil, false},
}

var tourTools = [...][2]string{{"Select", "V"}, {"Pen", "P"}, {"Highlighter", "H"}, {"Eraser", "E"},
	{"Shapes", "S"}, {"Fill", "B"}, {"Text", "T"}, {"Laser pointer", "L"}, {"Pan", "G"}}

const tourToolEvery = 1100 * time.Millisecond

// startTour begins the tour the first time a notebook is shown. Another
// window may have finished it meanwhile, so the saved settings decide.
func (a *App) startTour() {
	if a.cfg.TourDone || a.tourUI.on {
		return
	}
	if settings.Load().TourDone {
		a.cfg.TourDone = true
		return
	}
	a.tourUI.on = true
	a.tourGo(0)
	a.showMenu, a.showHelp, a.showStyle = false, false, false
}

// endTour closes the tour and remembers never to show it again.
func (a *App) endTour() {
	a.tourUI.on = false
	a.cfg.TourDone = true
	settings.Save(a.collectSettings())
}

// tourGo shows step i; its animations restart on the next frame.
func (a *App) tourGo(i int) {
	a.tourUI.step, a.tourUI.stepAt = i, time.Time{}
}

func (a *App) tourNext() {
	if a.tourUI.step+1 >= len(tourSteps) {
		a.endTour()
		return
	}
	a.tourGo(a.tourUI.step + 1)
}

func (a *App) tourClicks(gtx C) {
	t := &a.tourUI
	if t.skip.Clicked(gtx) {
		a.endTour()
	}
	if t.back.Clicked(gtx) && t.step > 0 {
		a.tourGo(t.step - 1)
	}
	if t.next.Clicked(gtx) {
		a.tourNext()
	}
}

// tourToolRect is where tool i sits in the dock (see dock: 36dp buttons
// with 2dp either side, inside the pill's 6dp/4dp padding).
func (a *App) tourToolRect(gtx C, i int) image.Rectangle {
	d := a.tourUI.dock
	x := d.Min.X + gtx.Dp(6) + i*gtx.Dp(40) + gtx.Dp(2)
	return image.Rect(x, d.Min.Y+gtx.Dp(4), x+gtx.Dp(36), d.Min.Y+gtx.Dp(40))
}

func lerpPt(a, b f32.Point, k float32) f32.Point { return a.Add(b.Sub(a).Mul(k)) }

func clamp01(v float32) float32 { return clamp(v, 0, 1) }

// easeOut is a cubic ease-out over 0..1.
func easeOut(v float32) float32 { v = 1 - clamp01(v); return 1 - v*v*v }

func fade(c color.NRGBA, f float32) color.NRGBA {
	c.A = uint8(float32(c.A) * clamp01(f))
	return c
}

// tour dims the window around the current step's control and shows its
// card beside it, with an arrow between them. Only the card takes clicks.
func (a *App) tour(gtx C) {
	a.tourClicks(gtx)
	t := &a.tourUI
	if !t.on {
		return
	}
	gtx.Execute(op.InvalidateCmd{}) // keep animating while the tour is up
	now := gtx.Now
	if t.start.IsZero() {
		t.start = now
	}
	if t.stepAt.IsZero() {
		t.stepAt = now
	}
	k := float32(1)
	if !t.last.IsZero() {
		dt := min(now.Sub(t.last).Seconds(), 0.1)
		k = float32(1 - math.Exp(-dt*9))
	}
	t.last = now
	full := gtx.Constraints.Max
	st := tourSteps[t.step]
	since := now.Sub(t.stepAt)
	secs := float32(now.Sub(t.start).Seconds())

	// Spotlight target: the control, or a point in the middle (the
	// spotlight closes) for the welcome and closing cards.
	centre := layout.FPt(full).Mul(0.5)
	tMin, tMax, ring := centre, centre, float32(0)
	var anchor image.Rectangle // what the card sits beside
	if st.target != nil {
		r := st.target(a)
		anchor = r
		if st.tools {
			t.tool = int(since/tourToolEvery) % len(tourTools)
			r = a.tourToolRect(gtx, t.tool)
		}
		r = r.Inset(-gtx.Dp(6))
		tMin, tMax, ring = layout.FPt(r.Min), layout.FPt(r.Max), 1
	}
	t.holeMin, t.holeMax = lerpPt(t.holeMin, tMin, k), lerpPt(t.holeMax, tMax, k)
	t.ring += (ring - t.ring) * k

	// Dim everything but the spotlight; fade in when the tour starts.
	hole := image.Rectangle{Min: t.holeMin.Round(), Max: t.holeMax.Round()}
	dim := fade(color.NRGBA{0, 0, 0, 0x80}, secs/0.4)
	for _, r := range []image.Rectangle{
		{Max: image.Pt(full.X, hole.Min.Y)},
		{Min: image.Pt(0, hole.Max.Y), Max: full},
		{Min: image.Pt(0, hole.Min.Y), Max: image.Pt(hole.Min.X, hole.Max.Y)},
		{Min: image.Pt(hole.Max.X, hole.Min.Y), Max: image.Pt(full.X, hole.Max.Y)},
	} {
		paint.FillShape(gtx.Ops, dim, clip.Rect(r).Op())
	}
	if t.ring > 0.02 {
		rad := gtx.Dp(12)
		stroke := func(r image.Rectangle, w float32, c color.NRGBA) {
			rr := clip.UniformRRect(r, rad+(r.Dx()-hole.Dx())/2)
			paint.FillShape(gtx.Ops, c, clip.Stroke{Path: rr.Path(gtx.Ops), Width: w}.Op())
		}
		stroke(hole, float32(gtx.Dp(2)), fade(cAccent, t.ring))
		// A ring ripples outwards, like a tap.
		p := float32(math.Mod(float64(secs), 1.6) / 1.6)
		stroke(hole.Inset(-int(p*float32(gtx.Dp(16)))), float32(gtx.Dp(2)), fade(cAccent, t.ring*(1-p)*0.8))
	}

	// Measure the card, then let it glide to its place: beside the
	// control (below it, or above when the control sits low) and a little
	// to the side so the arrow can curve, kept inside the window.
	cg := gtx
	cg.Constraints.Min = image.Point{}
	cg.Constraints.Max.X = min(full.X-gtx.Dp(32), gtx.Dp(340))
	m := op.Record(gtx.Ops)
	dims := a.tourCard(cg, st, since)
	call := m.Stop()
	cs := layout.FPt(dims.Size)
	want := centre.Sub(cs.Mul(0.5))
	side := float32(1)
	below := true
	if !anchor.Empty() {
		ac := layout.FPt(anchor.Min.Add(anchor.Max)).Mul(0.5)
		if ac.X > centre.X {
			side = -1
		}
		below = ac.Y < centre.Y
		want.X = ac.X - cs.X/2 + side*float32(gtx.Dp(90))
		gap := float32(gtx.Dp(70))
		if below {
			want.Y = float32(anchor.Max.Y) + gap
		} else {
			want.Y = float32(anchor.Min.Y) - gap - cs.Y
		}
		edge := float32(gtx.Dp(16))
		want.X = clamp(want.X, edge, float32(full.X)-cs.X-edge)
		want.Y = clamp(want.Y, edge, float32(full.Y)-cs.Y-edge)
	}
	if t.cardPos == (f32.Point{}) {
		t.cardPos = want
	}
	t.cardPos = lerpPt(t.cardPos, want, k)

	if !anchor.Empty() && t.ring > 0.5 {
		a.tourArrow(gtx, cs, below, side, since, secs)
	}

	// The card fades and rises in on each step.
	in := easeOut(float32(since.Seconds()) / 0.3)
	pos := t.cardPos.Add(f32.Pt(0, (1-in)*float32(gtx.Dp(10))))
	defer op.Offset(pos.Round()).Push(gtx.Ops).Pop()
	defer paint.PushOpacity(gtx.Ops, in).Pop()
	call.Add(gtx.Ops)
}

// tourArrow draws a curved, hand-drawn arrow from the card to the
// spotlight. It draws itself in on each step and bobs gently.
func (a *App) tourArrow(gtx C, cs f32.Point, below bool, side float32, since time.Duration, secs float32) {
	t := &a.tourUI
	hc := t.holeMin.Add(t.holeMax).Mul(0.5)
	bob := float32(math.Sin(float64(secs)*4)) * float32(gtx.Dp(3))
	off := float32(gtx.Dp(10))
	start := f32.Pt(t.cardPos.X+cs.X/2-side*cs.X/4, t.cardPos.Y-off)
	end := f32.Pt(hc.X, t.holeMax.Y+off+bob)
	if !below {
		start.Y = t.cardPos.Y + cs.Y + off
		end.Y = t.holeMin.Y - off - bob
	}
	d := end.Sub(start)
	if math.Hypot(float64(d.X), float64(d.Y)) < float64(gtx.Dp(24)) {
		return // the card is right against the control
	}
	// Bow the curve sideways, away from the card's offset.
	ctrl := start.Add(d.Mul(0.5)).Add(f32.Pt(-d.Y, d.X).Mul(0.3 * side))
	if !below {
		ctrl = start.Add(d.Mul(0.5)).Add(f32.Pt(d.Y, -d.X).Mul(0.3 * side))
	}
	bez := func(u float32) f32.Point {
		v := 1 - u
		return start.Mul(v * v).Add(ctrl.Mul(2 * u * v)).Add(end.Mul(u * u))
	}
	p := easeOut((float32(since.Seconds()) - 0.15) / 0.5)
	if p <= 0 {
		return
	}
	const n = 32
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(start)
	for i := 1; i <= n; i++ {
		path.LineTo(bez(p * float32(i) / n))
	}
	// Arrowhead along the curve's direction at its tip.
	tip := bez(p)
	tan := bez(p).Sub(bez(max(0, p-0.05)))
	if l := float32(math.Hypot(float64(tan.X), float64(tan.Y))); l > 0 && p > 0.2 {
		tan = tan.Mul(1 / l)
		h := float32(gtx.Dp(11))
		for _, ang := range []float64{2.6, -2.6} {
			s, c := float32(math.Sin(ang)), float32(math.Cos(ang))
			w := f32.Pt(tan.X*c-tan.Y*s, tan.X*s+tan.Y*c).Mul(h)
			path.MoveTo(tip)
			path.LineTo(tip.Add(w))
		}
	}
	paint.FillShape(gtx.Ops, cAccent, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(2.5))}.Op())
}

func (a *App) tourCard(gtx C, st tourStep, since time.Duration) D {
	t := &a.tourUI
	last := t.step == len(tourSteps)-1
	return card(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.tourDots),
			layout.Rigid(func(gtx C) D {
				l := material.Body1(a.th, st.title)
				l.Color, l.Font.Weight, l.TextSize = cText, font.Bold, unit.Sp(16)
				d := layout.Inset{Top: 10, Bottom: 6}.Layout(gtx, l.Layout)
				if st.target == nil { // the welcome and closing cards get a scribble
					squiggle(gtx, image.Pt(0, d.Size.Y-gtx.Dp(3)), gtx.Dp(130), float32(since.Seconds()))
					d.Size.Y += gtx.Dp(6)
				}
				return d
			}),
			layout.Rigid(func(gtx C) D { return a.text(gtx, st.body, cMuted, 13) }),
			layout.Rigid(func(gtx C) D {
				if !st.tools {
					return D{}
				}
				tl := tourTools[t.tool]
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							l := material.Body1(a.th, tl[0])
							l.Color, l.Font.Weight, l.TextSize = cText, font.SemiBold, unit.Sp(15)
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return a.text(gtx, "press "+tl[1], cAccent, 13) })
						}),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							if last {
								return D{}
							}
							return t.skip.Layout(gtx, func(gtx C) D {
								col := cMuted
								if t.skip.Hovered() {
									col = cText
								}
								return layout.Inset{Top: 5, Bottom: 5, Right: 8}.Layout(gtx, func(gtx C) D {
									return a.text(gtx, "Skip all", col, 13)
								})
							})
						}),
						layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 0)} }),
						layout.Rigid(func(gtx C) D {
							if t.step == 0 {
								return D{}
							}
							return a.chip(gtx, &t.back, "Back", false)
						}),
						layout.Rigid(func(gtx C) D {
							label := "Next"
							switch {
							case t.step == 0:
								label = "Show me"
							case last:
								label = "Start writing"
							}
							return a.chip(gtx, &t.next, label, true)
						}),
					)
				})
			}),
		)
	})
}

// squiggle writes a wavy accent underline of width w at o, drawing itself
// in over the first moments of the step.
func squiggle(gtx C, o image.Point, w int, secs float32) {
	p := easeOut((secs - 0.2) / 0.7)
	if p <= 0 {
		return
	}
	const n = 40
	amp, wave := float32(gtx.Dp(2.5)), float32(gtx.Dp(14))
	var path clip.Path
	path.Begin(gtx.Ops)
	for i := 0; i <= n; i++ {
		x := float32(w) * p * float32(i) / n
		pt := layout.FPt(o).Add(f32.Pt(x, amp*float32(math.Sin(float64(x/wave*math.Pi)))))
		if i == 0 {
			path.MoveTo(pt)
		} else {
			path.LineTo(pt)
		}
	}
	paint.FillShape(gtx.Ops, cAccent, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(2))}.Op())
}

// tourDots shows progress: one dot per step, the current one wider.
func (a *App) tourDots(gtx C) D {
	h, gap := gtx.Dp(6), gtx.Dp(5)
	x := 0
	for i := range tourSteps {
		w, col := h, cBorder
		if i == a.tourUI.step {
			w, col = gtx.Dp(18), cAccent
		}
		r := image.Rect(x, 0, x+w, h)
		paint.FillShape(gtx.Ops, col, clip.UniformRRect(r, h/2).Op(gtx.Ops))
		x += w + gap
	}
	return D{Size: image.Pt(x-gap, h)}
}
