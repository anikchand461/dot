// Package export writes notebooks to PDF (vector) and PNG.
package export

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"

	"gioui.org/f32"

	"dot/internal/canvas"
	"dot/internal/document"
)

// PDF writes every page as vector graphics: strokes stay sharp at any
// zoom and print resolution. The PDF is self-contained (base-14
// Helvetica, embedded images), so it opens in any viewer.
func PDF(w io.Writer, d *document.Document) error {
	pw := &pdfWriter{w: bufio.NewWriter(w)}
	pw.raw("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	catalog, pages := pw.reserve(), pw.reserve()
	// The 12 standard PDF fonts cover Sans/Serif/Mono × regular/bold/italic.
	var fonts strings.Builder
	for i, name := range base14 {
		id := pw.reserve()
		pw.obj(id, "<< /Type /Font /Subtype /Type1 /BaseFont /"+name+" /Encoding /WinAnsiEncoding >>")
		fmt.Fprintf(&fonts, " /F%d %d 0 R", i, id)
	}
	// Handwriting fonts are embedded (TrueType) so they look the same in
	// any PDF viewer.
	pw.handFonts = map[string]string{}
	for _, p := range d.Pages {
		for _, l := range p.Layers {
			for _, it := range l.Items {
				e, ok := it.(*document.RawElement)
				if !ok || e.Name != "text" {
					continue
				}
				fam, bold, _ := canvas.ParseFont(e.Attr("font"))
				key := handKey(fam, bold)
				if _, done := pw.handFonts[key]; done || !canvas.IsHandwriting(fam) {
					continue
				}
				name := fmt.Sprintf("H%d", len(pw.handFonts))
				pw.handFonts[key] = name
				fmt.Fprintf(&fonts, " /%s %d 0 R", name, pw.embedTrueType(fam, bold))
			}
		}
	}

	var kids []string
	for _, p := range d.Pages {
		res := &resources{alpha: map[uint8]string{}}
		content := pw.pageContent(p, res)
		cid := pw.stream(content, "")
		var xobj, gs strings.Builder
		for name, id := range res.images {
			fmt.Fprintf(&xobj, " /%s %d 0 R", name, id)
		}
		for a, name := range res.alpha {
			fmt.Fprintf(&gs, " /%s << /Type /ExtGState /ca %.3f /CA %.3f >>", name, float64(a)/255, float64(a)/255)
		}
		pid := pw.reserve()
		pw.obj(pid, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Contents %d 0 R /Resources << /Font <<%s >> /XObject <<%s >> /ExtGState <<%s >> >> >>",
			pages, f(p.Width), f(p.Height), cid, fonts.String(), xobj.String(), gs.String()))
		kids = append(kids, fmt.Sprintf("%d 0 R", pid))
	}
	pw.obj(pages, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(kids), strings.Join(kids, " ")))
	pw.obj(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return pw.finish(catalog)
}

type resources struct {
	images map[string]int
	alpha  map[uint8]string
}

type pdfWriter struct {
	handFonts map[string]string // "Family|bold" -> resource name
	w         *bufio.Writer
	n         int
	offsets   []int
	err       error
}

func (pw *pdfWriter) raw(s string) {
	if pw.err != nil {
		return
	}
	n, err := pw.w.WriteString(s)
	pw.n += n
	pw.err = err
}

func (pw *pdfWriter) reserve() int {
	pw.offsets = append(pw.offsets, 0)
	return len(pw.offsets)
}

