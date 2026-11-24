package ui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/ink"
)

// penCards shows the four pen types as cards: a drawing of the pen's tip
// and a handwriting sample written by that pen with the current settings.
func (a *App) penCards(gtx C) D {
	gap := gtx.Dp(8)
	w := (gtx.Constraints.Max.X - gap) / 2
	var rows []layout.FlexChild
	for r := 0; r < 2; r++ {
		r := r
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return a.penCard(gtx, ink.Style(r*2), w) }),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gap, 0)} }),
					layout.Rigid(func(gtx C) D { return a.penCard(gtx, ink.Style(r*2+1), w) }),
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (a *App) penCard(gtx C, st ink.Style, w int) D {
	c := &a.penStyles[st]
	return c.Layout(gtx, func(gtx C) D {
		h := gtx.Dp(104)
		r := image.Rectangle{Max: image.Pt(w, h)}
		active := a.cv.PenStyle == st
		bg := cSurface2
		if c.Hovered() && !active {
			bg = cBorder
		}
		rad := gtx.Dp(12)
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, rad).Op(gtx.Ops))
		if active {
			paint.FillShape(gtx.Ops, cAccent, clip.Stroke{Path: clip.UniformRRect(r.Inset(1), rad).Path(gtx.Ops), Width: float32(gtx.Dp(2))}.Op())
		}
		ink := a.cv.PenColor
		paper := a.doc.Pages[a.cv.Current()].Background.Color
		if document.IsDark(paper) == document.IsDark(ink) { // keep the sample visible
			ink = map[bool]color.NRGBA{true: penPalette[1], false: penPalette[0]}[document.IsDark(paper)]
		}

		// Pen tip drawing, top-left.
		tip := gtx.Dp(46)
		t := op.Offset(image.Pt(gtx.Dp(6), gtx.Dp(4))).Push(gtx.Ops)
		drawTip(gtx, st, tip, ink)
		t.Pop()

		// Name, top-right of the tip.
		t = op.Offset(image.Pt(tip+gtx.Dp(8), gtx.Dp(14))).Push(gtx.Ops)
		l := material.Body2(a.th, styleTitle[st])
		l.Color, l.MaxLines = cText, 1
		if active {
			l.Color = cWhite
		}
		lg := gtx
		lg.Constraints.Min = image.Point{}
		lg.Constraints.Max.X = w - tip - gtx.Dp(12)
		l.Layout(lg)
		t.Pop()
		t = op.Offset(image.Pt(tip+gtx.Dp(8), gtx.Dp(32))).Push(gtx.Ops)
		d := material.Caption(a.th, styleHint[st])
		d.Color, d.MaxLines = cMuted, 1
		d.Layout(lg)
		t.Pop()

		// Handwriting sample on a strip of the current paper colour.
		strip := image.Rect(gtx.Dp(6), gtx.Dp(54), w-gtx.Dp(6), h-gtx.Dp(6))
		paint.FillShape(gtx.Ops, paper, clip.UniformRRect(strip, gtx.Dp(8)).Op(gtx.Ops))
		cl := clip.UniformRRect(strip, gtx.Dp(8)).Push(gtx.Ops)
		t = op.Offset(strip.Min).Push(gtx.Ops)
		a.writeSample(gtx, st, strip.Size(), ink)
		t.Pop()
		cl.Pop()
		return D{Size: r.Max}
	})
}

var (
	styleTitle = map[ink.Style]string{ink.Fountain: "Fountain", ink.Ballpoint: "Ballpoint", ink.Brush: "Brush", ink.Calligraphy: "Calligraphy"}
	styleHint  = map[ink.Style]string{ink.Fountain: "Thin ↔ thick", ink.Ballpoint: "Even line", ink.Brush: "Bold & soft", ink.Calligraphy: "Flat nib"}
)

// writeSample writes cursive loops ("ℓℓℓℓ") with natural pressure:
// heavier on down-strokes, lighter on up-strokes, as a hand would.
func (a *App) writeSample(gtx C, st ink.Style, size image.Point, col color.NRGBA) {
	s := float32(gtx.Dp(1)) * 1.15 // px per pt
	wPt, hPt := float32(size.X)/s, float32(size.Y)/s
	e := ink.Engine{Cfg: a.cv.Ink, Style: st, Base: clamp(a.cv.PenWidth, 1.1, 3)}
	e.Cfg.Smoothing = 0
	const n = 140
	pt := func(i int) f32.Point {
		t := float64(i) / n * 4.3 * 2 * math.Pi // ~4 loops
		x := 0.9*t - 1.35*math.Sin(t)           // prolate cycloid: loops
		y := -1.35 * math.Cos(t)
		return f32.Pt(6+float32(x)*(wPt-12)/float32(0.9*4.3*2*math.Pi+1.35), hPt/2+float32(y)*hPt*0.26)
	}
	press := func(i int) float32 {
		if i == 0 {
			return 0.3
		}
		d := pt(i).Sub(pt(i - 1))
		l := float32(math.Hypot(float64(d.X), float64(d.Y)))
		if l == 0 {
			return 0.5
		}
		return 0.35 + 0.55*max(0, d.Y/l) // down-strokes press harder
	}
	e.Begin(pt(0), press(0), true)
	for i := 1; i <= n; i++ {
		e.Add(pt(i), press(i), 0.15)
	}
	pts, ws := e.Finish(pt(n))
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(gtx.Ops)
	canvas.DrawStroke(gtx.Ops, &document.Stroke{Color: col, Width: e.Base, Points: pts, Widths: ws})
	t.Pop()
}

