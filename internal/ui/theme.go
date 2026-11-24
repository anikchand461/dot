package ui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"

	"dot/internal/canvas"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Colours. Accent is shared with the canvas selection chrome.
var (
	cSurface  = color.NRGBA{0x22, 0x23, 0x29, 0xff}
	cSurface2 = color.NRGBA{0x2e, 0x2f, 0x38, 0xff}
	cBorder   = color.NRGBA{0x3a, 0x3b, 0x45, 0xff}
	cText     = color.NRGBA{0xec, 0xec, 0xf1, 0xff}
	cMuted    = color.NRGBA{0x9a, 0x9b, 0xa8, 0xff}
	cAccent   = canvas.Accent
	cShadow   = color.NRGBA{0, 0, 0, 0x55}
	cWhite    = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	cDanger   = color.NRGBA{0xff, 0x5c, 0x6c, 0xff}
)

func mustIcon(data []byte) *widget.Icon {
	ic, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return ic
}

// glyph draws a sz×sz pictogram in col.
type glyph func(gtx C, sz int, col color.NRGBA)

func iconGlyph(data []byte) glyph {
	ic := mustIcon(data)
	return func(gtx C, sz int, col color.NRGBA) {
		gtx.Constraints = layout.Exact(image.Pt(sz, sz))
		ic.Layout(gtx, col)
	}
}

// eraserGlyph: material icons have no eraser, so draw one: a tilted
// two-tone block.
func eraserGlyph(gtx C, sz int, col color.NRGBA) {
	s := float32(sz)
	defer op.Affine(f32.AffineId().Rotate(f32.Pt(s/2, s/2), -0.75)).Push(gtx.Ops).Pop()
	w, h := s*0.82, s*0.42
	x0, y0 := (s-w)/2, (s-h)/2
	r := int(s * 0.08)
	body := image.Rect(int(x0), int(y0), int(x0+w), int(y0+h))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: clip.UniformRRect(body, r).Path(gtx.Ops), Width: s * 0.09}.Op())
	tip := image.Rect(int(x0), int(y0), int(x0+w*0.42), int(y0+h))
	paint.FillShape(gtx.Ops, col, clip.UniformRRect(tip, r).Op(gtx.Ops))
}

// lassoGlyph: a dashed loop with a little tail, the familiar "lasso select".
func lassoGlyph(gtx C, sz int, col color.NRGBA) {
	s := float32(sz)
	c := f32.Pt(s*0.54, s*0.40)
	rx, ry := s*0.40, s*0.28
	const n = 24
	var p clip.Path
	p.Begin(gtx.Ops)
	for i := 0; i < n; i += 2 { // every other step: dashes
		a0 := 2 * math.Pi * float64(i) / n
		a1 := 2 * math.Pi * float64(i+1) / n
		p.MoveTo(c.Add(f32.Pt(rx*float32(math.Cos(a0)), ry*float32(math.Sin(a0)))))
		p.LineTo(c.Add(f32.Pt(rx*float32(math.Cos(a1)), ry*float32(math.Sin(a1)))))
	}
	// Tail from the bottom-left of the loop.
	start := c.Add(f32.Pt(rx*float32(math.Cos(2.4)), ry*float32(math.Sin(2.4))))
	p.MoveTo(start)
	p.QuadTo(f32.Pt(s*0.12, s*0.80), f32.Pt(s*0.30, s*0.95))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: s * 0.09}.Op())
}

