package canvas

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/document"
	"dot/internal/ink"
)

// SetTool switches tools, finishing any text edit and dropping the
// selection when leaving the select tool.
func (c *Canvas) SetTool(t Tool) {
	if c.editing != nil {
		c.commitText()
	}
	c.ApplyCrop()
	if t != Select {
		c.ClearSelection()
	}
	c.Tool = t
}

func (c *Canvas) press(gtx layout.Context, e pointer.Event) {
	if c.crop != nil {
		if c.cropPress(e.Position) {
			c.gesture, c.gPage = gCrop, c.crop.page
		} else {
			c.ApplyCrop() // clicking elsewhere finishes the crop
		}
		return
	}
	double := e.Time-c.lastTap < 400*time.Millisecond && hypot(e.Position.Sub(c.lastTapAt)) < 8
	c.lastTap, c.lastTapAt = e.Time, e.Position
	if double && (c.Tool == Select || c.Tool == Shape) {
		// Excalidraw-style: double-click a shape to type inside it.
		if page, pt, ok := c.pageAt(e.Position); ok {
			if _, _, hit := c.shapeAt(page, pt); hit {
				c.ClearSelection()
				c.startText(page, pt)
				return
			}
		}
	}
	if c.Tool == Select && c.sel != nil {
		if i, ok := c.bendHandleAt(e.Position); ok {
			c.startBend(i)
			return
		}
		if corner, ok := c.handleAt(e.Position); ok {
			b := c.sel.bounds
			cs := [4]f32.Point{b.Min, {X: b.Max.X, Y: b.Min.Y}, b.Max, {X: b.Min.X, Y: b.Max.Y}}
			c.lift()
			c.sel.anchor, c.gStart = cs[(corner+2)%4], cs[corner]
			c.gesture, c.gPage = gScale, c.sel.page
			return
		}
		if image.Pt(int(e.Position.X), int(e.Position.Y)).In(c.selScreenRect()) {
			c.lift()
			c.gesture, c.gPage, c.gStart = gMove, c.sel.page, c.local(c.sel.page, e.Position)
			return
		}
	}
	page, pt, ok := c.pageAt(e.Position)
	if !ok {
		c.ClearSelection()
		return
	}
	c.gPage, c.gStart, c.gLast = page, pt, pt
	switch c.Tool {
	case Pen, Highlighter:
		c.gesture = gInk
		pressure, real := c.Input.Begin(pt, e.Time)
		c.engine.Cfg, c.engine.Style, c.engine.Base = c.Ink, c.PenStyle, c.PenWidth
		s := &document.Stroke{Tool: document.ToolPen, Color: c.PenColor, Width: c.PenWidth, Style: c.LineStyle}
		if c.Tool == Highlighter {
			s.Style = ""
			c.engine.Cfg.Taper, c.engine.Style, c.engine.Base = false, ink.Ballpoint, c.HighlighterWidth
			s.Tool, s.Color, s.Width = document.ToolHighlighter, c.HighlighterColor, c.HighlighterWidth
		}
		c.engine.Begin(pt, pressure, real)
		s.Points = c.engine.Points
		if c.Tool == Pen {
			s.Widths = c.engine.Widths
		}
		c.live, c.snapped = s, false
		c.lastMove, c.moveAt = gtx.Now, e.Position
	case Shape:
		c.gesture = gShape
		c.live = &document.Stroke{Tool: document.ToolPen, Color: c.PenColor, Width: c.PenWidth,
			Style: c.LineStyle, Points: ink.Build(c.ShapeKind, pt, pt)}
		if c.ShapeKind.Bendable() {
			c.live.Shape, c.live.Ctrl = ink.ShapeNames[c.ShapeKind], []f32.Point{pt, pt, pt}
		}
		if c.ShapeKind.Closed() {
			c.live.Fill = c.FillColor
		}
	case Fill:
		c.pour(page, pt)
	case Eraser:
		c.gesture, c.erased = gErase, nil
		c.eraseAt(pt)
	case Laser:
		c.gesture = gLaser
		c.lasers = append(c.lasers, laserStroke{page: page, pts: []f32.Point{pt}})
	case Text:
		c.startText(page, pt)
	case Select:
		c.ClearSelection()
		c.gesture, c.lasso = gLasso, append(c.lasso[:0], pt)
	}
}