// Pen tip drawings. Each is drawn upright in a unit box (tip at the
// bottom) and tilted like a pen in a writing hand.
var (
	penBody   = color.NRGBA{0x3e, 0x41, 0x4c, 0xff}
	penShine  = color.NRGBA{0x5c, 0x60, 0x6e, 0xff}
	penGrip   = color.NRGBA{0x22, 0x24, 0x2a, 0xff}
	gold      = color.NRGBA{0xe3, 0xb9, 0x5e, 0xff}
	goldDark  = color.NRGBA{0x9c, 0x76, 0x2c, 0xff}
	silver    = color.NRGBA{0xc9, 0xcd, 0xd6, 0xff}
	silverDim = color.NRGBA{0x8d, 0x92, 0x9e, 0xff}
)

func drawTip(gtx C, st ink.Style, size int, inkCol color.NRGBA) {
	s := float32(size)
	defer op.Affine(f32.AffineId().
		Scale(f32.Point{}, f32.Pt(s, s)).
		Rotate(f32.Pt(s/2, s/2), 0.62)).Push(gtx.Ops).Pop()
	poly := func(col color.NRGBA, pts ...f32.Point) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(pts[0])
		for _, q := range pts[1:] {
			p.LineTo(q)
		}
		p.Close()
		paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
	}
	line := func(col color.NRGBA, w float32, a, b f32.Point) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(a)
		p.LineTo(b)
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: w}.Op())
	}
	dot := func(col color.NRGBA, c f32.Point, r float32) {
		var p clip.Path
		p.Begin(gtx.Ops)
		const k = 0.5522847
		p.MoveTo(c.Add(f32.Pt(r, 0)))
		p.CubeTo(c.Add(f32.Pt(r, -k*r)), c.Add(f32.Pt(k*r, -r)), c.Add(f32.Pt(0, -r)))
		p.CubeTo(c.Add(f32.Pt(-k*r, -r)), c.Add(f32.Pt(-r, -k*r)), c.Add(f32.Pt(-r, 0)))
		p.CubeTo(c.Add(f32.Pt(-r, k*r)), c.Add(f32.Pt(-k*r, r)), c.Add(f32.Pt(0, r)))
		p.CubeTo(c.Add(f32.Pt(k*r, r)), c.Add(f32.Pt(r, k*r)), c.Add(f32.Pt(r, 0)))
		p.Close()
		paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
	}
	P := f32.Pt
	// Barrel with a highlight.
	poly(penBody, P(0.34, -0.1), P(0.66, -0.1), P(0.66, 0.40), P(0.34, 0.40))
	poly(penShine, P(0.40, -0.1), P(0.46, -0.1), P(0.46, 0.40), P(0.40, 0.40))

	switch st {
	case ink.Fountain:
		poly(penGrip, P(0.35, 0.40), P(0.65, 0.40), P(0.62, 0.55), P(0.38, 0.55))
		// Pointed nib with shoulders, a breather hole and a slit.
		poly(goldDark, P(0.37, 0.53), P(0.63, 0.53), P(0.61, 0.66), P(0.51, 0.97), P(0.49, 0.97), P(0.39, 0.66))
		poly(gold, P(0.39, 0.54), P(0.61, 0.54), P(0.595, 0.66), P(0.505, 0.94), P(0.495, 0.94), P(0.405, 0.66))
		dot(goldDark, P(0.5, 0.68), 0.028)
		line(goldDark, 0.012, P(0.5, 0.70), P(0.5, 0.95))
		dot(inkCol, P(0.5, 0.97), 0.022)
	case ink.Ballpoint:
		// Ridged grip, conical metal tip, a ball of ink.
		poly(silverDim, P(0.35, 0.40), P(0.65, 0.40), P(0.64, 0.56), P(0.36, 0.56))
		for _, y := range []float32{0.44, 0.48, 0.52} {
			line(penGrip, 0.012, P(0.36, y), P(0.64, y))
		}
		poly(silver, P(0.37, 0.56), P(0.63, 0.56), P(0.53, 0.92), P(0.47, 0.92))
		poly(silverDim, P(0.50, 0.56), P(0.63, 0.56), P(0.53, 0.92), P(0.50, 0.92))
		dot(inkCol, P(0.5, 0.94), 0.035)
	case ink.Brush:
		// Metal ferrule and a full, tapering body of inked bristles.
		poly(silver, P(0.35, 0.40), P(0.65, 0.40), P(0.63, 0.56), P(0.37, 0.56))
		line(silverDim, 0.015, P(0.36, 0.47), P(0.64, 0.47))
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(P(0.37, 0.56))
		p.CubeTo(P(0.28, 0.68), P(0.36, 0.85), P(0.5, 0.99))
		p.CubeTo(P(0.64, 0.85), P(0.72, 0.68), P(0.63, 0.56))
		p.Close()
		paint.FillShape(gtx.Ops, inkCol, clip.Outline{Path: p.End()}.Op())
		line(color.NRGBA{0xff, 0xff, 0xff, 0x40}, 0.02, P(0.44, 0.6), P(0.49, 0.9))
	case ink.Calligraphy:
		poly(penGrip, P(0.35, 0.40), P(0.65, 0.40), P(0.62, 0.55), P(0.38, 0.55))
		// Broad nib ending in a flat, chisel edge.
		poly(goldDark, P(0.37, 0.53), P(0.63, 0.53), P(0.62, 0.70), P(0.58, 0.95), P(0.42, 0.95), P(0.38, 0.70))
		poly(gold, P(0.39, 0.54), P(0.61, 0.54), P(0.60, 0.70), P(0.565, 0.93), P(0.435, 0.93), P(0.40, 0.70))
		dot(goldDark, P(0.5, 0.67), 0.025)
		line(goldDark, 0.012, P(0.5, 0.69), P(0.5, 0.93))
		line(inkCol, 0.04, P(0.42, 0.955), P(0.58, 0.955))
	}
}
