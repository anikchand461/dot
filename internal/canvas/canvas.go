// Package canvas is the zoomable, continuously scrolling multi-page view
// and the tools that act on it.
package canvas

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/document"
	"dot/internal/ink"
	"dot/internal/input"
)

type Tool int

const (
	Select Tool = iota
	Pen
	Highlighter
	Eraser
	Shape
	Text
	Laser
	Hand
	Fill // paint bucket: tap inside a shape or loop to fill it
)

type EraserMode int

const (
	EraseStroke  EraserMode = iota // whole strokes
	ErasePartial                   // only what the eraser touches
)

const (
	pageGap   = 24 // pt between pages
	marginDp  = 32 // dp around the page column
	minZoom   = 0.1
	maxZoom   = 8
	cacheKeep = 180 // frames an off-screen page keeps its recorded ops
	holdSnap  = 550 * time.Millisecond
)

var (
	DeskColor   = color.NRGBA{0x17, 0x18, 0x1c, 0xff}
	shadowColor = color.NRGBA{0, 0, 0, 0x60}
	Accent      = color.NRGBA{0x8b, 0x6c, 0xff, 0xff}
)

// pageCache holds a page's recorded drawing: the paper, plus the ink in
// chunks of consecutive items (in document order, so stacking is kept).
// Each chunk is its own op list: an edit re-records only the chunk it
// touches, other chunks stay cached on the GPU, and chunks whose bounds
// are off screen aren't drawn at all (a big win when zoomed in).
type pageCache struct {
	bg       op.Ops
	bgCall   op.CallOp
	bgKey    string
	bgVer    uint64 // PDF/picture backgrounds re-render: follow Version too
	chunks   []*inkChunk
	version  uint64
	valid    bool
	lastUsed uint64
	lod      float32 // detail level the ink was recorded at
	full     op.Ops  // everything, for thumbnails
	fullCall op.CallOp
	fullVer  uint64
}

type inkChunk struct {
	items  []document.Item
	ops    op.Ops
	call   op.CallOp
	bounds document.Rect
}

const chunkItems = 48 // items per chunk: small enough to re-record fast

// lodFor picks a detail level (minimum point spacing, pt) for a zoom of
// scale px/pt: none at normal zoom, coarser as pages get small. Steps
// (not a continuous value) so zooming doesn't re-record every frame.
func lodFor(scale float32) float32 {
	switch {
	case scale >= 1:
		return 0
	case scale >= 0.5:
		return 1.2
	case scale >= 0.25:
		return 2.4
	}
	return 4.8
}

type Canvas struct {
	Doc     *document.Document
	History *document.History
	Input   *input.Provider
	Theme   *material.Theme

	Tool             Tool
	PenColor         color.NRGBA
	PenWidth         float32 // pt
	PenStyle         ink.Style
	Ink              ink.Config
	HighlighterColor color.NRGBA
	HighlighterWidth float32
	EraserRadius     float32
	EraserMode       EraserMode
	ShapeKind        ink.Shape
	// LineStyle is the pen/shape pattern: "", "dash", "dot", "dashdot".
	LineStyle string
	// FillColor fills new shapes and is what the Fill tool pours;
	// A == 0 means "no fill" (the Fill tool then removes fills).
	FillColor  color.NRGBA
	TextSize   float32
	TextFont   string // "Sans", "Serif", "Mono"
	TextBold   bool
	TextItalic bool
	// SnapShapes turns a stroke into a clean shape when the pen is held
	// still at the end ("draw and hold").
	SnapShapes bool
	// SpacePan is set while the space bar is held: drag pans.
	SpacePan bool

	// Wake schedules a frame from another goroutine (window.Invalidate).
	Wake func()
	// OnScrollPastEnd is called when the user keeps scrolling down past the
	// last page (the app adds a new page).
	OnScrollPastEnd func()
	// OnAddPage is called when the + under the last page is clicked.
	OnAddPage  func()
	overscroll float32
	overAt     time.Time // last scroll that pushed past the end
	addCenter  f32.Point // screen px of the + under the last page
	addRadius  float32   // 0 while it is off screen

	// Zoom is dp per pt; 1 shows an A4 page 595dp wide.
	Zoom float32

	view     image.Point
	scale    float32   // px per pt this frame
	off      f32.Point // screen px of the document origin
	pan      f32.Point // px
	tops     []float32 // y of each page in document pt
	contentW float32
	contentH float32
	current  int

	// active gesture
	gesture  gesture
	gPage    *document.Page
	gStart   f32.Point // page pt
	gLast    f32.Point // page pt
	panLast  f32.Point // screen px
	hover    f32.Point // screen px
	hovering bool

	// pen
	engine   ink.Engine
	live     *document.Stroke
	lastMove time.Time
	moveAt   f32.Point
	snapped  bool

	// eraser
	erased document.Batch

	// lasso & selection
	lasso []f32.Point
	sel   *selection
	crop  *cropState
	bend  *bendState

	lastTap   time.Duration
	lastTapAt f32.Point

	// text
	editor   widget.Editor
	editing  *textEdit
	editRect image.Rectangle

	skip      map[document.Item]bool
	clipboard []document.Item

	caches map[*document.Page]*pageCache
	frame  uint64

	// Smooth zoom/scroll animations.
	now        time.Time
	lastFrame  time.Time
	zoomTarget float32
	zoomAnchor f32.Point
	zooming    bool
	panTarget  float32
	scrolling  bool

	// PDF render generations already applied to this canvas's pages.
	pdfSeen map[pdfKey]int

	// Laser pointer strokes (never saved; they fade out).
	lasers []laserStroke
}