func (c *Canvas) drag(gtx layout.Context, e pointer.Event) {
	if c.gPage == nil {
		return
	}
	if c.gesture == gCrop {
		c.cropDrag(e.Position)
		return
	}
	pt := c.local(c.gPage, e.Position)
	switch c.gesture {
	case gInk:
		if c.snapped || e.Kind == pointer.Release {
			return
		}
		if d := e.Position.Sub(c.moveAt); d.X*d.X+d.Y*d.Y > 9 {
			c.lastMove, c.moveAt = gtx.Now, e.Position
		}
		pressure, _ := c.Input.Next(pt, e.Time)
		c.engine.Add(pt, pressure, 0.6/c.scale)
		pts, ws := c.engine.Live(pt)
		c.live.Points = pts
		if c.live.Widths != nil {
			c.live.Widths = ws
		}
	case gShape:
		if e.Modifiers.Contain(key.ModShift) { // square / circle / 45° line
			pt = ink.Constrain(c.ShapeKind, c.gStart, pt)
		}
		if c.live.Ctrl != nil {
			c.live.Ctrl = []f32.Point{c.gStart, c.gStart.Add(pt).Mul(0.5), pt}
			c.live.Points = ink.Curve(c.ShapeKind, c.gStart, c.live.Ctrl[1], pt)
		} else {
			c.live.Points = ink.Build(c.ShapeKind, c.gStart, pt)
		}
	case gBend:
		c.dragBend(pt)
	case gErase:
		c.eraseAt(pt)
	case gLaser:
		l := &c.lasers[len(c.lasers)-1]
		if d := pt.Sub(l.pts[len(l.pts)-1]); d.X*d.X+d.Y*d.Y > sq(1.5/c.scale) {
			l.pts = append(l.pts, pt)
		}
	case gLasso:
		if d := pt.Sub(c.lasso[len(c.lasso)-1]); d.X*d.X+d.Y*d.Y > sq(2/c.scale) {
			c.lasso = append(c.lasso, pt)
		}
	case gMove:
		c.sel.move = pt.Sub(c.gStart)
	case gScale:
		h := c.gStart.Sub(c.sel.anchor)
		v := pt.Sub(c.sel.anchor)
		if l2 := h.X*h.X + h.Y*h.Y; l2 > 0 {
			c.sel.scale = max(0.05, min(20, (v.X*h.X+v.Y*h.Y)/l2))
		}
	}
	c.gLast = pt
}

func (c *Canvas) release(gtx layout.Context, e pointer.Event) {
	switch c.gesture {
	case gInk:
		if !c.snapped {
			pts, ws := c.engine.Finish(c.local(c.gPage, e.Position))
			c.live.Points = pts
			if c.live.Widths != nil {
				c.live.Widths = ws
			}
		}
		if len(c.live.Points) == 1 { // a tap leaves a dot
			c.live.Points = append(c.live.Points, c.live.Points[0].Add(f32.Pt(0.01, 0)))
			if c.live.Widths != nil {
				c.live.Widths = append(c.live.Widths, c.live.Widths[0])
			}
		}
		c.History.Do(c.Doc, &document.AddItem{Page: c.gPage, Layer: c.gPage.TopLayer(), Item: c.live})
		c.appendItem(c.gPage, c.live)
	case gShape:
		if d := c.gLast.Sub(c.gStart); d.X*d.X+d.Y*d.Y > 1 {
			c.History.Do(c.Doc, &document.AddItem{Page: c.gPage, Layer: c.gPage.TopLayer(), Item: c.live})
			c.appendItem(c.gPage, c.live)
			if c.live.Ctrl != nil { // select it so it can be bent right away
				c.sel = newSelection(c.gPage, []selItem{{c.gPage.TopLayer(), c.live}})
			}
		}
	case gBend:
		c.finishBend()
	case gErase:
		if len(c.erased) > 0 {
			c.History.Push(c.Doc, c.erased) // already applied while dragging
		}
		c.erased = nil
	case gLaser:
		c.lasers[len(c.lasers)-1].end = c.now
	case gLasso:
		c.selectLasso(c.gPage, c.lasso)
	case gMove, gScale:
		c.bake()
	case gCrop:
		c.crop.handle = -1
	}
	c.gesture, c.live, c.gPage = gNone, nil, nil
}

// checkHold implements "draw and hold": keeping the pen still for a
// moment at the end of a stroke snaps it to a clean shape.
func (c *Canvas) checkHold(gtx layout.Context) {
	if c.gesture != gInk || c.snapped || !c.SnapShapes || len(c.engine.Points) < 5 {
		return
	}
	if wait := c.lastMove.Add(holdSnap); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	kind, pts := ink.Recognize(c.engine.Points)
	c.lastMove = gtx.Now.Add(holdSnap) // don't retry every frame
	if kind == ink.NoShape {
		return
	}
	c.live.Points, c.snapped = pts, true
	if c.live.Widths != nil {
		c.live.Widths = nil
	}
}

// ---- eraser ----

