package canvas

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/png"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"dot/internal/document"
)

// viewCenter returns the current page and the visible centre in its coordinates.
func (c *Canvas) viewCenter() (*document.Page, f32.Point) {
	p := c.Doc.Pages[c.Current()]
	pt := c.local(p, c.center())
	pt.X = max(0, min(p.Width, pt.X))
	pt.Y = max(0, min(p.Height, pt.Y))
	return p, pt
}

// InsertImage places an image (PNG, JPEG or GIF bytes) in the middle of
// the view, sized to fit comfortably, and selects it so it can be moved,
// resized or cropped straight away. It is stored as a Xournal++ <image>.
func (c *Canvas) InsertImage(data []byte) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG")) { // Xournal++ embeds PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		data = buf.Bytes()
	}
	page, center := c.viewCenter()
	b := img.Bounds()
	// Screenshots are usually 2× (Retina): 1 px ≈ 0.5 pt reads naturally.
	w, h := float32(b.Dx())*0.5, float32(b.Dy())*0.5
	if f := min(1, page.Width*0.8/w, page.Height*0.6/h); f < 1 {
		w, h = w*f, h*f
	}
	x := max(0, min(page.Width-w, center.X-w/2))
	y := max(0, min(page.Height-h, center.Y-h/2))
	el := newImage(data, document.Rect{Min: f32.Pt(x, y), Max: f32.Pt(x+w, y+h)})
	c.addAndSelect(page, el)
	return nil
}

func newImage(png []byte, r document.Rect) *document.RawElement {
	el := &document.RawElement{Name: "image", Inner: base64.StdEncoding.EncodeToString(png)}
	setBox(el, r)
	return el
}

func setBox(el *document.RawElement, r document.Rect) {
	el.SetAttr("left", fnum(r.Min.X))
	el.SetAttr("top", fnum(r.Min.Y))
	el.SetAttr("right", fnum(r.Max.X))
	el.SetAttr("bottom", fnum(r.Max.Y))
}

// InsertText pastes plain text as a text box in the middle of the view.
func (c *Canvas) InsertText(s string) {
	page, center := c.viewCenter()
	c.addAndSelect(page, document.NewText(center, c.TextSize, c.PenColor, s))
}

func (c *Canvas) addAndSelect(page *document.Page, it document.Item) {
	c.ClearSelection()
	layer := page.TopLayer()
	c.History.Do(c.Doc, &document.AddItem{Page: page, Layer: layer, Item: it})
	c.Tool = Select
	c.sel = newSelection(page, []selItem{{layer, it}})
}

// ---- crop ----

type cropState struct {
	page   *document.Page
	layer  *document.Layer
	el     *document.RawElement
	box    document.Rect // the image's current box
	rect   document.Rect // crop rectangle, inside box
	handle int           // dragged handle, -1 = none
}

// CanCrop reports whether the selection is a single image.
func (c *Canvas) CanCrop() bool {
	if c.sel == nil || len(c.sel.items) != 1 {
		return false
	}
	el, ok := c.sel.items[0].item.(*document.RawElement)
	return ok && el.Name == "image" && el.Image() != nil
}

func (c *Canvas) Cropping() bool { return c.crop != nil }

// StartCrop shows crop handles on the selected image.
func (c *Canvas) StartCrop() {
	if !c.CanCrop() {
		return
	}
	c.bake()
	si := c.sel.items[0]
	el := si.item.(*document.RawElement)
	box, _ := document.ItemBounds(el)
	c.crop = &cropState{page: c.sel.page, layer: si.layer, el: el, box: box, rect: box, handle: -1}
	c.sel = nil
}

func (c *Canvas) CancelCrop() {
	if c.crop == nil {
		return
	}
	cr := c.crop
	c.crop = nil
	c.sel = newSelection(cr.page, []selItem{{cr.layer, cr.el}})
}

// ApplyCrop cuts the image to the crop rectangle (one undo step).
func (c *Canvas) ApplyCrop() {
	cr := c.crop
	if cr == nil {
		return
	}
	c.crop = nil
	if cr.rect == cr.box {
		c.sel = newSelection(cr.page, []selItem{{cr.layer, cr.el}})
		return
	}
	src := cr.el.Image()
	b := src.Bounds()
	bw, bh := cr.box.Max.X-cr.box.Min.X, cr.box.Max.Y-cr.box.Min.Y
	px := func(v, from, size float32, n int) int { return int(float32(n)*(v-from)/size + 0.5) }
	pr := image.Rect(
		b.Min.X+px(cr.rect.Min.X, cr.box.Min.X, bw, b.Dx()), b.Min.Y+px(cr.rect.Min.Y, cr.box.Min.Y, bh, b.Dy()),
		b.Min.X+px(cr.rect.Max.X, cr.box.Min.X, bw, b.Dx()), b.Min.Y+px(cr.rect.Max.Y, cr.box.Min.Y, bh, b.Dy()),
	).Intersect(b)
	if pr.Empty() {
		c.sel = newSelection(cr.page, []selItem{{cr.layer, cr.el}})
		return
	}
	out := image.NewNRGBA(image.Rect(0, 0, pr.Dx(), pr.Dy()))
	draw.Draw(out, out.Bounds(), src, pr.Min, draw.Src)
	var buf bytes.Buffer
	if png.Encode(&buf, out) != nil {
		return
	}
	n := cr.el.Clone()
	n.Inner = base64.StdEncoding.EncodeToString(buf.Bytes())
	setBox(n, cr.rect)
	c.History.Do(c.Doc, &document.Splice{Page: cr.page, Layer: cr.layer, Old: cr.el, New: []document.Item{n}})
	c.sel = newSelection(cr.page, []selItem{{cr.layer, n}})
}