type laserStroke struct {
	page *document.Page
	pts  []f32.Point
	end  time.Time // zero while still drawing
}

const (
	laserHold = 500 * time.Millisecond // fully visible after lifting
	laserFade = laserHold + 700*time.Millisecond
)

type gesture int

const (
	gNone gesture = iota
	gPan
	gInk
	gShape
	gErase
	gCrop
	gBend
	gLaser
	gLasso
	gMove
	gScale
)

func New(doc *document.Document, hist *document.History, in *input.Provider, th *material.Theme) *Canvas {
	c := &Canvas{
		Doc: doc, History: hist, Input: in, Theme: th,
		Tool:             Pen,
		PenColor:         color.NRGBA{0x10, 0x10, 0x10, 0xff},
		PenWidth:         1.41,
		Ink:              ink.DefaultConfig(),
		HighlighterColor: color.NRGBA{0xff, 0xe0, 0x00, 0x7f},
		HighlighterWidth: 8.5,
		EraserRadius:     6,
		ShapeKind:        ink.Rect,
		TextSize:         14,
		TextFont:         "Sans",
		SnapShapes:       true,
		Zoom:             1.25,
		caches:           map[*document.Page]*pageCache{},
		skip:             map[document.Item]bool{},
	}
	c.editor.Submit = false
	return c
}

// SetDocument swaps in a freshly loaded or new document.
func (c *Canvas) SetDocument(d *document.Document) {
	c.Doc = d
	c.live, c.erased, c.gesture, c.sel, c.editing, c.crop = nil, nil, gNone, nil, nil, nil
	c.pan = f32.Point{}
	clear(c.caches)
	clear(c.skip)
	ResetImageCache()
	resetRasters()
}

// Busy reports whether a gesture or text edit is in progress (the UI
// suppresses single-key shortcuts then).
func (c *Canvas) Editing() bool { return c.editing != nil }

// Current is the index of the page nearest the middle of the view.
func (c *Canvas) Current() int { return max(0, min(c.current, len(c.Doc.Pages)-1)) }

// clampPanY keeps vertical scrolling within the document.
func (c *Canvas) clampPanY(y, margin, vh float32) float32 {
	return max(-margin, min(max(0, c.contentH*c.scale+2*margin-vh*0.5), y))
}

// ScrollToPage glides to page i. The target is clamped to where the view
// can actually scroll, so the animation always finishes (an unreachable
// target would keep redrawing forever).
func (c *Canvas) ScrollToPage(i int) {
	c.layoutPages() // pages may have been added since this frame's layout
	if i < 0 || i >= len(c.tops) {
		return
	}
	margin := float32(0)
	if c.Zoom > 0 {
		margin = marginDp * c.scale / c.Zoom
	}
	c.panTarget, c.scrolling = c.clampPanY(c.tops[i]*c.scale, margin, float32(c.view.Y)), true
}

func (c *Canvas) center() f32.Point { return f32.Pt(float32(c.view.X)/2, float32(c.view.Y)/2) }