func (pw *pdfWriter) obj(id int, body string) {
	pw.offsets[id-1] = pw.n
	pw.raw(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", id, body))
}

// stream writes a Flate-compressed stream object and returns its id.
func (pw *pdfWriter) stream(data []byte, dict string) int {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(data)
	zw.Close()
	id := pw.reserve()
	pw.offsets[id-1] = pw.n
	pw.raw(fmt.Sprintf("%d 0 obj\n<< /Length %d /Filter /FlateDecode%s >>\nstream\n", id, z.Len(), dict))
	pw.raw(z.String())
	pw.raw("\nendstream\nendobj\n")
	return id
}

func (pw *pdfWriter) finish(root int) error {
	xref := pw.n
	pw.raw(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(pw.offsets)+1))
	for _, o := range pw.offsets {
		pw.raw(fmt.Sprintf("%010d 00000 n \n", o))
	}
	pw.raw(fmt.Sprintf("trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(pw.offsets)+1, root, xref))
	if pw.err != nil {
		return pw.err
	}
	return pw.w.Flush()
}

func f(v float32) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

func rgb(c color.NRGBA) string {
	return fmt.Sprintf("%.3f %.3f %.3f", float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
}

func (pw *pdfWriter) pageContent(p *document.Page, res *resources) []byte {
	var b bytes.Buffer
	// Flip to Xournal++'s top-left origin.
	fmt.Fprintf(&b, "1 0 0 -1 0 %s cm\n", f(p.Height))
	if p.Background.Type != "pdf" { // PDF pages stay transparent: the original goes underneath
		fmt.Fprintf(&b, "%s rg 0 0 %s %s re f\n", rgb(p.Background.Color), f(p.Width), f(p.Height))
	}
	if bg := p.Background; bg.Type == "pixmap" && bg.ImagePath != "" {
		if img, err := canvas.LoadBackgroundImage(bg.ImagePath); err == nil {
			// Cover the page (as on screen), clipped to it, then soften.
			x, y, dw, dh := canvas.CoverRect(img.Bounds().Dx(), img.Bounds().Dy(), p.Width, p.Height)
			name := pw.imageXObject(img, res)
			fmt.Fprintf(&b, "q 0 0 %s %s re W n %s 0 0 %s %s %s cm /%s Do Q\n",
				f(p.Width), f(p.Height), f(dw), f(-dh), f(x), f(y+dh), name)
		}
		if bg.Soften > 0 {
			alphaState(&b, uint8(min(0.95, bg.Soften)*255), res)
			fmt.Fprintf(&b, "%s rg 0 0 %s %s re f\n", rgb(bg.Color), f(p.Width), f(p.Height))
			alphaState(&b, 255, res)
		}
	}

	r := p.Ruling()
	lines := func(segs [][2]f32.Point, c color.NRGBA) {
		if len(segs) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s RG %s w\n", rgb(c), f(document.RuleWidth))
		for _, s := range segs {
			fmt.Fprintf(&b, "%s %s m %s %s l\n", f(s[0].X), f(s[0].Y), f(s[1].X), f(s[1].Y))
		}
		b.WriteString("S\n")
	}
	lines(r.Lines, r.LineColor)
	lines(r.Margin, document.MarginColor)
	if len(r.Dots) > 0 {
		fmt.Fprintf(&b, "%s rg\n", rgb(r.DotColor))
		for _, d := range r.Dots {
			circlePath(&b, d, 0.75)
		}
		b.WriteString("f\n")
	}

	for _, l := range p.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			switch it := it.(type) {
			case *document.Stroke:
				strokePath(&b, it, res)
			case *document.RawElement:
				switch it.Name {
				case "text":
					pw.textOps(&b, it)
				case "image":
					pw.imageOps(&b, it, res)
				}
			}
		}
	}
	return b.Bytes()
}

func alphaState(b *bytes.Buffer, a uint8, res *resources) {
	name, ok := res.alpha[a]
	if !ok {
		name = fmt.Sprintf("GA%d", a)
		res.alpha[a] = name
	}
	fmt.Fprintf(b, "/%s gs\n", name)
}

// strokePath writes a stroke: its fill (if any), then each solid piece of
// its dash pattern as PDF line runs with round caps and joins. A pressure
// piece is split into runs whose width stays within ±8%, so the file
// stays compact while the thick/thin shape is preserved.
//
// A translucent stroke (highlighter) is instead filled as its outline, in
// one non-zero fill, as on screen: viewers differ in how they stroke a
// translucent path that crosses itself (many darken every overlap and
// round join), but a single fill paints every spot exactly once.
func strokePath(b *bytes.Buffer, s *document.Stroke, res *resources) {
	if len(s.Points) == 0 {
		return
	}
	if s.Fill.A > 0 && len(s.Points) > 2 {
		b.WriteString("q\n")
		if s.Fill.A < 255 {
			alphaState(b, s.Fill.A, res)
		}
		fmt.Fprintf(b, "%s rg %s %s m", rgb(s.Fill), f(s.Points[0].X), f(s.Points[0].Y))
		for _, p := range s.Points[1:] {
			fmt.Fprintf(b, " %s %s l", f(p.X), f(p.Y))
		}
		b.WriteString(" h f\nQ\n")
	}
	b.WriteString("q\n")
	if s.Color.A < 255 {
		alphaState(b, s.Color.A, res)
		fmt.Fprintf(b, "%s rg\n", rgb(s.Color))
		for _, path := range outline(s) {
			fmt.Fprintf(b, "%s %s m", f(path[0][0]), f(path[0][1]))
			for _, p := range path[1:] {
				fmt.Fprintf(b, " %s %s l", f(p[0]), f(p[1]))
			}
			b.WriteString(" h\n")
		}
		b.WriteString("f\nQ\n")
		return
	}
	fmt.Fprintf(b, "%s RG 1 J 1 j\n", rgb(s.Color))
	for _, ln := range runs(s) {
		fmt.Fprintf(b, "%s w %s %s m", f(ln.Width), f(ln.Points[0][0]), f(ln.Points[0][1]))
		for _, p := range ln.Points[1:] {
			fmt.Fprintf(b, " %s %s l", f(p[0]), f(p[1]))
		}
		b.WriteString(" S\n")
	}
	b.WriteString("Q\n")
}

func circlePath(b *bytes.Buffer, c f32.Point, r float32) {
	const k = 0.5522847
	kr := k * r
	fmt.Fprintf(b, "%s %s m ", f(c.X+r), f(c.Y))
	fmt.Fprintf(b, "%s %s %s %s %s %s c ", f(c.X+r), f(c.Y-kr), f(c.X+kr), f(c.Y-r), f(c.X), f(c.Y-r))
	fmt.Fprintf(b, "%s %s %s %s %s %s c ", f(c.X-kr), f(c.Y-r), f(c.X-r), f(c.Y-kr), f(c.X-r), f(c.Y))
	fmt.Fprintf(b, "%s %s %s %s %s %s c ", f(c.X-r), f(c.Y+kr), f(c.X-kr), f(c.Y+r), f(c.X), f(c.Y+r))
	fmt.Fprintf(b, "%s %s %s %s %s %s c h\n", f(c.X+kr), f(c.Y+r), f(c.X+r), f(c.Y+kr), f(c.X+r), f(c.Y))
}

func (pw *pdfWriter) textOps(b *bytes.Buffer, e *document.RawElement) {
	size := e.Float("size")
	if size <= 0 {
		size = 12
	}
	x, y := e.Float("x"), e.Float("y")
	res := fmt.Sprintf("F%d", fontIndex(e.Attr("font")))
	if fam, bold, _ := canvas.ParseFont(e.Attr("font")); canvas.IsHandwriting(fam) {
		if n, ok := pw.handFonts[handKey(fam, bold)]; ok {
			res = n
		}
	}
	fmt.Fprintf(b, "BT %s rg /%s %s Tf\n", rgb(e.Color()), res, f(size))
	for i, ln := range textLines(e, size) {
		// The text matrix flips y back so glyphs are upright.
		fmt.Fprintf(b, "1 0 0 -1 %s %s Tm (%s) Tj\n", f(x+ln.dx), f(y+size*0.9+float32(i)*size*1.15), pdfString(ln.text))
	}
	b.WriteString("ET\n")
}

type textLine struct {
	text string
	dx   float32 // offset from the text's x (centring)
}

// textLines splits a text element into lines. Labels inside shapes
// (inkWidth) are word-wrapped to that width and centred, as on screen.
func textLines(e *document.RawElement, size float32) []textLine {
	var out []textLine
	w := e.Float("inkWidth")
	font := e.Attr("font")
	for _, para := range strings.Split(e.Text(), "\n") {
		if w <= 0 {
			out = append(out, textLine{text: para})
			continue
		}
		cur := ""
		flush := func() {
			out = append(out, textLine{text: cur, dx: (w - canvas.MeasureLine(font, size, cur)) / 2})
			cur = ""
		}
		for _, word := range strings.Fields(para) {
			try := word
			if cur != "" {
				try = cur + " " + word
			}
			if cur != "" && canvas.MeasureLine(font, size, try) > w {
				flush()
				cur = word
			} else {
				cur = try
			}
		}
		flush()
	}
	return out
}

// pdfString escapes s for a WinAnsi literal string; characters outside
// Latin-1 become '?'.
func pdfString(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case r < 32:
			out.WriteByte(' ')
		case r < 256:
			fmt.Fprintf(&out, "\\%03o", r)
		case winAnsi[r] != 0:
			fmt.Fprintf(&out, "\\%03o", winAnsi[r])
		default:
			out.WriteByte('?')
		}
	}
	return out.String()
}