func (c *Canvas) eraseAt(pt f32.Point) {
	page := c.gPage
	for _, l := range page.Layers {
		if l.Hidden {
			continue
		}
		for i := len(l.Items) - 1; i >= 0; i-- {
			s, ok := l.Items[i].(*document.Stroke)
			if !ok || !s.Bounds().Expand(c.EraserRadius).Contains(pt) || !hits(s, pt, c.EraserRadius) {
				continue
			}
			var cmd document.Command
			if c.EraserMode == ErasePartial {
				pieces, changed := splitStroke(s, pt, c.EraserRadius)
				if !changed {
					continue
				}
				cmd = &document.Splice{Page: page, Layer: l, Old: s, New: pieces}
			} else {
				cmd = &document.RemoveItem{Page: page, Layer: l, Item: s}
			}
			cmd.Apply(c.Doc)
			c.erased = append(c.erased, cmd)
		}
	}
}

func hits(s *document.Stroke, p f32.Point, r float32) bool {
	for i := range s.Points {
		a := s.Points[i]
		b := a
		if i+1 < len(s.Points) {
			b = s.Points[i+1]
		}
		lim := r + s.WidthAt(i)/2
		if segDist2(p, a, b) <= lim*lim {
			return true
		}
	}
	return false
}

// splitStroke cuts the part of s within r of p, returning the pieces left.
func splitStroke(s *document.Stroke, p f32.Point, r float32) ([]document.Item, bool) {
	// Densify so a long segment can be cut in the middle.
	step := max(r/3, 0.3)
	var pts []f32.Point
	var ws []float32
	for i, a := range s.Points {
		pts, ws = append(pts, a), append(ws, s.WidthAt(i))
		if i+1 == len(s.Points) {
			break
		}
		b := s.Points[i+1]
		d := b.Sub(a)
		n := int(hypot(d) / step)
		for k := 1; k < n; k++ {
			t := float32(k) / float32(n)
			pts = append(pts, a.Add(d.Mul(t)))
			ws = append(ws, s.WidthAt(i)+(s.WidthAt(i+1)-s.WidthAt(i))*t)
		}
	}
	var pieces []document.Item
	changed := false
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= 2 {
			n := &document.Stroke{Tool: s.Tool, Color: s.Color, Width: s.Width, Extra: s.Extra,
				Points: append([]f32.Point(nil), pts[start:end]...)}
			if s.Widths != nil {
				n.Widths = append([]float32(nil), ws[start:end]...)
			}
			pieces = append(pieces, n)
		}
		start = -1
	}
	for i, q := range pts {
		if d := q.Sub(p); d.X*d.X+d.Y*d.Y < r*r {
			changed = true
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(pts))
	return pieces, changed
}

func hypot(p f32.Point) float32 {
	return float32(sqrt(float64(p.X*p.X + p.Y*p.Y)))
}

func segDist2(p, a, b f32.Point) float32 {
	ab, ap := b.Sub(a), p.Sub(a)
	l2 := ab.X*ab.X + ab.Y*ab.Y
	t := float32(0)
	if l2 > 0 {
		t = max(0, min(1, (ap.X*ab.X+ap.Y*ab.Y)/l2))
	}
	d := ap.Sub(ab.Mul(t))
	return d.X*d.X + d.Y*d.Y
}

// ---- selection ----

type selItem struct {
	layer *document.Layer
	item  document.Item
}

type selection struct {
	page   *document.Page
	items  []selItem
	bounds document.Rect // page pt, before the live transform
	lifted bool
	move   f32.Point
	scale  float32
	anchor f32.Point
}

func (s *selection) transform() (f32.Affine2D, float32) {
	sc := s.scale
	if sc == 0 {
		sc = 1
	}
	return f32.AffineId().Scale(s.anchor, f32.Pt(sc, sc)).Offset(s.move), sc
}

func newSelection(page *document.Page, items []selItem) *selection {
	if len(items) == 0 {
		return nil
	}
	s := &selection{page: page, items: items}
	s.recomputeBounds()
	return s
}

func (s *selection) recomputeBounds() {
	first := true
	for _, si := range s.items {
		r, ok := document.ItemBounds(si.item)
		if !ok {
			continue
		}
		if first {
			s.bounds, first = r, false
			continue
		}
		s.bounds.Min = f32.Pt(min(s.bounds.Min.X, r.Min.X), min(s.bounds.Min.Y, r.Min.Y))
		s.bounds.Max = f32.Pt(max(s.bounds.Max.X, r.Max.X), max(s.bounds.Max.Y, r.Max.Y))
	}
}

func (c *Canvas) HasSelection() bool { return c.sel != nil }

func (c *Canvas) ClearSelection() {
	if c.sel != nil && c.sel.lifted {
		c.bake()
	}
	c.sel = nil
}

// SelectionRect is the selection's on-screen box, for the action bar.
func (c *Canvas) SelectionRect() (image.Rectangle, bool) {
	if c.sel == nil || c.gesture == gMove || c.gesture == gScale {
		return image.Rectangle{}, false
	}
	return c.selScreenRect(), true
}

