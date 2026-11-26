package canvas

import (
	"image"
	"image/color"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"dot/internal/document"
)

// All drawing here is in page-local pt. Callers apply a zoom transform, so
// recorded ops stay valid at every zoom level (vector paths rescale on the GPU).

// DrawPage records paper, ruling, images, text and ink for p. Items in
// skip (a selection being dragged) are left out; the canvas draws them live.
func DrawPage(ops *op.Ops, p *document.Page, skip map[document.Item]bool) {
	DrawPageWith(ops, p, skip, Shaper())
}

// DrawPageWith draws with a given text shaper. A text.Shaper is not safe
// for concurrent use, so background renderers (thumbnails) pass their own.
func DrawPageWith(ops *op.Ops, p *document.Page, skip map[document.Item]bool, sh *text.Shaper) {
	DrawPageLOD(ops, p, skip, sh, 0)
}

// DrawPageLOD draws with a level of detail: stroke points closer than
// minSeg pt are merged. Pages shown small (zoomed out, thumbnails) don't
// need detail finer than a pixel, and skipping it saves memory and GPU.
func DrawPageLOD(ops *op.Ops, p *document.Page, skip map[document.Item]bool, sh *text.Shaper, minSeg float32) {
	DrawBackground(ops, p)
	defer clip.Outline{Path: rectPath(ops, 0, 0, p.Width, p.Height)}.Op().Push(ops).Pop()
	for _, l := range p.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			if !skip[it] {
				drawItem(ops, it, sh, minSeg)
			}
		}
	}
}

func DrawItem(ops *op.Ops, it document.Item) { drawItem(ops, it, Shaper(), 0) }

// DrawBackground draws the paper: colour, ruling, PDF page or picture.
func DrawBackground(ops *op.Ops, p *document.Page) {
	page := rectPath(ops, 0, 0, p.Width, p.Height)
	paint.FillShape(ops, p.Background.Color, clip.Outline{Path: page}.Op())
	defer clip.Outline{Path: rectPath(ops, 0, 0, p.Width, p.Height)}.Op().Push(ops).Pop()
	switch p.Background.Type {
	case "solid":
		drawRuling(ops, p)
	case "pdf":
		drawPDFBackground(ops, p)
	case "pixmap":
		drawImageBackground(ops, p)
	}
}

func drawItem(ops *op.Ops, it document.Item, sh *text.Shaper, minSeg float32) {
	switch it := it.(type) {
	case *document.Stroke:
		drawStroke(ops, it, minSeg)
	case *document.RawElement:
		switch it.Name {
		case "image":
			drawImage(ops, it)
		case "text":
			drawText(ops, it, sh)
		}
	}
}

var (
	shaperOnce sync.Once
	shaper     *text.Shaper
)

func Shaper() *text.Shaper {
	shaperOnce.Do(func() { shaper = text.NewShaper(text.WithCollection(collection())) })
	return shaper
}

// DrawText renders a Xournal++ <text> element. A layout context with
// 1 px per sp makes the font size come out in pt.
func DrawText(ops *op.Ops, e *document.RawElement) { drawText(ops, e, Shaper()) }

func drawText(ops *op.Ops, e *document.RawElement, sh *text.Shaper) {
	size := e.Float("size")
	if size <= 0 {
		size = 12
	}
	defer op.Affine(f32.AffineId().Offset(f32.Pt(e.Float("x"), e.Float("y")))).Push(ops).Pop()
	gtx := layout.Context{
		Ops:         ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(1<<20, 1<<20)},
	}
	m := op.Record(ops)
	paint.ColorOp{Color: e.Color()}.Add(ops)
	col := m.Stop()
	fam, bold, italic := ParseFont(e.Attr("font"))
	lbl := widget.Label{LineHeightScale: 1.15}
	if w := e.Float("inkWidth"); w > 0 { // a label wrapped and centred in a shape
		gtx.Constraints = layout.Exact(image.Pt(int(w), 1<<20))
		gtx.Constraints.Min.Y = 0
		lbl.Alignment = text.Middle
	}
	lbl.Layout(gtx, sh, GioFont(fam, bold, italic), unit.Sp(size), e.Text(), col)
}

func rectPath(ops *op.Ops, x0, y0, x1, y1 float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(x0, y0))
	p.LineTo(f32.Pt(x1, y0))
	p.LineTo(f32.Pt(x1, y1))
	p.LineTo(f32.Pt(x0, y1))
	p.Close()
	return p.End()
}

func drawRuling(ops *op.Ops, p *document.Page) {
	r := p.Ruling()
	lines := func(segs [][2]f32.Point, col color.NRGBA) {
		if len(segs) == 0 {
			return
		}
		var path clip.Path
		path.Begin(ops)
		for _, s := range segs {
			path.MoveTo(s[0])
			path.LineTo(s[1])
		}
		paint.FillShape(ops, col, clip.Stroke{Path: path.End(), Width: document.RuleWidth}.Op())
	}
	lines(r.Lines, r.LineColor)
	lines(r.Margin, document.MarginColor)
	if len(r.Dots) > 0 {
		var path clip.Path
		path.Begin(ops)
		for _, d := range r.Dots {
			circle(&path, d, 0.75)
		}
		paint.FillShape(ops, r.DotColor, clip.Outline{Path: path.End()}.Op())
	}
}