// CropRect is the crop rectangle on screen, for placing the Done/Cancel bar.
func (c *Canvas) CropRect() (image.Rectangle, bool) {
	if c.crop == nil {
		return image.Rectangle{}, false
	}
	a, b := c.toScreen(c.crop.page, c.crop.rect.Min), c.toScreen(c.crop.page, c.crop.rect.Max)
	return image.Rect(int(a.X), int(a.Y), int(b.X), int(b.Y)), true
}

// cropHandles: 4 corners then 4 edge midpoints, in page pt.
func (cr *cropState) handles() [8]f32.Point {
	r := cr.rect
	mx, my := (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2
	return [8]f32.Point{r.Min, {X: r.Max.X, Y: r.Min.Y}, r.Max, {X: r.Min.X, Y: r.Max.Y},
		{X: mx, Y: r.Min.Y}, {X: r.Max.X, Y: my}, {X: mx, Y: r.Max.Y}, {X: r.Min.X, Y: my}}
}

// cropPress starts dragging the handle under the pointer; true if hit.
func (c *Canvas) cropPress(screen f32.Point) bool {
	cr := c.crop
	for i, h := range cr.handles() {
		if d := c.toScreen(cr.page, h).Sub(screen); d.X*d.X+d.Y*d.Y < 16*16 {
			cr.handle = i
			return true
		}
	}
	return false
}

func (c *Canvas) cropDrag(screen f32.Point) {
	cr := c.crop
	if cr.handle < 0 {
		return
	}
	p := c.local(cr.page, screen)
	const minSize = 8
	r, b := cr.rect, cr.box
	left := func() { r.Min.X = max(b.Min.X, min(p.X, r.Max.X-minSize)) }
	right := func() { r.Max.X = min(b.Max.X, max(p.X, r.Min.X+minSize)) }
	top := func() { r.Min.Y = max(b.Min.Y, min(p.Y, r.Max.Y-minSize)) }
	bottom := func() { r.Max.Y = min(b.Max.Y, max(p.Y, r.Min.Y+minSize)) }
	switch cr.handle {
	case 0:
		left()
		top()
	case 1:
		right()
		top()
	case 2:
		right()
		bottom()
	case 3:
		left()
		bottom()
	case 4:
		top()
	case 5:
		right()
	case 6:
		bottom()
	case 7:
		left()
	}
	cr.rect = r
}

// drawCrop dims the parts being cut away and shows the handles.
func (c *Canvas) drawCrop(gtx layout.Context) {
	cr := c.crop
	if cr == nil {
		return
	}
	sp := func(p f32.Point) image.Point {
		s := c.toScreen(cr.page, p)
		return image.Pt(int(s.X), int(s.Y))
	}
	box := image.Rectangle{Min: sp(cr.box.Min), Max: sp(cr.box.Max)}
	in := image.Rectangle{Min: sp(cr.rect.Min), Max: sp(cr.rect.Max)}
	dim := color.NRGBA{0, 0, 0, 0xa0}
	for _, r := range []image.Rectangle{
		{Min: box.Min, Max: image.Pt(box.Max.X, in.Min.Y)},
		{Min: image.Pt(box.Min.X, in.Max.Y), Max: box.Max},
		{Min: image.Pt(box.Min.X, in.Min.Y), Max: image.Pt(in.Min.X, in.Max.Y)},
		{Min: image.Pt(in.Max.X, in.Min.Y), Max: image.Pt(box.Max.X, in.Max.Y)},
	} {
		paint.FillShape(gtx.Ops, dim, clip.Rect(r).Op())
	}
	paint.FillShape(gtx.Ops, Accent, clip.Stroke{Path: clip.Rect(in).Path(), Width: 2}.Op())
	hr := gtx.Dp(6)
	for _, h := range cr.handles() {
		q := sp(h)
		r := image.Rect(q.X-hr, q.Y-hr, q.X+hr, q.Y+hr)
		paint.FillShape(gtx.Ops, Accent, clip.UniformRRect(r.Inset(-1), 3).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, color.NRGBA{255, 255, 255, 255}, clip.UniformRRect(r.Inset(1), 2).Op(gtx.Ops))
	}
}

func fnum(v float32) string {
	return document.FormatNum(v)
}