func (c *Canvas) selScreenRect() image.Rectangle {
	if c.sel == nil {
		return image.Rectangle{}
	}
	m, _ := c.sel.transform()
	a := c.toScreen(c.sel.page, m.Transform(c.sel.bounds.Min))
	b := c.toScreen(c.sel.page, m.Transform(c.sel.bounds.Max))
	const pad = 8
	return image.Rect(int(min(a.X, b.X))-pad, int(min(a.Y, b.Y))-pad, int(max(a.X, b.X))+pad, int(max(a.Y, b.Y))+pad)
}

func (c *Canvas) handleAt(p f32.Point) (int, bool) {
	r := c.selScreenRect()
	cs := [4]image.Point{r.Min, {X: r.Max.X, Y: r.Min.Y}, r.Max, {X: r.Min.X, Y: r.Max.Y}}
	for i, q := range cs {
		if d := p.Sub(f32.Pt(float32(q.X), float32(q.Y))); d.X*d.X+d.Y*d.Y < 14*14 {
			return i, true
		}
	}
	return 0, false
}

func (c *Canvas) drawSelectionChrome(gtx layout.Context) {
	if c.sel == nil {
		return
	}
	r := c.selScreenRect()
	fill := Accent
	fill.A = 0x18
	paint.FillShape(gtx.Ops, fill, clip.UniformRRect(r, gtx.Dp(4)).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: clip.UniformRRect(r, gtx.Dp(4)).Path(gtx.Ops), Width: 1.5}.Op())
	hr := gtx.Dp(5)
	for _, q := range []image.Point{r.Min, {X: r.Max.X, Y: r.Min.Y}, r.Max, {X: r.Min.X, Y: r.Max.Y}} {
		rect := image.Rect(q.X-hr, q.Y-hr, q.X+hr, q.Y+hr)
		paint.FillShape(gtx.Ops, Accent, clip.Ellipse(rect.Inset(-1)).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, color.NRGBA{255, 255, 255, 255}, clip.Ellipse(rect.Inset(1)).Op(gtx.Ops))
	}
	// Bend handles: ends (white) and the middle (filled) of a line/arrow.
	if hs, ok := c.bendHandles(); ok {
		for i, q := range hs {
			pr := gtx.Dp(7)
			rect := image.Rect(int(q.X)-pr, int(q.Y)-pr, int(q.X)+pr, int(q.Y)+pr)
			paint.FillShape(gtx.Ops, Accent, clip.Ellipse(rect).Op(gtx.Ops))
			if i != 1 {
				paint.FillShape(gtx.Ops, color.NRGBA{255, 255, 255, 255}, clip.Ellipse(rect.Inset(gtx.Dp(2))).Op(gtx.Ops))
			}
		}
	}
}

func (c *Canvas) selectLasso(page *document.Page, poly []f32.Point) {
	var items []selItem
	tap := len(poly) < 3 || polyLen(poly) < 6/c.scale
	for _, l := range page.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			if tap {
				if hitItem(it, poly[0], 4/c.scale) {
					items = []selItem{{l, it}} // topmost wins
				}
				continue
			}
			if insideLasso(it, poly) {
				items = append(items, selItem{l, it})
			}
		}
	}
	if tap && len(items) == 1 {
		// A tapped shape carries its label along.
		if s, ok := items[0].item.(*document.Stroke); ok && closedOutline(s) {
			for _, l := range page.Layers {
				for _, it := range l.Items {
					if r, ok := it.(*document.RawElement); ok && r.Name == "text" {
						if b, ok := document.ItemBounds(r); ok && pointInPoly(b.Min.Add(b.Max).Mul(0.5), s.Points) {
							items = append(items, selItem{l, it})
						}
					}
				}
			}
		}
	}
	c.sel = newSelection(page, items)
}

func hitItem(it document.Item, p f32.Point, r float32) bool {
	if s, ok := it.(*document.Stroke); ok {
		if !s.Bounds().Expand(r).Contains(p) {
			return false
		}
		// Clicking anywhere inside a box or filled shape picks it, like
		// Excalidraw; open ink still needs a click on the line.
		if (s.Fill.A > 0 || closedOutline(s)) && pointInPoly(p, s.Points) {
			return true
		}
		return hits(s, p, r)
	}
	b, ok := document.ItemBounds(it)
	return ok && b.Contains(p)
}

// insideLasso: strokes need most of their points inside, boxes their centre.
func insideLasso(it document.Item, poly []f32.Point) bool {
	if s, ok := it.(*document.Stroke); ok {
		in := 0
		for _, p := range s.Points {
			if pointInPoly(p, poly) {
				in++
			}
		}
		return in*10 >= len(s.Points)*7
	}
	b, ok := document.ItemBounds(it)
	return ok && pointInPoly(b.Min.Add(b.Max).Mul(0.5), poly)
}