// DrawStroke fills a variable-width stroke as a few closed outlines
// ("chunks"): left edge forward, right edge back. A chunk ends whenever the
// stroke has turned ~90° (or after chunkPoints points), and a round joint covers
// each seam; the stroke ends get round caps.
//
// Why chunks: Gio accumulates path coverage in limited precision, and a
// single outline that crosses itself many times (loops in handwriting)
// leaves faint vertical streaks below the stroke. Chunks that barely
// turn don't self-intersect, so coverage cancels exactly. They are also
// far cheaper than a disc per point. All pieces share one winding
// direction, so with the non-zero rule they merge seamlessly and
// translucent highlighters stay even.
func DrawStroke(ops *op.Ops, s *document.Stroke) { drawStroke(ops, s, 0) }

func drawStroke(ops *op.Ops, s *document.Stroke, minSeg float32) {
	if s.Fill.A > 0 && len(s.Points) > 2 {
		fillPoly(ops, s.Points, s.Fill)
	}
	for _, pc := range s.Pieces() {
		if pc.Dot {
			var path clip.Path
			path.Begin(ops)
			circle(&path, pc.Points[0], pc.Widths[0]/2)
			paint.FillShape(ops, s.Color, clip.Outline{Path: path.End()}.Op())
			continue
		}
		drawSolid(ops, &document.Stroke{Points: pc.Points, Widths: pc.Widths, Width: s.Width, Color: s.Color}, minSeg)
	}
}

// fillPoly paints the inside of a (closed or implicitly closed) outline.
func fillPoly(ops *op.Ops, pts []f32.Point, c color.NRGBA) {
	var path clip.Path
	path.Begin(ops)
	path.MoveTo(pts[0])
	for _, p := range pts[1:] {
		path.LineTo(p)
	}
	path.Close()
	paint.FillShape(ops, c, clip.Outline{Path: path.End()}.Op())
}

func drawSolid(ops *op.Ops, s *document.Stroke, minSeg float32) {
	if len(s.Points) == 0 {
		return
	}
	var path clip.Path
	path.Begin(ops)
	solidOutline(&path, s, minSeg)
	paint.FillShape(ops, s.Color, clip.Outline{Path: path.End()}.Op())
}

// Pather receives an outline; *clip.Path is one.
type Pather interface {
	MoveTo(to f32.Point)
	LineTo(to f32.Point)
	CubeTo(ctrl0, ctrl1, to f32.Point)
	Close()
}

// StrokeOutline traces s as the closed outlines the screen fills: filled
// as one shape with the non-zero rule, every spot is painted once, so a
// translucent highlighter stays even where it crosses itself.
func StrokeOutline(p Pather, s *document.Stroke) {
	for _, pc := range s.Pieces() {
		if pc.Dot {
			circle(p, pc.Points[0], pc.Widths[0]/2)
			continue
		}
		solidOutline(p, &document.Stroke{Points: pc.Points, Widths: pc.Widths, Width: s.Width}, 0)
	}
}

// solidOutline traces one solid piece.
func solidOutline(path Pather, s *document.Stroke, minSeg float32) {
	pts, ws := renderPoints(s, minSeg)
	n := len(pts)
	if n == 0 {
		return
	}
	if n == 1 {
		circle(path, pts[0], ws[0]/2)
		return
	}
	start := 0
	var turned float64
	for i := 1; i < n; i++ {
		if i < n-1 {
			a, b := pts[i].Sub(pts[i-1]), pts[i+1].Sub(pts[i])
			turned += math.Abs(math.Atan2(float64(a.X*b.Y-a.Y*b.X), float64(a.X*b.X+a.Y*b.Y)))
		}
		if i == n-1 || turned > math.Pi/2 || i-start >= chunkPoints {
			chunk(path, pts[start:i+1], ws[start:i+1], start == 0, i == n-1)
			if i < n-1 {
				circle(path, pts[i], ws[i]/2) // round joint over the seam
			}
			start, turned = i, 0
		}
	}
}

// chunk appends one closed outline around pts; first/last get round caps,
// inner ends are cut square (the caller covers them with a joint disc).
func chunk(path Pather, pts []f32.Point, ws []float32, first, last bool) {
	n := len(pts)
	nrm := make([]f32.Point, n)
	for i := range pts {
		d := pts[min(i+1, n-1)].Sub(pts[max(i-1, 0)])
		l := float32(math.Hypot(float64(d.X), float64(d.Y)))
		if l > 0 {
			nrm[i] = f32.Pt(-d.Y/l, d.X/l)
		} else if i > 0 {
			nrm[i] = nrm[i-1]
		}
	}
	path.MoveTo(pts[0].Add(nrm[0].Mul(ws[0] / 2)))
	for i := 1; i < n; i++ {
		path.LineTo(pts[i].Add(nrm[i].Mul(ws[i] / 2)))
	}
	if last {
		halfCap(path, pts[n-1], nrm[n-1], ws[n-1]/2, true)
	} else {
		path.LineTo(pts[n-1].Sub(nrm[n-1].Mul(ws[n-1] / 2)))
	}
	for i := n - 2; i >= 0; i-- {
		path.LineTo(pts[i].Sub(nrm[i].Mul(ws[i] / 2)))
	}
	if first {
		halfCap(path, pts[0], nrm[0], ws[0]/2, false)
	}
	path.Close()
}