// ZoomBy smoothly zooms around the centre of the view.
func (c *Canvas) ZoomBy(f float32) {
	base := c.Zoom
	if c.zooming {
		base = c.zoomTarget
	}
	c.animateZoom(base*f, c.center())
}

func (c *Canvas) animateZoom(z float32, at f32.Point) {
	c.zoomTarget, c.zoomAnchor, c.zooming = max(minZoom, min(maxZoom, z)), at, true
}

// fitZoom is the zoom at which the widest page fills the view width.
func (c *Canvas) fitZoom() float32 {
	if c.contentW == 0 || c.scale == 0 {
		return c.Zoom
	}
	pxPerDp := c.scale / c.Zoom
	return min(3, (float32(c.view.X)-2*marginDp*pxPerDp)/(c.contentW*pxPerDp))
}

// FitWidth smoothly zooms so the widest page fills the view width.
func (c *Canvas) FitWidth() { c.animateZoom(c.fitZoom(), c.center()) }

// animate advances zoom/scroll animations with frame-rate independent
// easing (~70 ms time constant), so they feel the same at 60 or 120 Hz.
func (c *Canvas) animate(gtx layout.Context) {
	dt := c.now.Sub(c.lastFrame)
	c.lastFrame = c.now
	if dt <= 0 || dt > 100*time.Millisecond {
		dt = 16 * time.Millisecond
	}
	k := float32(1 - math.Exp(-dt.Seconds()/0.07))
	if c.zooming {
		z := c.Zoom + (c.zoomTarget-c.Zoom)*k
		if math.Abs(float64(c.zoomTarget-z)) < 0.002*float64(c.zoomTarget) {
			z, c.zooming = c.zoomTarget, false
		}
		c.zoomAt(c.zoomAnchor, z/c.Zoom)
	}
	if c.scrolling {
		before := c.pan.Y
		c.pan.Y += (c.panTarget - c.pan.Y) * k
		// Done when there, or when no longer moving (belt and braces
		// against any target the clamp keeps out of reach).
		if math.Abs(float64(c.panTarget-c.pan.Y)) < 0.5 || math.Abs(float64(c.pan.Y-before)) < 0.05 {
			c.pan.Y, c.scrolling = c.panTarget, false
		}
	}
	// Trackpad pinch and two-finger double tap (smart zoom).
	z, smart := c.Input.TakeGestures()
	at := c.center()
	if c.hovering {
		at = c.hover
	}
	if z != 1 {
		c.zooming = false
		c.zoomAt(at, z)
	}
	if smart > 0 {
		if fit := c.fitZoom(); c.Zoom > fit*1.15 || c.Zoom < fit*0.85 {
			c.animateZoom(fit, at)
		} else {
			c.animateZoom(fit*2, at)
		}
	}
	if c.zooming || c.scrolling {
		gtx.Execute(op.InvalidateCmd{})
	}
}

// syncPage brings p's cache up to date, re-recording only what changed.
// Off-screen pages drop their caches after cacheKeep frames, so memory
// tracks what is visible rather than document length.
func (c *Canvas) syncPage(p *document.Page) *pageCache {
	pc := c.caches[p]
	if pc == nil {
		pc = new(pageCache)
		c.caches[p] = pc
	}
	pc.lastUsed = c.frame
	lod := lodFor(c.scale)
	if pc.valid && pc.version == p.Version && pc.lod == lod {
		return pc
	}
	if key := bgKey(p); !pc.valid || key != pc.bgKey || p.Background.Type != "solid" && pc.bgVer != p.Version {
		pc.bg.Reset()
		m := op.Record(&pc.bg)
		DrawBackground(&pc.bg, p)
		pc.bgCall, pc.bgKey, pc.bgVer = m.Stop(), key, p.Version
	}
	// The visible items, in drawing order.
	var items []document.Item
	for _, l := range p.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			if !c.skip[it] {
				items = append(items, it)
			}
		}
	}
	// Reuse every old chunk whose items are still there unchanged.
	byStart := map[document.Item]*inkChunk{}
	if pc.valid && pc.lod == lod {
		for _, ch := range pc.chunks {
			byStart[ch.items[0]] = ch
		}
	}
	matches := func(ch *inkChunk, rest []document.Item) bool {
		if ch == nil || len(rest) < len(ch.items) {
			return false
		}
		for k, it := range ch.items {
			if rest[k] != it {
				return false
			}
		}
		return true
	}
	var out []*inkChunk
	for i := 0; i < len(items); {
		if ch := byStart[items[i]]; matches(ch, items[i:]) {
			out = append(out, ch)
			i += len(ch.items)
			continue
		}
		j := i + 1
		for j < len(items) && j-i < chunkItems && !matches(byStart[items[j]], items[j:]) {
			j++
		}
		out = append(out, c.recordChunk(items[i:j], lod))
		i = j
	}
	// Appends make tiny chunks; once there are too many, regroup.
	if len(out) > len(items)/chunkItems*2+8 {
		out = out[:0]
		for i := 0; i < len(items); i += chunkItems {
			out = append(out, c.recordChunk(items[i:min(i+chunkItems, len(items))], lod))
		}
	}
	pc.chunks, pc.version, pc.lod, pc.valid = out, p.Version, lod, true
	return pc
}