func pointInPoly(p f32.Point, poly []f32.Point) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

func polyLen(poly []f32.Point) float32 {
	var l float32
	for i := 1; i < len(poly); i++ {
		l += hypot(poly[i].Sub(poly[i-1]))
	}
	return l
}

// lift hides the selected items from the cached page so a drag only
// redraws the selection, not the whole page.
func (c *Canvas) lift() {
	s := c.sel
	if s.lifted {
		return
	}
	for _, si := range s.items {
		c.skip[si.item] = true
	}
	s.lifted, s.move, s.scale, s.anchor = true, f32.Point{}, 1, f32.Point{}
	s.page.Touch()
}

// bake writes the live transform into the document as one undo step.
func (c *Canvas) bake() {
	s := c.sel
	if s == nil || !s.lifted {
		return
	}
	for _, si := range s.items {
		delete(c.skip, si.item)
	}
	s.lifted = false
	m, sc := s.transform()
	if s.move == (f32.Point{}) && sc == 1 {
		s.page.Touch()
		return
	}
	var batch document.Batch
	for i, si := range s.items {
		n := document.Transformed(si.item, m, sc)
		batch = append(batch, &document.Splice{Page: s.page, Layer: si.layer, Old: si.item, New: []document.Item{n}})
		s.items[i].item = n
	}
	c.History.Do(c.Doc, batch)
	s.move, s.scale = f32.Point{}, 1
	s.recomputeBounds()
}

func (c *Canvas) DeleteSelection() {
	if c.sel == nil {
		return
	}
	c.bake()
	var batch document.Batch
	for _, si := range c.sel.items {
		batch = append(batch, &document.RemoveItem{Page: c.sel.page, Layer: si.layer, Item: si.item})
	}
	c.History.Do(c.Doc, batch)
	c.sel = nil
}

func (c *Canvas) RecolorSelection(col color.NRGBA) bool {
	if c.sel == nil {
		return false
	}
	c.bake()
	var batch document.Batch
	for i, si := range c.sel.items {
		n := document.Recolored(si.item, col)
		if n == si.item {
			continue
		}
		batch = append(batch, &document.Splice{Page: c.sel.page, Layer: si.layer, Old: si.item, New: []document.Item{n}})
		c.sel.items[i].item = n
	}
	if len(batch) > 0 {
		c.History.Do(c.Doc, batch)
	}
	return true
}

func (c *Canvas) Copy() {
	if c.sel == nil {
		return
	}
	c.bake()
	c.clipboard = c.clipboard[:0]
	for _, si := range c.sel.items {
		c.clipboard = append(c.clipboard, si.item)
	}
}

func (c *Canvas) Cut() { c.Copy(); c.DeleteSelection() }

// HasClipboard reports whether ink has been copied inside dot!.
func (c *Canvas) HasClipboard() bool { return len(c.clipboard) > 0 }

// Paste drops the clipboard onto the current page, offset so it is visible
// as a copy, and selects it.
func (c *Canvas) Paste() {
	if len(c.clipboard) == 0 {
		return
	}
	page := c.Doc.Pages[c.Current()]
	if c.sel != nil && c.sel.page == page {
		c.bake()
	}
	m := f32.AffineId().Offset(f32.Pt(14, 14))
	layer := page.TopLayer()
	var batch document.Batch
	var items []selItem
	for _, it := range c.clipboard {
		n := document.Transformed(it, m, 1)
		batch = append(batch, &document.AddItem{Page: page, Layer: layer, Item: n})
		items = append(items, selItem{layer, n})
	}
	c.History.Do(c.Doc, batch)
	c.clipboard = c.clipboard[:0]
	for _, si := range items {
		c.clipboard = append(c.clipboard, si.item) // next paste offsets again
	}
	c.Tool = Select
	c.sel = newSelection(page, items)
}

func (c *Canvas) DuplicateSelection() {
	if c.sel == nil {
		return
	}
	saved := c.clipboard
	c.Copy()
	c.Paste()
	c.clipboard = saved
}

func (c *Canvas) SelectAll() {
	page := c.Doc.Pages[c.Current()]
	var items []selItem
	for _, l := range page.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			items = append(items, selItem{l, it})
		}
	}
	c.Tool = Select
	c.sel = newSelection(page, items)
}

// ---- text ----

type textEdit struct {
	page   *document.Page
	pos    f32.Point
	layer  *document.Layer
	old    *document.RawElement
	size   float32
	color  color.NRGBA
	family string
	bold   bool
	italic bool
	focus  bool
	// Labels typed into a shape wrap to the shape and are centred in it.
	centered bool
	center   f32.Point
	boxW     float32
}