// shapesGlyph: a circle, square and triangle — reads as "shapes".
func shapesGlyph(gtx C, sz int, col color.NRGBA) {
	s := float32(sz)
	w := s * 0.085
	var p clip.Path
	// circle (top-left)
	p.Begin(gtx.Ops)
	c, r := f32.Pt(s*0.30, s*0.30), s*0.21
	const k = 0.5522847
	p.MoveTo(c.Add(f32.Pt(r, 0)))
	p.CubeTo(c.Add(f32.Pt(r, -k*r)), c.Add(f32.Pt(k*r, -r)), c.Add(f32.Pt(0, -r)))
	p.CubeTo(c.Add(f32.Pt(-k*r, -r)), c.Add(f32.Pt(-r, -k*r)), c.Add(f32.Pt(-r, 0)))
	p.CubeTo(c.Add(f32.Pt(-r, k*r)), c.Add(f32.Pt(-k*r, r)), c.Add(f32.Pt(0, r)))
	p.CubeTo(c.Add(f32.Pt(k*r, r)), c.Add(f32.Pt(r, k*r)), c.Add(f32.Pt(r, 0)))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: w}.Op())
	// square (bottom-right, filled)
	sq := image.Rect(int(s*0.52), int(s*0.52), int(s*0.92), int(s*0.92))
	paint.FillShape(gtx.Ops, col, clip.UniformRRect(sq, int(s*0.05)).Op(gtx.Ops))
	// triangle (top-right)
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(s*0.75, s*0.08))
	p.LineTo(f32.Pt(s*0.95, s*0.42))
	p.LineTo(f32.Pt(s*0.55, s*0.42))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: w}.Op())
}

// laserGlyph: a glowing dot.
func laserGlyph(gtx C, sz int, col color.NRGBA) {
	c := sz / 2
	for i, a := range []uint8{0x30, 0x60} {
		r := sz/2 - i*sz/6
		paint.FillShape(gtx.Ops, color.NRGBA{0xff, 0x40, 0x50, a}, clip.Ellipse(image.Rect(c-r, c-r, c+r, c+r)).Op(gtx.Ops))
	}
	r := sz / 5
	paint.FillShape(gtx.Ops, col, clip.Ellipse(image.Rect(c-r, c-r, c+r, c+r)).Op(gtx.Ops))
}

var (
	gLaser     = glyph(laserGlyph)
	gCrop      = iconGlyph(icons.ImageCrop)
	gFill      = iconGlyph(icons.EditorFormatColorFill)
	gWindow    = iconGlyph(icons.ActionOpenInNew)
	gHome      = iconGlyph(icons.ActionHome)
	gSearch    = iconGlyph(icons.ActionSearch)
	gRename    = iconGlyph(icons.EditorModeEdit)
	gChevron   = iconGlyph(icons.NavigationExpandMore)
	gImage     = iconGlyph(icons.EditorInsertPhoto)
	gPDF       = iconGlyph(icons.ImagePictureAsPDF)
	gSelect    = glyph(lassoGlyph)
	gPen       = iconGlyph(icons.ContentCreate)
	gHighlight = iconGlyph(icons.EditorHighlight)
	gEraser    = glyph(eraserGlyph)
	gShape     = glyph(shapesGlyph)
	gText      = iconGlyph(icons.EditorTextFields)
	gHand      = iconGlyph(icons.ActionPanTool)
	gUndo      = iconGlyph(icons.ContentUndo)
	gRedo      = iconGlyph(icons.ContentRedo)
	gMenu      = iconGlyph(icons.NavigationMenu)
	gPages     = iconGlyph(icons.ActionViewAgenda)
	gSetup     = iconGlyph(icons.ActionDescription)
	gAdd       = iconGlyph(icons.ContentAdd)
	gDelete    = iconGlyph(icons.ActionDelete)
	gCopy      = iconGlyph(icons.ContentContentCopy)
	gUp        = iconGlyph(icons.NavigationArrowUpward)
	gDown      = iconGlyph(icons.NavigationArrowDownward)
	gZoomIn    = iconGlyph(icons.ContentAdd)
	gZoomOut   = iconGlyph(icons.ContentRemoveCircleOutline)
	gFit       = iconGlyph(icons.ActionAspectRatio)
	gClose     = iconGlyph(icons.NavigationClose)
	gExport    = iconGlyph(icons.FileFileDownload)
	gHelp      = iconGlyph(icons.ActionHelpOutline)
)