// bgKey identifies what the paper looks like.
func bgKey(p *document.Page) string {
	b := p.Background
	return fmt.Sprint(p.Width, p.Height, b.Type, b.Style, b.Color, b.PDFPath, b.PDFPage, b.ImagePath, b.Soften)
}

func (c *Canvas) recordChunk(items []document.Item, lod float32) *inkChunk {
	ch := &inkChunk{items: append([]document.Item(nil), items...)}
	m := op.Record(&ch.ops)
	for k, it := range ch.items {
		drawItem(&ch.ops, it, Shaper(), lod)
		if b, ok := document.ItemBounds(it); ok {
			if s, isStroke := it.(*document.Stroke); isStroke && s.Fill.A > 0 {
				b = s.Bounds()
			}
			if k == 0 {
				ch.bounds = b
			} else {
				ch.bounds = union(ch.bounds, b)
			}
		}
	}
	ch.call = m.Stop()
	return ch
}

func union(a, b document.Rect) document.Rect {
	return document.Rect{Min: f32.Pt(min(a.Min.X, b.Min.X), min(a.Min.Y, b.Min.Y)),
		Max: f32.Pt(max(a.Max.X, b.Max.X), max(a.Max.Y, b.Max.Y))}
}

// drawPageVisible draws p's paper and only the ink chunks overlapping
// view (page pt).
func (c *Canvas) drawPageVisible(ops *op.Ops, p *document.Page, view document.Rect) {
	pc := c.syncPage(p)
	pc.bgCall.Add(ops)
	cl := clip.Outline{Path: rectPath(ops, 0, 0, p.Width, p.Height)}.Op().Push(ops)
	for _, ch := range pc.chunks {
		b := ch.bounds.Expand(4)
		if b.Max.X >= view.Min.X && b.Min.X <= view.Max.X && b.Max.Y >= view.Min.Y && b.Min.Y <= view.Max.Y {
			ch.call.Add(ops)
		}
	}
	cl.Pop()
}

// PageOps returns p's whole drawing in pt units (for thumbnails).
func (c *Canvas) PageOps(p *document.Page) op.CallOp {
	pc := c.syncPage(p)
	if pc.fullVer != pc.version || pc.fullCall == (op.CallOp{}) {
		pc.full.Reset()
		m := op.Record(&pc.full)
		c.drawPageVisible(&pc.full, p, document.Rect{Min: f32.Pt(-1e9, -1e9), Max: f32.Pt(1e9, 1e9)})
		pc.fullCall, pc.fullVer = m.Stop(), pc.version
	}
	return pc.fullCall
}

// appendItem is kept for callers; chunking makes appends cheap already.
func (c *Canvas) appendItem(p *document.Page, it document.Item) {}

func (c *Canvas) layoutPages() {
	c.tops = c.tops[:0]
	var y, w float32
	for _, p := range c.Doc.Pages {
		c.tops = append(c.tops, y)
		y += p.Height + pageGap
		w = max(w, p.Width)
	}
	c.contentW, c.contentH = w, max(0, y-pageGap)
}

func (c *Canvas) updateTransform(gtx layout.Context) {
	c.scale = c.Zoom * gtx.Metric.PxPerDp
	margin := marginDp * gtx.Metric.PxPerDp
	vw, vh := float32(c.view.X), float32(c.view.Y)
	cw := c.contentW * c.scale
	if slack := (cw-vw)/2 + margin; slack > 0 {
		c.pan.X = max(-slack, min(slack, c.pan.X))
	} else {
		c.pan.X = 0
	}
	c.pan.Y = c.clampPanY(c.pan.Y, margin, vh)
	c.off = f32.Pt((vw-cw)/2-c.pan.X, margin*2-c.pan.Y)
}