func (c *Canvas) startText(page *document.Page, pt f32.Point) {
	te := &textEdit{page: page, size: c.TextSize, color: c.PenColor, layer: page.TopLayer(), focus: true,
		family: c.TextFont, bold: c.TextBold, italic: c.TextItalic}
	for li := len(page.Layers) - 1; li >= 0 && te.old == nil; li-- {
		l := page.Layers[li]
		for i := len(l.Items) - 1; i >= 0; i-- {
			r, ok := l.Items[i].(*document.RawElement)
			if !ok || r.Name != "text" {
				continue
			}
			if b, _ := document.ItemBounds(r); b.Expand(3).Contains(pt) {
				te.old, te.layer = r, l
				break
			}
		}
	}
	if te.old != nil {
		te.pos = f32.Pt(te.old.Float("x"), te.old.Float("y"))
		te.size, te.color = te.old.Float("size"), te.old.Color()
		te.family, te.bold, te.italic = ParseFont(te.old.Attr("font"))
		if w := te.old.Float("inkWidth"); w > 0 {
			te.centered, te.boxW = true, w
			te.center = f32.Pt(te.pos.X+w/2, 0)
			if s, _, ok := c.shapeAt(page, pt); ok {
				te.center = s.Bounds().Min.Add(s.Bounds().Max).Mul(0.5)
			} else {
				_, h := measureWrapped(te.old.Attr("font"), te.size, te.old.Text(), w)
				te.center.Y = te.pos.Y + h/2
			}
		}
		c.editor.SetText(te.old.Text())
		c.skip[te.old] = true
		page.Touch()
	} else {
		te.pos = pt.Sub(f32.Pt(0, te.size*0.6))
		c.editor.SetText("")
		if s, _, ok := c.shapeAt(page, pt); ok { // typing in a box: label it
			c.labelIn(te, s)
		}
	}
	c.editing = te
}

// EditingRect is the on-screen box of the text being edited.
func (c *Canvas) EditingRect() (image.Rectangle, bool) {
	if c.editing == nil {
		return image.Rectangle{}, false
	}
	return c.editRect, true
}

// SetTextStyle changes the font/style/size/colour for new text and for the
// text being edited, keeping the keyboard focus in the text box.
func (c *Canvas) SetTextStyle(family string, bold, italic bool, size float32, col color.NRGBA) {
	c.TextFont, c.TextBold, c.TextItalic, c.TextSize = family, bold, italic, size
	if te := c.editing; te != nil {
		te.family, te.bold, te.italic, te.size, te.color = family, bold, italic, size, col
		te.focus = true
	}
}

// TextStyle is the style in effect: the edited text's, else the defaults.
func (c *Canvas) TextStyle() (family string, bold, italic bool, size float32, col color.NRGBA) {
	if te := c.editing; te != nil {
		return te.family, te.bold, te.italic, te.size, te.color
	}
	return c.TextFont, c.TextBold, c.TextItalic, c.TextSize, c.PenColor
}

// FinishText commits the text being edited, if any.
func (c *Canvas) FinishText() { c.commitText() }

func (c *Canvas) commitText() {
	te := c.editing
	c.editing = nil
	if te == nil {
		return
	}
	txt := strings.TrimRight(c.editor.Text(), " \n")
	fontAttr := FontAttr(te.family, te.bold, te.italic)
	if te.old != nil {
		delete(c.skip, te.old)
		te.page.Touch()
		o := te.old
		if txt == o.Text() && fontAttr == o.Attr("font") && te.size == o.Float("size") && te.color == o.Color() {
			return
		}
		var repl []document.Item
		if txt != "" {
			n := o.Clone()
			n.SetText(txt)
			if te.centered {
				_, h := measureWrapped(fontAttr, te.size, txt, te.boxW)
				n.SetAttr("y", document.FormatNum(te.center.Y-h/2))
			}
			n.SetAttr("font", fontAttr)
			n.SetAttr("size", document.FormatNum(te.size))
			n.SetAttr("color", document.ColorString(te.color))
			repl = append(repl, n)
		}
		c.History.Do(c.Doc, &document.Splice{Page: te.page, Layer: te.layer, Old: te.old, New: repl})
		return
	}
	if txt != "" {
		pos := te.pos
		if te.centered {
			_, h := measureWrapped(fontAttr, te.size, txt, te.boxW)
			pos = f32.Pt(te.center.X-te.boxW/2, te.center.Y-h/2)
		}
		el := document.NewText(pos, te.size, te.color, txt)
		el.SetAttr("font", fontAttr)
		if te.centered {
			el.SetAttr("inkWidth", document.FormatNum(te.boxW))
		}
		c.History.Do(c.Doc, &document.AddItem{Page: te.page, Layer: te.layer, Item: el})
	}
}