func (pw *pdfWriter) imageOps(b *bytes.Buffer, e *document.RawElement, res *resources) {
	img := e.Image()
	if img == nil {
		return
	}
	l, t, r, btm := e.Float("left"), e.Float("top"), e.Float("right"), e.Float("bottom")
	if r <= l || btm <= t {
		return
	}
	name := pw.imageXObject(img, res)
	// Unit square -> image box; v=1 is the image's top row.
	fmt.Fprintf(b, "q %s 0 0 %s %s %s cm /%s Do Q\n", f(r-l), f(-(btm - t)), f(l), f(btm), name)
}

// imageXObject embeds img (with an alpha mask when needed) and returns its
// resource name.
func (pw *pdfWriter) imageXObject(img image.Image, res *resources) string {
	bounds := img.Bounds()
	rgbData := make([]byte, 0, bounds.Dx()*bounds.Dy()*3)
	alpha := make([]byte, 0, bounds.Dx()*bounds.Dy())
	opaque := true
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			rgbData = append(rgbData, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			opaque = opaque && c.A == 255
		}
	}
	dims := fmt.Sprintf(" /Type /XObject /Subtype /Image /Width %d /Height %d /BitsPerComponent 8", bounds.Dx(), bounds.Dy())
	smask := ""
	if !opaque {
		sid := pw.stream(alpha, dims+" /ColorSpace /DeviceGray")
		smask = fmt.Sprintf(" /SMask %d 0 R", sid)
	}
	id := pw.stream(rgbData, dims+" /ColorSpace /DeviceRGB"+smask)
	if res.images == nil {
		res.images = map[string]int{}
	}
	name := fmt.Sprintf("Im%d", id)
	res.images[name] = id
	return name
}