func (c *Canvas) zoomAt(at f32.Point, f float32) {
	if c.scale == 0 {
		c.Zoom = max(minZoom, min(maxZoom, c.Zoom*f))
		return
	}
	d := c.toDoc(at)
	pxPerDp := c.scale / c.Zoom
	margin := marginDp * pxPerDp
	c.Zoom = max(minZoom, min(maxZoom, c.Zoom*f))
	s := c.Zoom * pxPerDp
	c.pan.X = (float32(c.view.X)-c.contentW*s)/2 - (at.X - d.X*s)
	c.pan.Y = margin*2 - (at.Y - d.Y*s)
	c.scale = s
}

func (c *Canvas) toDoc(p f32.Point) f32.Point { return p.Sub(c.off).Div(c.scale) }

func (c *Canvas) pageIndex(p *document.Page) int {
	for i, q := range c.Doc.Pages {
		if q == p {
			return i
		}
	}
	return -1
}

func (c *Canvas) pageOrigin(i int) f32.Point {
	return f32.Pt((c.contentW-c.Doc.Pages[i].Width)/2, c.tops[i])
}

// pageAt finds the page under a screen point and returns the point in
// that page's coordinates.
func (c *Canvas) pageAt(screen f32.Point) (*document.Page, f32.Point, bool) {
	d := c.toDoc(screen)
	for i, p := range c.Doc.Pages {
		l := d.Sub(c.pageOrigin(i))
		if l.X >= 0 && l.Y >= 0 && l.X <= p.Width && l.Y <= p.Height {
			return p, l, true
		}
	}
	return nil, f32.Point{}, false
}

// local maps a screen point into page p's coordinates.
func (c *Canvas) local(p *document.Page, screen f32.Point) f32.Point {
	if i := c.pageIndex(p); i >= 0 {
		return c.toDoc(screen).Sub(c.pageOrigin(i))
	}
	return f32.Point{}
}

// toScreen maps a page point to screen px.
func (c *Canvas) toScreen(p *document.Page, pt f32.Point) f32.Point {
	if i := c.pageIndex(p); i >= 0 {
		return c.off.Add(c.pageOrigin(i).Add(pt).Mul(c.scale))
	}
	return f32.Point{}
}