// iconBtn is a square, rounded icon button with an optional tooltip.
func (a *App) iconBtn(gtx C, c *widget.Clickable, g glyph, active bool, tip string) D {
	sz := gtx.Dp(36)
	return c.Layout(gtx, func(gtx C) D {
		rect := image.Rectangle{Max: image.Pt(sz, sz)}
		switch {
		case active:
			paint.FillShape(gtx.Ops, cAccent, clip.UniformRRect(rect, gtx.Dp(9)).Op(gtx.Ops))
		case c.Hovered():
			paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(rect, gtx.Dp(9)).Op(gtx.Ops))
		}
		col := cText
		if active {
			col = cWhite
		}
		isz := gtx.Dp(20)
		off := (sz - isz) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		g(gtx, isz, col)
		t.Pop()
		if tip != "" && c.Hovered() {
			a.tooltip(gtx, tip, image.Pt(sz/2, sz+gtx.Dp(6)))
		}
		return D{Size: rect.Max}
	})
}

// tooltip draws a label centred under anchor, above everything else.
func (a *App) tooltip(gtx C, text string, anchor image.Point) {
	m := op.Record(gtx.Ops)
	l := material.Body2(a.th, text)
	l.Color = cText
	l.TextSize = unit.Sp(12)
	mm := op.Record(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	dims := layout.UniformInset(6).Layout(gtx, l.Layout)
	call := mm.Stop()
	x := anchor.X - dims.Size.X/2
	t := op.Offset(image.Pt(x, anchor.Y)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, color.NRGBA{0x0c, 0x0c, 0x10, 0xf0}, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(6)).Op(gtx.Ops))
	call.Add(gtx.Ops)
	t.Pop()
	op.Defer(gtx.Ops, m.Stop())
}

// chip is a pill-shaped text toggle.
func (a *App) chip(gtx C, c *widget.Clickable, label string, active bool) D {
	return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			l := material.Body2(a.th, label)
			l.TextSize = unit.Sp(13)
			l.Color = cText
			bg := cSurface2
			if active {
				bg, l.Color = cAccent, cWhite
			} else if c.Hovered() {
				bg = cBorder
			}
			m := op.Record(gtx.Ops)
			gtx.Constraints.Min = image.Point{}
			dims := layout.Inset{Top: 5, Bottom: 5, Left: 11, Right: 11}.Layout(gtx, l.Layout)
			call := m.Stop()
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: dims.Size}, dims.Size.Y/2).Op(gtx.Ops))
			call.Add(gtx.Ops)
			return dims
		})
	})
}

// swatch is a colour dot; active ones get an accent ring.
func (a *App) swatch(gtx C, c *widget.Clickable, col color.NRGBA, active bool) D {
	return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			d := gtx.Dp(26)
			r := image.Rectangle{Max: image.Pt(d, d)}
			if active {
				paint.FillShape(gtx.Ops, cAccent, clip.Ellipse(r).Op(gtx.Ops))
				paint.FillShape(gtx.Ops, cSurface, clip.Ellipse(r.Inset(gtx.Dp(2))).Op(gtx.Ops))
			}
			fill := col
			fill.A = 0xff
			paint.FillShape(gtx.Ops, cBorder, clip.Ellipse(r.Inset(gtx.Dp(4)-1)).Op(gtx.Ops))
			paint.FillShape(gtx.Ops, fill, clip.Ellipse(r.Inset(gtx.Dp(4))).Op(gtx.Ops))
			return D{Size: r.Max}
		})
	})
}

// uiBlock is the tag of every floating surface's background. Controls on
// a surface sit above it; the background swallows clicks so they never
// fall through and draw on the page underneath.
var uiBlock = new(int)

func block(gtx C, r image.Rectangle, radius int) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, uiBlock)
	pointer.CursorDefault.Add(gtx.Ops)
}