var base14 = []string{
	"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
	"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
	"Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
}

// fontIndex picks the base14 font matching a Xournal++ font attribute.
func fontIndex(attr string) int {
	family, bold, italic := canvas.ParseFont(attr)
	i := map[string]int{"Sans": 0, "Serif": 4, "Mono": 8}[family]
	if bold {
		i++
	}
	if italic {
		i += 2
	}
	return i
}

// winAnsi maps the typographic characters WinAnsiEncoding has in
// 0x80–0x9F (dashes, curly quotes, bullet, ellipsis, euro, ...).
var winAnsi = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88,
	'‰': 0x89, 'Š': 0x8a, '‹': 0x8b, 'Œ': 0x8c, 'Ž': 0x8e, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9a, '›': 0x9b,
	'œ': 0x9c, 'ž': 0x9e, 'Ÿ': 0x9f,
}

func handKey(fam string, bold bool) string { return fmt.Sprintf("%s|%v", fam, bold) }

// embedTrueType writes a simple TrueType font (WinAnsi encoding) with
// its glyph widths and returns the font object id.
func (pw *pdfWriter) embedTrueType(fam string, bold bool) int {
	ttf := canvas.FontFile(fam, bold)
	attr := canvas.FontAttr(fam, bold, false)
	ps := strings.ReplaceAll(fam, " ", "")
	if bold {
		ps += "-Bold"
	}
	file := pw.stream(ttf, fmt.Sprintf(" /Length1 %d", len(ttf)))
	desc := pw.reserve()
	pw.obj(desc, fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 32 /FontBBox [-300 -400 1300 1100] "+
		"/ItalicAngle 0 /Ascent 900 /Descent -300 /CapHeight 700 /StemV 80 /FontFile2 %d 0 R >>", ps, file))
	var widths strings.Builder
	for c := 32; c <= 255; c++ {
		r := rune(c)
		if c >= 128 && c < 160 {
			r = 0
			for k, v := range winAnsi {
				if int(v) == c {
					r = k
				}
			}
		}
		w := float32(0)
		if r != 0 {
			w = canvas.MeasureLine(attr, 1000, string(r))
		}
		fmt.Fprintf(&widths, " %d", int(w+0.5))
	}
	id := pw.reserve()
	pw.obj(id, fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /%s /FirstChar 32 /LastChar 255 "+
		"/Widths [%s ] /Encoding /WinAnsiEncoding /FontDescriptor %d 0 R >>", ps, widths.String(), desc))
	return id
}