func (c *Canvas) Layout(gtx layout.Context) layout.Dimensions {
	c.frame++
	c.now = gtx.Now
	c.view = gtx.Constraints.Max
	c.layoutPages()
	c.updateTransform(gtx)
	c.animate(gtx)
	c.updateTransform(gtx)
	c.handleEvents(gtx)
	c.checkHold(gtx)
	c.layoutPages() // events may have added pages (the + under the last one)
	c.updateTransform(gtx)

	defer clip.Rect{Max: c.view}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, DeskColor)

	var visible []*document.Page
	defer func() { c.syncPDFs(visible) }()
	mid := c.toDoc(f32.Pt(0, float32(c.view.Y)/2)).Y
	bestDist := float32(math.MaxFloat32)
	for i, p := range c.Doc.Pages {
		o := c.pageOrigin(i)
		if dist := float32(math.Abs(float64(o.Y + p.Height/2 - mid))); dist < bestDist {
			bestDist, c.current = dist, i
		}
		x0, y0 := c.off.X+o.X*c.scale, c.off.Y+o.Y*c.scale
		x1, y1 := x0+p.Width*c.scale, y0+p.Height*c.scale
		if y1 < 0 || y0 > float32(c.view.Y) || x1 < 0 || x0 > float32(c.view.X) {
			continue // virtualised: off-screen pages cost nothing
		}
		visible = append(visible, p)
		r := gtx.Dp(3)
		sh := clip.UniformRRect(image.Rect(int(x0)-1, int(y0)+2, int(x1)+1, int(y1)+5), r).Op(gtx.Ops)
		paint.FillShape(gtx.Ops, shadowColor, sh)

		t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(c.scale, c.scale)).Offset(f32.Pt(x0, y0))).Push(gtx.Ops)
		// The part of this page on screen, in page pt.
		view := document.Rect{Min: f32.Pt(-x0/c.scale, -y0/c.scale),
			Max: f32.Pt((float32(c.view.X)-x0)/c.scale, (float32(c.view.Y)-y0)/c.scale)}
		drawn := false
		if c.scale < rasterBelow && len(c.skip) == 0 {
			if img, sz, ok, retry := c.pageRaster(p, c.scale); ok {
				k := p.Width / float32(sz.X) // picture px -> page pt
				it := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(k, k))).Push(gtx.Ops)
				img.Filter = paint.FilterLinear
				img.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				it.Pop()
				drawn = true
			} else if gtx.Now.Before(retry) {
				gtx.Execute(op.InvalidateCmd{At: retry}) // ask again once edits pause
			}
		}
		if !drawn {
			c.drawPageVisible(gtx.Ops, p, view)
		}
		cl := clip.Outline{Path: rectPath(gtx.Ops, 0, 0, p.Width, p.Height)}.Op().Push(gtx.Ops)
		c.drawOverlay(gtx, p)
		cl.Pop()
		t.Pop()
	}
	c.drawAddPage(gtx)
	c.drawSelectionChrome(gtx)
	c.drawCrop(gtx)

	if c.Tool == Eraser && c.hovering {
		var path clip.Path
		path.Begin(gtx.Ops)
		circle(&path, c.hover, c.EraserRadius*c.scale)
		paint.FillShape(gtx.Ops, color.NRGBA{0xff, 0xff, 0xff, 0x30}, clip.Outline{Path: path.End()}.Op())
		path.Begin(gtx.Ops)
		circle(&path, c.hover, c.EraserRadius*c.scale)
		paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: path.End(), Width: 1.5}.Op())
	}

	// Register for input over the whole canvas.
	event.Op(gtx.Ops, c)
	c.cursor().Add(gtx.Ops)

	c.layoutEditor(gtx)

	c.expireLasers(gtx)
	c.evictRasters()
	for p, pc := range c.caches {
		if c.frame-pc.lastUsed > cacheKeep {
			delete(c.caches, p)
		}
	}
	return layout.Dimensions{Size: c.view}
}

func (c *Canvas) cursor() pointer.Cursor {
	switch {
	case c.gesture == gPan:
		return pointer.CursorGrabbing
	case c.gesture == gNone && c.hovering && c.overAddButton(c.hover):
		return pointer.CursorPointer
	case c.SpacePan || c.Tool == Hand:
		return pointer.CursorGrab
	case c.Tool == Eraser:
		return pointer.CursorNone
	case c.Tool == Text:
		return pointer.CursorText
	case c.Tool == Fill:
		return pointer.CursorPointer
	case c.Tool == Select:
		if c.sel != nil && c.hovering {
			if _, ok := c.handleAt(c.hover); ok {
				return pointer.CursorNorthWestResize
			}
			if image.Pt(int(c.hover.X), int(c.hover.Y)).In(c.selScreenRect()) {
				return pointer.CursorAllScroll
			}
		}
		return pointer.CursorDefault
	}
	return pointer.CursorCrosshair
}

// drawOverlay draws live, not-yet-committed content in page coordinates.
func (c *Canvas) drawOverlay(gtx layout.Context, p *document.Page) {
	c.drawLasers(gtx, p)
	if c.gPage != p && (c.sel == nil || c.sel.page != p) {
		return
	}
	if c.live != nil && c.gPage == p {
		DrawStroke(gtx.Ops, c.live)
	}
	if c.bend != nil && c.gPage == p {
		DrawStroke(gtx.Ops, c.bend.cur)
	}
	if c.gesture == gLasso && c.gPage == p && len(c.lasso) > 1 {
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(c.lasso[0])
		for _, q := range c.lasso[1:] {
			path.LineTo(q)
		}
		paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: path.End(), Width: 1.2 / c.scale * 1.5}.Op())
	}
	if c.sel != nil && c.sel.page == p && c.sel.lifted {
		m, _ := c.sel.transform()
		t := op.Affine(m).Push(gtx.Ops)
		for _, si := range c.sel.items {
			DrawItem(gtx.Ops, si.item)
		}
		t.Pop()
	}
}

