package document

import (
	"bytes"
	"encoding/xml"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/f32"
)

// Text elements stay RawElements so unknown attributes survive; these
// helpers read and build the Xournal++ <text> form:
//
//	<text font="Sans" size="12" x="188.3" y="290.8" color="#00c0ffff">Hello</text>

func NewText(pos f32.Point, size float32, c color.NRGBA, s string) *RawElement {
	r := &RawElement{Name: "text"}
	r.SetAttr("font", "Sans")
	r.SetAttr("size", num(size))
	r.SetAttr("x", num(pos.X))
	r.SetAttr("y", num(pos.Y))
	r.SetAttr("color", colorString(c))
	r.SetText(s)
	return r
}

func (r *RawElement) SetAttr(name, value string) {
	for i, a := range r.Attrs {
		if a.Name.Local == name {
			r.Attrs[i].Value = value
			return
		}
	}
	r.Attrs = append(r.Attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

func (r *RawElement) Float(name string) float32 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(r.Attr(name)), 32)
	return float32(v)
}

func (r *RawElement) Color() color.NRGBA {
	c, ok := parseColor(r.Attr("color"))
	if !ok {
		return Black
	}
	return c
}

// Text returns the unescaped character data of the element.
func (r *RawElement) Text() string {
	var out strings.Builder
	d := xml.NewDecoder(strings.NewReader("<t>" + r.Inner + "</t>"))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			out.Write(cd)
		}
	}
	return out.String()
}

func (r *RawElement) SetText(s string) {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	r.Inner = b.String()
}

// Clone copies the element (not its decoded image cache).
func (r *RawElement) Clone() *RawElement {
	return &RawElement{Name: r.Name, Attrs: append([]xml.Attr(nil), r.Attrs...), Inner: r.Inner}
}

// ItemBounds returns an item's bounding box in page pt. Text size is
// estimated from its font size (no shaper here), which is close enough
// for hit-testing and selection.
func ItemBounds(it Item) (Rect, bool) {
	switch it := it.(type) {
	case *Stroke:
		return it.Bounds(), len(it.Points) > 0
	case *RawElement:
		switch it.Name {
		case "image", "teximage":
			r := Rect{f32.Pt(it.Float("left"), it.Float("top")), f32.Pt(it.Float("right"), it.Float("bottom"))}
			return r, r.Max.X > r.Min.X
		case "text":
			size := it.Float("size")
			lines := strings.Split(it.Text(), "\n")
			longest := 0
			for _, l := range lines {
				longest = max(longest, len([]rune(l)))
			}
			p := f32.Pt(it.Float("x"), it.Float("y"))
			if w := it.Float("inkWidth"); w > 0 { // wrapped label: estimate wrapped lines
				n := 0
				for _, l := range lines {
					n += max(1, int(math.Ceil(float64(float32(len([]rune(l)))*size*0.56/w))))
				}
				return Rect{p, p.Add(f32.Pt(w, float32(n)*size*1.2))}, true
			}
			return Rect{p, p.Add(f32.Pt(float32(max(longest, 1))*size*0.56, float32(len(lines))*size*1.25))}, true
		}
	}
	return Rect{}, false
}

// Transformed returns a copy of it moved/scaled by m; widths and font
// sizes scale by s.
func Transformed(it Item, m f32.Affine2D, s float32) Item {
	switch it := it.(type) {
	case *Stroke:
		c := it.Clone()
		for i, p := range c.Points {
			c.Points[i] = m.Transform(p)
		}
		c.Width *= s
		for i := range c.Widths {
			c.Widths[i] *= s
		}
		for i, p := range c.Ctrl {
			c.Ctrl[i] = m.Transform(p)
		}
		c.boundsOK = false
		return c
	case *RawElement:
		c := it.Clone()
		switch c.Name {
		case "text":
			p := m.Transform(f32.Pt(c.Float("x"), c.Float("y")))
			c.SetAttr("x", num(p.X))
			c.SetAttr("y", num(p.Y))
			c.SetAttr("size", num(c.Float("size")*s))
			if w := c.Float("inkWidth"); w > 0 {
				c.SetAttr("inkWidth", num(w*s))
			}
		case "image", "teximage":
			a := m.Transform(f32.Pt(c.Float("left"), c.Float("top")))
			b := m.Transform(f32.Pt(c.Float("right"), c.Float("bottom")))
			c.SetAttr("left", num(a.X))
			c.SetAttr("top", num(a.Y))
			c.SetAttr("right", num(b.X))
			c.SetAttr("bottom", num(b.Y))
			// The decoded image is reused; only the box changed.
			if img := it.Image(); img != nil {
				c.imgOnce.Do(func() { c.img = img })
			}
		}
		return c
	}
	return it
}