func (c *Canvas) layoutEditor(gtx layout.Context) {
	te := c.editing
	if te == nil || c.Theme == nil {
		return
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &c.editor, Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			c.commitText()
			return
		}
	}
	at := c.toScreen(te.page, te.pos)
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = max(gtx.Dp(120), int((te.page.Width-te.pos.X)*c.scale))
	if te.centered {
		w := int(te.boxW * c.scale)
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	}
	ed := material.Editor(c.Theme, &c.editor, "Type here…")
	if te.centered {
		ed.Editor.Alignment = text.Middle
		ed.Hint = ""
	}
	ed.Font = GioFont(te.family, te.bold, te.italic)
	ed.TextSize = unit.Sp(te.size * c.scale / gtx.Metric.PxPerSp)
	ed.Color = te.color
	ed.HintColor = color.NRGBA{0x90, 0x90, 0x98, 0xff}
	ed.SelectionColor = color.NRGBA{Accent.R, Accent.G, Accent.B, 0x60}
	ed.LineHeightScale = 1.15
	m := op.Record(gtx.Ops)
	dims := ed.Layout(gtx)
	call := m.Stop()
	if te.centered { // keep the growing text vertically centred in the shape
		mid := c.toScreen(te.page, te.center)
		at = f32.Pt(mid.X-float32(dims.Size.X)/2, mid.Y-float32(dims.Size.Y)/2)
	}
	defer op.Offset(image.Pt(int(at.X), int(at.Y))).Push(gtx.Ops).Pop()
	box := image.Rectangle{Max: dims.Size}.Inset(-gtx.Dp(4))
	c.editRect = box.Add(image.Pt(int(at.X), int(at.Y)))
	paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: clip.UniformRRect(box, gtx.Dp(3)).Path(gtx.Ops), Width: 1}.Op())
	call.Add(gtx.Ops)
	if te.focus {
		gtx.Execute(key.FocusCmd{Tag: &c.editor})
		te.focus = false
	}
}

// pour fills the topmost shape or loop under pt with FillColor (or
// clears its fill when FillColor is "none"). Hand-drawn loops count too:
// the outline is treated as closed.
func (c *Canvas) pour(page *document.Page, pt f32.Point) {
	for li := len(page.Layers) - 1; li >= 0; li-- {
		l := page.Layers[li]
		if l.Hidden {
			continue
		}
		for i := len(l.Items) - 1; i >= 0; i-- {
			s, ok := l.Items[i].(*document.Stroke)
			if !ok || len(s.Points) < 3 || s.Tool == document.ToolHighlighter ||
				!s.Bounds().Contains(pt) || !pointInPoly(pt, s.Points) {
				continue
			}
			if s.Fill == c.FillColor {
				return
			}
			n := s.Clone()
			n.Fill = c.FillColor
			c.History.Do(c.Doc, &document.Splice{Page: page, Layer: l, Old: s, New: []document.Item{n}})
			return
		}
	}
}

// RefillSelection sets the fill of the selected shapes (A == 0 removes it).
func (c *Canvas) RefillSelection(fill color.NRGBA) bool {
	if c.sel == nil {
		return false
	}
	c.bake()
	var batch document.Batch
	for i, si := range c.sel.items {
		s, ok := si.item.(*document.Stroke)
		if !ok || len(s.Points) < 3 || s.Fill == fill {
			continue
		}
		n := s.Clone()
		n.Fill = fill
		batch = append(batch, &document.Splice{Page: c.sel.page, Layer: si.layer, Old: s, New: []document.Item{n}})
		c.sel.items[i].item = n
	}
	if len(batch) > 0 {
		c.History.Do(c.Doc, batch)
	}
	return true
}

// closedOutline reports whether a stroke encloses an area (a box, circle,
// hand-drawn loop...).
func closedOutline(s *document.Stroke) bool {
	if len(s.Points) < 4 || s.Ctrl != nil {
		return false
	}
	b := s.Bounds()
	size := max(b.Max.X-b.Min.X, b.Max.Y-b.Min.Y)
	return hypot(s.Points[0].Sub(s.Points[len(s.Points)-1])) < max(size*0.15, 4)
}

// shapeAt finds the topmost closed shape containing pt.
func (c *Canvas) shapeAt(page *document.Page, pt f32.Point) (*document.Stroke, *document.Layer, bool) {
	for li := len(page.Layers) - 1; li >= 0; li-- {
		l := page.Layers[li]
		if l.Hidden {
			continue
		}
		for i := len(l.Items) - 1; i >= 0; i-- {
			s, ok := l.Items[i].(*document.Stroke)
			if ok && s.Tool != document.ToolHighlighter && closedOutline(s) &&
				s.Bounds().Contains(pt) && pointInPoly(pt, s.Points) {
				return s, l, true
			}
		}
	}
	return nil, nil, false
}