func (c *Canvas) handleEvents(gtx layout.Context) {
	big := pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  c,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll | pointer.Move | pointer.Leave | pointer.Enter,
			ScrollX: big, ScrollY: big,
		})
		if !ok {
			return
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		c.hover, c.hovering = e.Position, e.Kind != pointer.Leave
		switch e.Kind {
		case pointer.Scroll:
			c.zooming, c.scrolling = false, false
			if e.Modifiers.Contain(key.ModShortcut) || e.Modifiers.Contain(key.ModCtrl) {
				// Ctrl+wheel is also how Windows precision touchpads report pinch.
				f := float32(math.Exp(float64(-e.Scroll.Y) * 0.01))
				c.zoomAt(e.Position, max(0.85, min(1.18, f)))
			} else {
				c.pan = c.pan.Add(e.Scroll)
				c.checkOverscroll(gtx, e.Scroll.Y)
			}
			c.updateTransform(gtx)
		case pointer.Press:
			if c.editing != nil {
				c.commitText()
			}
			// Fingers, the secondary/middle buttons, space and the hand
			// tool pan; the pen/primary button uses the active tool.
			c.zooming, c.scrolling = false, false
			if c.overAddButton(e.Position) && c.OnAddPage != nil {
				c.gesture, c.gPage = gNone, nil
				c.OnAddPage()
				continue
			}
			if e.Source == pointer.Touch || c.SpacePan || c.Tool == Hand ||
				e.Buttons.Contain(pointer.ButtonSecondary) || e.Buttons.Contain(pointer.ButtonTertiary) {
				c.gesture, c.panLast = gPan, e.Position
				continue
			}
			c.press(gtx, e)
		case pointer.Drag:
			if c.gesture == gPan {
				d := e.Position.Sub(c.panLast)
				c.pan = c.pan.Sub(d)
				c.checkOverscroll(gtx, -d.Y)
				c.panLast = e.Position
				c.updateTransform(gtx)
				continue
			}
			c.drag(gtx, e)
		case pointer.Release:
			if c.gesture == gPan {
				c.gesture = gNone
				continue
			}
			c.drag(gtx, e)
			c.release(gtx, e)
		case pointer.Cancel:
			if c.gesture != gPan {
				c.release(gtx, e)
			}
			c.gesture = gNone
		}
	}
}

func sq(v float32) float32 { return v * v }

// ---- laser pointer ----

func (c *Canvas) drawLasers(gtx layout.Context, p *document.Page) {
	for _, l := range c.lasers {
		if l.page != p || len(l.pts) == 0 {
			continue
		}
		a := float32(1)
		if age := c.now.Sub(l.end); !l.end.IsZero() && age > laserHold {
			a = 1 - float32(age-laserHold)/float32(laserFade-laserHold)
		}
		if a <= 0 {
			continue
		}
		px := 1 / c.scale // one screen pixel in pt: keep the beam the same size at any zoom
		glow := &document.Stroke{Color: color.NRGBA{0xff, 0x30, 0x40, uint8(90 * a)}, Width: 14 * px, Points: l.pts}
		core := &document.Stroke{Color: color.NRGBA{0xff, 0xe0, 0xe0, uint8(255 * a)}, Width: 3 * px, Points: l.pts}
		DrawStroke(gtx.Ops, glow)
		DrawStroke(gtx.Ops, core)
	}
}

func (c *Canvas) expireLasers(gtx layout.Context) {
	live := c.lasers[:0]
	for _, l := range c.lasers {
		if l.end.IsZero() || c.now.Sub(l.end) < laserFade {
			live = append(live, l)
		}
	}
	c.lasers = live
	if len(c.lasers) > 0 {
		gtx.Execute(op.InvalidateCmd{})
	}
}

// overscrollFull is how far past the end you scroll to add a page.
const overscrollFull = 140 // dp

// checkOverscroll accumulates scrolling beyond the end of the last page
// and asks for a new page once it's clearly intentional. A blank last
// page never grows another (no runs of empty pages from a flick).
func (c *Canvas) checkOverscroll(gtx layout.Context, dy float32) {
	margin := marginDp * gtx.Metric.PxPerDp
	maxPan := max(0, c.contentH*c.scale+2*margin-float32(c.view.Y)*0.5)
	if dy < 0 || c.pan.Y < maxPan || c.lastPageBlank() {
		c.overscroll = 0
		return
	}
	if dy == 0 { // sideways wobble, or a drag's first move: keep the progress
		return
	}
	c.overscroll += dy
	c.overAt = c.now
	if c.overscroll > float32(gtx.Dp(overscrollFull)) && c.OnScrollPastEnd != nil {
		c.overscroll = 0
		c.OnScrollPastEnd()
	}
}