// Recolored returns a copy of it in colour c (highlighters keep their alpha).
func Recolored(it Item, c color.NRGBA) Item {
	switch it := it.(type) {
	case *Stroke:
		n := it.Clone()
		c.A = it.Color.A
		n.Color = c
		return n
	case *RawElement:
		if it.Name == "text" {
			n := it.Clone()
			n.SetAttr("color", colorString(c))
			return n
		}
	}
	return it
}

// Piece is a solid run of a styled stroke.
type Piece struct {
	Points []f32.Point
	Widths []float32
	Dot    bool // a single dot (dotted lines)
}

// Pieces splits a dashed/dotted stroke into solid pieces along its length.
// Dash lengths scale with the line width so patterns look right at any
// thickness. Solid strokes return themselves as one piece.
func (s *Stroke) Pieces() []Piece {
	// Arrowheads stay solid on dashed/dotted arrows: pattern only the body.
	if s.Style != "" && len(s.Ctrl) == 3 && (s.Shape == "Arrow" || s.Shape == "Double arrow") {
		const body = 33 // points in an editable curve's body (ink.Curve)
		lo := 0
		if s.Shape == "Double arrow" {
			lo = 4
		}
		if len(s.Points) >= lo+body {
			solid := func(from, to int) Piece {
				ws := make([]float32, 0, to-from)
				for i := from; i < to; i++ {
					ws = append(ws, s.WidthAt(i))
				}
				return Piece{Points: s.Points[from:to], Widths: ws}
			}
			b := *s
			b.Points, b.Widths, b.Ctrl = s.Points[lo:lo+body], nil, nil
			if s.Widths != nil {
				b.Widths = s.Widths[lo : lo+body]
			}
			out := b.Pieces()
			if lo > 0 {
				out = append(out, solid(0, lo))
			}
			if lo+body < len(s.Points) {
				out = append(out, solid(lo+body-1, len(s.Points)))
			}
			return out
		}
	}
	if s.Style == "" || s.Style == "plain" || len(s.Points) < 2 {
		ws := make([]float32, len(s.Points))
		for i := range ws {
			ws[i] = s.WidthAt(i)
		}
		return []Piece{{Points: s.Points, Widths: ws}}
	}
	w := max(s.Width, 0.6)
	var pattern []float32 // on, off, on, off... (a 0 "on" is a dot)
	switch s.Style {
	case "dot":
		pattern = []float32{0, 2.6 * w}
	case "dashdot":
		pattern = []float32{4 * w, 2.4 * w, 0, 2.4 * w}
	default: // "dash" and Xournal++ custom patterns
		pattern = []float32{4.5 * w, 3 * w}
	}
	var out []Piece
	var cur Piece
	k, left := 0, pattern[0]
	on := true
	emit := func() {
		if len(cur.Points) > 1 {
			out = append(out, cur)
		}
		cur = Piece{}
	}
	if pattern[0] == 0 {
		out = append(out, Piece{Points: []f32.Point{s.Points[0]}, Widths: []float32{s.WidthAt(0)}, Dot: true})
		k, left, on = 1, pattern[1], false
	} else {
		cur.Points, cur.Widths = []f32.Point{s.Points[0]}, []float32{s.WidthAt(0)}
	}
	for i := 0; i+1 < len(s.Points); i++ {
		a, b := s.Points[i], s.Points[i+1]
		wa, wb := s.WidthAt(i), s.WidthAt(i+1)
		seg := b.Sub(a)
		l := float32(math.Hypot(float64(seg.X), float64(seg.Y)))
		t := float32(0)
		for l > 0 && (l-t) >= left {
			t += left
			p, pw := a.Add(seg.Mul(t/l)), wa+(wb-wa)*t/l
			if on {
				cur.Points, cur.Widths = append(cur.Points, p), append(cur.Widths, pw)
				emit()
			}
			k = (k + 1) % len(pattern)
			left, on = pattern[k], !on
			if on && left == 0 { // a dot
				out = append(out, Piece{Points: []f32.Point{p}, Widths: []float32{pw}, Dot: true})
				k = (k + 1) % len(pattern)
				left, on = pattern[k], false
			} else if on {
				cur.Points, cur.Widths = []f32.Point{p}, []float32{pw}
			}
		}
		left -= l - t
		if on {
			cur.Points, cur.Widths = append(cur.Points, b), append(cur.Widths, wb)
		}
	}
	if on {
		emit()
	}
	return out
}