// halfCap draws a semicircle around c from the current point (c ± n·r)
// to the opposite side, bulging forward (end cap) or backward (start cap).
func halfCap(p Pather, c, n f32.Point, r float32, end bool) {
	const k = 0.5522847
	fwd := f32.Pt(n.Y, -n.X) // direction of travel
	if !end {
		fwd = fwd.Mul(-1)
		n = n.Mul(-1)
	}
	// From c+n·r round the front to c−n·r.
	tip := c.Add(fwd.Mul(r))
	p.CubeTo(c.Add(n.Mul(r)).Add(fwd.Mul(k*r)), tip.Add(n.Mul(k*r)), tip)
	p.CubeTo(tip.Sub(n.Mul(k*r)), c.Sub(n.Mul(r)).Add(fwd.Mul(k*r)), c.Sub(n.Mul(r)))
}

// renderPoints drops points closer than a fraction of the stroke width:
// invisible detail that only costs GPU time. Stored data is untouched.
func renderPoints(s *document.Stroke, minSeg float32) ([]f32.Point, []float32) {
	if len(s.Points) <= 2 {
		ws := make([]float32, len(s.Points))
		for i := range ws {
			ws[i] = s.WidthAt(i)
		}
		return s.Points, ws
	}
	pts := make([]f32.Point, 0, len(s.Points))
	ws := make([]float32, 0, len(s.Points))
	pts, ws = append(pts, s.Points[0]), append(ws, s.WidthAt(0))
	last := len(s.Points) - 1
	for i := 1; i < last; i++ {
		d := s.Points[i].Sub(pts[len(pts)-1])
		min := max(0.25, s.WidthAt(i)*0.3, minSeg)
		if d.X*d.X+d.Y*d.Y >= min*min {
			pts, ws = append(pts, s.Points[i]), append(ws, s.WidthAt(i))
		}
	}
	return append(pts, s.Points[last]), append(ws, s.WidthAt(last))
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// circle appends a circle traced in the same direction as the segment quads.
func circle(p Pather, c f32.Point, r float32) {
	const k = 0.5522847 // cubic Bézier circle constant
	kr := k * r
	p.MoveTo(c.Add(f32.Pt(r, 0)))
	p.CubeTo(c.Add(f32.Pt(r, -kr)), c.Add(f32.Pt(kr, -r)), c.Add(f32.Pt(0, -r)))
	p.CubeTo(c.Add(f32.Pt(-kr, -r)), c.Add(f32.Pt(-r, -kr)), c.Add(f32.Pt(-r, 0)))
	p.CubeTo(c.Add(f32.Pt(-r, kr)), c.Add(f32.Pt(-kr, r)), c.Add(f32.Pt(0, r)))
	p.CubeTo(c.Add(f32.Pt(kr, r)), c.Add(f32.Pt(r, kr)), c.Add(f32.Pt(r, 0)))
	p.Close()
}

// drawImage renders an <image> element kept from a Xournal++ file.
func drawImage(ops *op.Ops, e *document.RawElement) {
	img := e.Image()
	if img == nil {
		return
	}
	left, top, right, bottom := e.Float("left"), e.Float("top"), e.Float("right"), e.Float("bottom")
	b := img.Bounds()
	if right <= left || bottom <= top || b.Dx() == 0 || b.Dy() == 0 {
		return
	}
	sx, sy := (right-left)/float32(b.Dx()), (bottom-top)/float32(b.Dy())
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sx, sy)).Offset(f32.Pt(left, top))).Push(ops).Pop()
	imageMu.Lock()
	defer imageMu.Unlock()
	imgOp, ok := imageOps[e]
	if !ok {
		// Reuse the ImageOp so the GPU texture is uploaded once, not on
		// every page re-record.
		imgOp = paint.NewImageOp(img)
		imageOps[e] = imgOp
	}
	imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)
}

var (
	imageMu  sync.Mutex // thumbnails render on another goroutine
	imageOps = map[*document.RawElement]paint.ImageOp{}
)

// NewShaper makes a separate text shaper with dot!'s fonts.
func NewShaper() *text.Shaper { return text.NewShaper(text.WithCollection(collection())) }

// ResetImageCache drops decoded image textures, e.g. when a document closes.
func ResetImageCache() {
	imageMu.Lock()
	clear(imageOps)
	imageMu.Unlock()
}

func sqrt(v float64) float64 { return math.Sqrt(v) }

// chunkPoints caps a stroke outline piece. Gio accumulates path coverage
// in limited precision; long outline edges leave faint vertical streaks
// below thin strokes (measured: visible at 12+ points, gone at ≤7).
const chunkPoints = 7