func (c *Canvas) lastPageBlank() bool {
	if len(c.Doc.Pages) == 0 {
		return true
	}
	for _, l := range c.Doc.Pages[len(c.Doc.Pages)-1].Layers {
		if len(l.Items) > 0 {
			return false
		}
	}
	return true
}

func (c *Canvas) overAddButton(at f32.Point) bool {
	return c.addRadius > 0 && hypot(at.Sub(c.addCenter)) <= c.addRadius*1.15
}

// drawAddPage draws a big + under the last page. Scrolling on past the
// end fills a ring around it; a full ring (or a click) adds a page.
func (c *Canvas) drawAddPage(gtx layout.Context) {
	c.addRadius = 0
	if len(c.Doc.Pages) == 0 || c.OnAddPage == nil {
		return
	}
	// The ring empties again shortly after scrolling stops.
	if c.overscroll > 0 && c.now.Sub(c.overAt) > 250*time.Millisecond {
		if c.overscroll *= 0.8; c.overscroll < 1 {
			c.overscroll = 0
		}
		gtx.Execute(op.InvalidateCmd{})
	}
	last := len(c.Doc.Pages) - 1
	bottom := c.off.Y + (c.tops[last]+c.Doc.Pages[last].Height)*c.scale
	r := float32(gtx.Dp(30))
	center := f32.Pt(c.off.X+c.contentW*c.scale/2, bottom+float32(gtx.Dp(64))+r)
	if center.Y-r > float32(c.view.Y) || center.Y+r < 0 {
		return
	}
	progress := min(1, c.overscroll/float32(gtx.Dp(overscrollFull)))
	c.addCenter, c.addRadius = center, r
	r *= 1 + 0.2*progress

	hot := c.hovering && c.overAddButton(c.hover)
	bg := color.NRGBA{0xff, 0xff, 0xff, 0x10}
	if hot || progress > 0 {
		bg = color.NRGBA{0x8b, 0x6c, 0xff, 0x40}
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	circle(&path, center, r)
	paint.FillShape(gtx.Ops, bg, clip.Outline{Path: path.End()}.Op())
	path.Begin(gtx.Ops)
	circle(&path, center, r)
	paint.FillShape(gtx.Ops, color.NRGBA{0xff, 0xff, 0xff, 0x30}, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(1.5))}.Op())
	if progress > 0 {
		// Progress ring, clockwise from the top.
		ring := r + float32(gtx.Dp(5))
		path.Begin(gtx.Ops)
		const steps = 64
		end := int(math.Ceil(float64(progress * steps)))
		for k := 0; k <= end; k++ {
			a := float64(min(float32(k)/steps, progress))*2*math.Pi - math.Pi/2
			pt := center.Add(f32.Pt(ring*float32(math.Cos(a)), ring*float32(math.Sin(a))))
			if k == 0 {
				path.MoveTo(pt)
			} else {
				path.LineTo(pt)
			}
		}
		paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(3))}.Op())
	}
	// The plus.
	arm, th := r*0.45, float32(gtx.Dp(3))
	plus := color.NRGBA{0xff, 0xff, 0xff, 0xc0}
	if hot || progress > 0 {
		plus = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	}
	for _, rc := range []image.Rectangle{
		image.Rect(int(center.X-arm), int(center.Y-th/2), int(center.X+arm), int(center.Y+th/2)),
		image.Rect(int(center.X-th/2), int(center.Y-arm), int(center.X+th/2), int(center.Y+arm)),
	} {
		paint.FillShape(gtx.Ops, plus, clip.UniformRRect(rc, int(th/2)).Op(gtx.Ops))
	}
	// Label under it.
	label := "Add page"
	switch {
	case progress > 0:
		label = "Keep scrolling to add a page"
	case !c.lastPageBlank():
		label = "Add page · or keep scrolling"
	}
	l := material.Body2(c.Theme, label)
	l.Color = color.NRGBA{0xff, 0xff, 0xff, 0x90}
	l.Alignment = text.Middle
	lg := gtx
	lg.Constraints = layout.Exact(image.Pt(gtx.Dp(300), gtx.Dp(24)))
	t := op.Offset(image.Pt(int(center.X)-gtx.Dp(150), int(center.Y+r)+gtx.Dp(12))).Push(gtx.Ops)
	l.Layout(lg)
	t.Pop()
}