// drainBlocker consumes the events the backgrounds swallowed.
func drainBlocker(gtx C) {
	big := pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: uiBlock,
			Kinds:   pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
			ScrollX: big, ScrollY: big}); !ok {
			return
		}
	}
}

// surface paints a floating rounded panel of size r with shadow and border.
func surface(gtx C, r image.Rectangle, rad int) {
	paint.FillShape(gtx.Ops, cShadow, clip.UniformRRect(r.Add(image.Pt(0, gtx.Dp(3))), rad).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cSurface, clip.UniformRRect(r, rad).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cBorder, clip.Stroke{Path: clip.UniformRRect(r, rad).Path(gtx.Ops), Width: 1}.Op())
	block(gtx, r, rad)
}

// card draws w on a rounded surface with a soft shadow.
func card(gtx C, w layout.Widget) D {
	gtx.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	dims := layout.UniformInset(14).Layout(gtx, w)
	call := m.Stop()
	surface(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(14))
	call.Add(gtx.Ops)
	return dims
}

// pill is a compact floating bar (zoom, page indicator).
func pill(gtx C, w layout.Widget) D {
	gtx.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 4, Bottom: 4, Left: 6, Right: 6}.Layout(gtx, w)
	call := m.Stop()
	surface(gtx, image.Rectangle{Max: dims.Size}, dims.Size.Y/2)
	call.Add(gtx.Ops)
	return dims
}

func (a *App) title(gtx C, s string) D {
	l := material.Body2(a.th, s)
	l.Color = cMuted
	l.TextSize = unit.Sp(11.5)
	l.Font.Weight = font.SemiBold
	return layout.Inset{Top: 8, Bottom: 6}.Layout(gtx, l.Layout)
}

func (a *App) text(gtx C, s string, col color.NRGBA, size float32) D {
	l := material.Body2(a.th, s)
	l.Color = col
	l.TextSize = unit.Sp(size)
	return l.Layout(gtx)
}

func (a *App) slider(gtx C, f *widget.Float, label, value string) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return a.text(gtx, label, cText, 13) }),
				layout.Rigid(func(gtx C) D { return a.text(gtx, value, cMuted, 13) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X // sliders size to Min
			s := material.Slider(a.th, f)
			s.Color = cAccent
			return s.Layout(gtx)
		}),
	)
}

func (a *App) toggle(gtx C, b *widget.Bool, label string) D {
	return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D { return a.text(gtx, label, cText, 13) }),
			layout.Rigid(func(gtx C) D {
				s := material.Switch(a.th, b, label)
				s.Color.Enabled = cAccent
				s.Color.Track = cBorder
				return s.Layout(gtx)
			}),
		)
	})
}

func vsep(gtx C) D {
	return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
		sz := image.Pt(gtx.Dp(1), gtx.Dp(24))
		paint.FillShape(gtx.Ops, cBorder, clip.Rect{Max: sz}.Op())
		return D{Size: sz}
	})
}

// flow lays widgets out left to right, wrapping to new rows.
func flow(gtx C, ws ...layout.Widget) D {
	maxW := gtx.Constraints.Max.X
	var x, y, rowH, width int
	m := op.Record(gtx.Ops)
	for _, w := range ws {
		cg := gtx
		cg.Constraints.Min = image.Point{}
		mm := op.Record(gtx.Ops)
		d := w(cg)
		call := mm.Stop()
		if x > 0 && x+d.Size.X > maxW {
			x, y, rowH = 0, y+rowH, 0
		}
		t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		x += d.Size.X
		rowH = max(rowH, d.Size.Y)
		width = max(width, x)
	}
	m.Stop().Add(gtx.Ops)
	return D{Size: image.Pt(width, y+rowH)}
}

var (
	iconsFolder = icons.FileFolderOpen
	iconsSave   = icons.ContentSave
)