// measureText returns a text element's size in pt.
func measureText(fontAttr string, size float32, txt string) (float32, float32) {
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(1<<20, 1<<20)}}
	fam, b, i := ParseFont(fontAttr)
	d := widget.Label{LineHeightScale: 1.15}.Layout(gtx, Shaper(), GioFont(fam, b, i), unit.Sp(size), txt, op.CallOp{})
	return float32(d.Size.X), float32(d.Size.Y)
}

// ---- bendable lines & arrows ----

type bendState struct {
	old, cur *document.Stroke
	layer    *document.Layer
	handle   int // 0 start, 1 middle (bend), 2 end
}

// bendHandles: on-screen start, middle and end of a selected line/arrow.
func (c *Canvas) bendHandles() ([3]f32.Point, bool) {
	var hs [3]f32.Point
	if c.sel == nil || len(c.sel.items) != 1 || c.sel.lifted {
		return hs, false
	}
	s, ok := c.sel.items[0].item.(*document.Stroke)
	if !ok || len(s.Ctrl) != 3 {
		return hs, false
	}
	if c.bend != nil {
		s = c.bend.cur
	}
	a, ctl, b := s.Ctrl[0], s.Ctrl[1], s.Ctrl[2]
	for i, p := range []f32.Point{a, ink.MidPoint(a, ctl, b), b} {
		hs[i] = c.toScreen(c.sel.page, p)
	}
	return hs, true
}

func (c *Canvas) bendHandleAt(p f32.Point) (int, bool) {
	hs, ok := c.bendHandles()
	if !ok {
		return 0, false
	}
	for i, q := range hs {
		if hypot(q.Sub(p)) < 13 {
			return i, true
		}
	}
	return 0, false
}

func (c *Canvas) startBend(i int) {
	si := c.sel.items[0]
	old := si.item.(*document.Stroke)
	c.bend = &bendState{old: old, cur: old.Clone(), layer: si.layer, handle: i}
	c.skip[old] = true
	c.sel.page.Touch()
	c.gesture, c.gPage = gBend, c.sel.page
}

func (c *Canvas) dragBend(pt f32.Point) {
	bs := c.bend
	s := bs.cur
	a, ctl, b := s.Ctrl[0], s.Ctrl[1], s.Ctrl[2]
	mid := ink.MidPoint(a, ctl, b)
	switch bs.handle {
	case 0:
		a = pt
	case 2:
		b = pt
	default:
		mid = pt
	}
	if bs.handle != 1 { // moving an end keeps the bend's shape
		ctl = ink.ControlFor(a, mid.Add(a.Add(b).Mul(0.5).Sub(s.Ctrl[0].Add(s.Ctrl[2]).Mul(0.5))), b)
	} else {
		ctl = ink.ControlFor(a, mid, b)
	}
	s.Ctrl = []f32.Point{a, ctl, b}
	s.Points = ink.Curve(ink.ShapeByName(s.Shape), a, ctl, b)
	s.Invalidate()
}

func (c *Canvas) finishBend() {
	bs := c.bend
	c.bend = nil
	delete(c.skip, bs.old)
	c.History.Do(c.Doc, &document.Splice{Page: c.gPage, Layer: bs.layer, Old: bs.old, New: []document.Item{bs.cur}})
	c.sel = newSelection(c.gPage, []selItem{{bs.layer, bs.cur}})
}

// labelIn sets up a text edit as a label for shape s: wrapped to the
// shape's width, centred, in a colour that reads on the shape's fill.
func (c *Canvas) labelIn(te *textEdit, s *document.Stroke) {
	b := s.Bounds()
	te.centered = true
	te.center = b.Min.Add(b.Max).Mul(0.5)
	te.boxW = max(20, (b.Max.X-b.Min.X)*0.84)
	if s.Fill.A > 0x60 && document.IsDark(s.Fill) == document.IsDark(te.color) {
		te.color = document.Black
		if document.IsDark(s.Fill) {
			te.color = document.White
		}
	}
	te.pos = te.center.Sub(f32.Pt(te.boxW/2, te.size*0.6))
}

// measureWrapped returns a text's size in pt when wrapped to width w.
func measureWrapped(fontAttr string, size float32, txt string, w float32) (float32, float32) {
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(int(w), 1<<20)}}
	fam, b, i := ParseFont(fontAttr)
	d := widget.Label{LineHeightScale: 1.15, Alignment: text.Middle}.Layout(gtx, Shaper(), GioFont(fam, b, i), unit.Sp(size), txt, op.CallOp{})
	return float32(d.Size.X), float32(d.Size.Y)
}

// MeasureLine is a single line's width in pt (for PDF text layout).
func MeasureLine(fontAttr string, size float32, line string) float32 {
	w, _ := measureText(fontAttr, size, line)
	return w
}
