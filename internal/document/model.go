// Package document holds the notebook data model. Units are PostScript
// points (1/72 inch), the same as Xournal++, so files round-trip 1:1.
package document

import (
	"encoding/base64"
	"encoding/xml"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gioui.org/f32"
)

// Tool identifies how a stroke was made. Values match Xournal++'s tool="..." attribute.
type Tool string

const (
	ToolPen         Tool = "pen"
	ToolHighlighter Tool = "highlighter"
)

// Stroke is an immutable-once-committed ink stroke.
type Stroke struct {
	Tool   Tool
	Color  color.NRGBA
	Width  float32     // nominal width in pt
	Points []f32.Point // page-local coordinates in pt
	Widths []float32   // optional per-point width (pressure); nil = constant Width
	// Fill paints the inside of the (closed) stroke; Fill.A == 0 means none.
	Fill color.NRGBA
	// Style is the line pattern: "" (solid), "dash", "dot" or "dashdot".
	Style string
	// Editable lines/arrows: the shape name ("Line", "Arrow", "Double
	// arrow") and its start, bend control and end points. Points is
	// rebuilt from these when the curve is bent.
	Shape string
	Ctrl  []f32.Point
	Extra []xml.Attr // unknown attributes (capStyle, ts, fn, ...) kept for round-trip

	boundsOK bool
	bounds   Rect
}

// WidthAt returns the rendered width at point i.
func (s *Stroke) WidthAt(i int) float32 {
	if i < len(s.Widths) {
		return s.Widths[i]
	}
	return s.Width
}

// Bounds returns the stroke's bounding box including its half-width.
func (s *Stroke) Bounds() Rect {
	if s.boundsOK {
		return s.bounds
	}
	if len(s.Points) == 0 {
		return Rect{}
	}
	r := Rect{Min: s.Points[0], Max: s.Points[0]}
	var maxW float32
	for i, p := range s.Points {
		r.Min.X, r.Min.Y = min(r.Min.X, p.X), min(r.Min.Y, p.Y)
		r.Max.X, r.Max.Y = max(r.Max.X, p.X), max(r.Max.Y, p.Y)
		maxW = max(maxW, s.WidthAt(i))
	}
	h := maxW / 2
	s.bounds = Rect{Min: r.Min.Sub(f32.Pt(h, h)), Max: r.Max.Add(f32.Pt(h, h))}
	s.boundsOK = true
	return s.bounds
}

// Invalidate drops cached geometry after Points change.
func (s *Stroke) Invalidate() { s.boundsOK = false }

func (s *Stroke) Clone() *Stroke {
	c := *s
	c.Points = append([]f32.Point(nil), s.Points...)
	if s.Widths != nil {
		c.Widths = append([]float32(nil), s.Widths...)
	}
	c.Extra = append([]xml.Attr(nil), s.Extra...)
	c.Ctrl = append([]f32.Point(nil), s.Ctrl...)
	return &c
}

// RawElement is a layer element this app does not edit (text, image,
// teximage, link). It is written back verbatim so nothing is lost.
type RawElement struct {
	Name  string
	Attrs []xml.Attr
	Inner string // raw inner XML

	imgOnce sync.Once
	img     image.Image
}

func (r *RawElement) Attr(name string) string {
	for _, a := range r.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// Image decodes the base64 PNG/JPEG payload of an <image> element, once.
func (r *RawElement) Image() image.Image {
	if r.Name != "image" {
		return nil
	}
	r.imgOnce.Do(func() {
		dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimSpace(r.Inner)))
		if img, _, err := image.Decode(dec); err == nil {
			r.img = img
		}
	})
	return r.img
}

// Item is either *Stroke or *RawElement.
type Item interface{}

type Layer struct {
	Name   string
	Hidden bool
	Items  []Item
}

// Style names are Xournal++'s background style="..." values.
type Style string

const (
	StylePlain  Style = "plain"
	StyleRuled  Style = "ruled"  // horizontal lines
	StyleLined  Style = "lined"  // horizontal lines + margin
	StyleGraph  Style = "graph"  // square grid
	StyleDotted Style = "dotted" // dot grid
)

var Styles = []Style{StylePlain, StyleRuled, StyleLined, StyleGraph, StyleDotted}

type Background struct {
	Type  string // "solid", "pdf", "pixmap"
	Style Style
	Color color.NRGBA
	// PDF backgrounds: the file and 0-based page shown behind the ink.
	PDFPath string
	PDFPage int
	// Image backgrounds (Xournal++ "pixmap"): the picture fills the page;
	// Soften (0–0.9) lays the paper colour over it so ink stays readable.
	ImagePath string
	Soften    float32
	// Raw keeps the original attributes. Non-solid backgrounds (PDF,
	// pixmap) are written back from Raw unchanged.
	Raw []xml.Attr
}

type Page struct {
	Width, Height float32
	Background    Background
	Layers        []*Layer
	// Version increments on every change; renderers use it to invalidate caches.
	Version uint64
}

func (p *Page) Touch() { p.Version++ }

// TopLayer returns the layer new ink goes onto, creating one if needed.
func (p *Page) TopLayer() *Layer {
	if len(p.Layers) == 0 {
		p.Layers = append(p.Layers, &Layer{})
	}
	return p.Layers[len(p.Layers)-1]
}

func (p *Page) Clone() *Page {
	c := &Page{Width: p.Width, Height: p.Height, Background: p.Background}
	c.Background.Raw = append([]xml.Attr(nil), p.Background.Raw...)
	for _, l := range p.Layers {
		nl := &Layer{Name: l.Name, Hidden: l.Hidden}
		for _, it := range l.Items {
			if s, ok := it.(*Stroke); ok {
				nl.Items = append(nl.Items, s.Clone())
			} else {
				nl.Items = append(nl.Items, it)
			}
		}
		c.Layers = append(c.Layers, nl)
	}
	return c
}

type Document struct {
	Pages []*Page
	Dirty bool
	// Edits counts changes; a background save remembers the count it
	// captured so later edits are not marked saved.
	Edits uint64
}

// Snapshot returns a copy that a background save can read while editing
// continues. It is cheap: strokes, text and images are never modified
// in place (edits replace them), so only the page and layer lists are
// copied.
func (d *Document) Snapshot() *Document {
	c := &Document{Pages: make([]*Page, len(d.Pages)), Edits: d.Edits}
	for i, p := range d.Pages {
		cp := *p
		cp.Layers = make([]*Layer, len(p.Layers))
		for j, l := range p.Layers {
			cl := *l
			cl.Items = append([]Item(nil), l.Items...)
			cp.Layers[j] = &cl
		}
		c.Pages[i] = &cp
	}
	return c
}

// PaperSize is a named page size in pt, portrait orientation.
type PaperSize struct {
	Name string
	W, H float32
}

var PaperSizes = []PaperSize{
	{"A4", 595.27559, 841.88976},
	{"A5", 419.52756, 595.27559},
	{"Letter", 612, 792},
	{"Legal", 612, 1008},
	{"Tabloid", 792, 1224},
}

// MM converts millimetres to points.
func MM(v float32) float32 { return v * 72 / 25.4 }

var (
	White = color.NRGBA{255, 255, 255, 255}
	Black = color.NRGBA{0, 0, 0, 255}
)

func NewPage(w, h float32, style Style, paper color.NRGBA) *Page {
	return &Page{
		Width: w, Height: h,
		Background: Background{Type: "solid", Style: style, Color: paper},
		Layers:     []*Layer{{}},
	}
}

func New() *Document {
	a4 := PaperSizes[0]
	return &Document{Pages: []*Page{NewPage(a4.W, a4.H, StyleLined, White)}}
}

// Rect is an axis-aligned rectangle in pt.
type Rect struct{ Min, Max f32.Point }

func (r Rect) Contains(p f32.Point) bool {
	return p.X >= r.Min.X && p.X <= r.Max.X && p.Y >= r.Min.Y && p.Y <= r.Max.Y
}

// Expand grows r by d on every side.
func (r Rect) Expand(d float32) Rect {
	return Rect{r.Min.Sub(f32.Pt(d, d)), r.Max.Add(f32.Pt(d, d))}
}

// StyleOr returns the ruling style, or def for PDF/image backgrounds.
func (b Background) StyleOr(def Style) Style {
	if b.Type == "solid" {
		return b.Style
	}
	return def
}

// NewPDFDocument makes a notebook to annotate a PDF: one page per PDF
// page, each the PDF page's size with that page as its background.
func NewPDFDocument(path string, sizes [][2]float32) *Document {
	d := &Document{}
	for i, sz := range sizes {
		d.Pages = append(d.Pages, &Page{
			Width: sz[0], Height: sz[1],
			Background: Background{Type: "pdf", Color: White, PDFPath: path, PDFPage: i},
			Layers:     []*Layer{{}},
		})
	}
	return d
}

// ResolvePDFs turns PDF background file names stored relative to the
// notebook (Xournal++'s "attach" domain) into absolute paths.
func (d *Document) ResolvePDFs(xoppPath string) {
	for _, p := range d.Pages {
		b := &p.Background
		if b.Type == "pixmap" && b.ImagePath != "" && !filepath.IsAbs(b.ImagePath) {
			// Xournal++ attaches images beside the file as <file>.<name>.
			cand := filepath.Join(filepath.Dir(xoppPath), b.ImagePath)
			if alt := xoppPath + "." + b.ImagePath; fileExists(alt) {
				cand = alt
			}
			b.ImagePath = cand
		}
		if b.Type != "pdf" || b.PDFPath == "" || filepath.IsAbs(b.PDFPath) {
			continue
		}
		if b.PDFPath == "bg.pdf" { // attached: <notebook>.xopp.bg.pdf
			b.PDFPath = xoppPath + ".bg.pdf"
		} else {
			b.PDFPath = filepath.Join(filepath.Dir(xoppPath), b.PDFPath)
		}
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Snapshot returns a copy of p safe to draw on another goroutine (items
// themselves are never modified in place).
func (p *Page) Snapshot() *Page {
	cp := *p
	cp.Layers = make([]*Layer, len(p.Layers))
	for j, l := range p.Layers {
		cl := *l
		cl.Items = append([]Item(nil), l.Items...)
		cp.Layers[j] = &cl
	}
	return &cp
}
