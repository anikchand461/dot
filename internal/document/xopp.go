package document

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"image/color"
	"io"
	"strconv"
	"strings"

	"gioui.org/f32"
)

// The .xopp format is gzipped XML (see Xournal++ SaveHandler.cpp):
//
//	<xournal creator="..." fileversion="4">
//	  <title>...</title>
//	  <page width="595.27559" height="841.88976">
//	    <background type="solid" color="#ffffffff" style="graph"/>
//	    <layer>
//	      <stroke tool="pen" color="#000000ff" width="2.26 0.70 0.72 ...">x0 y0 x1 y1 ...</stroke>
//
// With pressure, width holds the nominal width followed by one absolute
// width per segment (len(points)-1 values).

const creator = "dot! 0.3"

type xDoc struct {
	XMLName xml.Name `xml:"xournal"`
	Pages   []xPage  `xml:"page"`
}

type xPage struct {
	Width      float32  `xml:"width,attr"`
	Height     float32  `xml:"height,attr"`
	Background xAttrs   `xml:"background"`
	Layers     []xLayer `xml:"layer"`
}

type xAttrs struct {
	Attrs []xml.Attr `xml:",any,attr"`
}

type xLayer struct {
	Name  string `xml:"name,attr"`
	Items []xAny `xml:",any"`
}

type xAny struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Inner   string     `xml:",innerxml"`
}

// Load reads a .xopp (gzipped) or plain XML Xournal/Xournal++ file.
func Load(r io.Reader) (*Document, error) {
	br := bufio.NewReader(r)
	var src io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		src = gz
	}
	var x xDoc
	if err := xml.NewDecoder(src).Decode(&x); err != nil {
		return nil, fmt.Errorf("parse xopp: %w", err)
	}
	d := &Document{}
	pdfFile := "" // Xournal++ names the PDF only on its first PDF page
	for _, xp := range x.Pages {
		p := &Page{Width: xp.Width, Height: xp.Height, Background: parseBackground(xp.Background.Attrs)}
		if b := &p.Background; b.Type == "pdf" {
			if b.PDFPath != "" {
				pdfFile = b.PDFPath
			}
			b.PDFPath = pdfFile
		}
		for _, xl := range xp.Layers {
			l := &Layer{Name: xl.Name}
			for _, it := range xl.Items {
				if it.XMLName.Local == "stroke" {
					if s, err := parseStroke(it); err == nil {
						l.Items = append(l.Items, s)
						continue
					}
				}
				l.Items = append(l.Items, &RawElement{Name: it.XMLName.Local, Attrs: it.Attrs, Inner: it.Inner})
			}
			p.Layers = append(p.Layers, l)
		}
		d.Pages = append(d.Pages, p)
	}
	if len(d.Pages) == 0 {
		return New(), nil
	}
	return d, nil
}

func parseBackground(attrs []xml.Attr) (b Background) {
	b = Background{Type: "solid", Style: StylePlain, Color: White, Raw: attrs}
	defer func() {
		if b.Type != "pdf" {
			b.PDFPath = ""
		}
		if b.Type != "pixmap" {
			b.ImagePath = ""
		}
	}()
	for _, a := range attrs {
		switch a.Name.Local {
		case "type":
			b.Type = a.Value
		case "style":
			b.Style = Style(a.Value)
		case "color":
			if c, ok := parseColor(a.Value); ok {
				b.Color = c
			}
		case "filename":
			b.PDFPath = a.Value
			b.ImagePath = a.Value
		case "soften":
			if v, err := strconv.ParseFloat(a.Value, 32); err == nil {
				b.Soften = float32(v)
			}
		case "pageno":
			if n, err := strconv.Atoi(a.Value); err == nil {
				b.PDFPage = n - 1
			}
		}
	}
	return b
}

func parseStroke(x xAny) (*Stroke, error) {
	s := &Stroke{Tool: ToolPen, Color: Black, Width: 1}
	var widths []float32
	fillAlpha, fillColor := -1, ""
	for _, a := range x.Attrs {
		switch a.Name.Local {
		case "fill": // Xournal++: fill alpha 0–255 in the stroke's colour
			if v, err := strconv.Atoi(a.Value); err == nil {
				fillAlpha = v
			}
		case "fillColor": // dot!: a fill colour of its own
			fillColor = a.Value
		case "style":
			s.Style = a.Value
		case "inkShape":
			s.Shape = a.Value
		case "inkCtrl":
			var v []float32
			for _, f := range strings.Fields(a.Value) {
				x, err := strconv.ParseFloat(f, 32)
				if err != nil {
					v = nil
					break
				}
				v = append(v, float32(x))
			}
			for i := 0; i+1 < len(v); i += 2 {
				s.Ctrl = append(s.Ctrl, f32.Pt(v[i], v[i+1]))
			}
		case "tool":
			s.Tool = Tool(a.Value)
		case "color":
			if c, ok := parseColor(a.Value); ok {
				s.Color = c
			}
		case "width":
			for _, f := range strings.Fields(a.Value) {
				v, err := strconv.ParseFloat(f, 32)
				if err != nil {
					return nil, err
				}
				widths = append(widths, float32(v))
			}
		default:
			s.Extra = append(s.Extra, a)
		}
	}
	if len(widths) > 0 {
		s.Width = widths[0]
	}
	if c, ok := parseColor(fillColor); ok {
		s.Fill = c
	} else if fillAlpha > 0 {
		s.Fill = s.Color
		s.Fill.A = uint8(min(255, fillAlpha))
	}
	nums := strings.Fields(x.Inner)
	if len(nums) < 2 {
		return nil, fmt.Errorf("stroke without points")
	}
	s.Points = make([]f32.Point, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		px, err1 := strconv.ParseFloat(nums[i], 32)
		py, err2 := strconv.ParseFloat(nums[i+1], 32)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad point %q %q", nums[i], nums[i+1])
		}
		s.Points = append(s.Points, f32.Pt(float32(px), float32(py)))
	}
	// Segment widths -> per-point widths: point i uses segment i's width,
	// the last point repeats the last segment.
	if seg := widths[min(1, len(widths)):]; len(widths) > 1 {
		s.Widths = make([]float32, len(s.Points))
		for i := range s.Widths {
			s.Widths[i] = seg[min(i, len(seg)-1)]
		}
	}
	return s, nil
}

var namedColors = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "blue": {0x33, 0x33, 0xcc, 255}, "red": {0xff, 0, 0, 255},
	"green": {0, 0x80, 0, 255}, "gray": {0x80, 0x80, 0x80, 255}, "lightblue": {0, 0xc0, 0xff, 255},
	"lightgreen": {0, 0xff, 0, 255}, "magenta": {0xff, 0, 0xff, 255}, "orange": {0xff, 0x80, 0, 255},
	"yellow": {0xff, 0xff, 0, 255}, "white": {0xff, 0xff, 0xff, 255},
}

func parseColor(s string) (color.NRGBA, bool) {
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	s = strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	switch len(s) {
	case 8:
		return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	case 6:
		return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
	}
	return color.NRGBA{}, false
}

func colorString(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

func num(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }

// Save writes d as a gzipped .xopp file that Xournal++ can open.
func Save(w io.Writer, d *Document) error {
	// Fast compression: ~4x quicker than the default for a few % size.
	gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err := Encode(gz, d); err != nil {
		return err
	}
	return gz.Close()
}

// Encode writes the uncompressed XML.
func Encode(w io.Writer, d *Document) error {
	b := bufio.NewWriterSize(w, 1<<16)
	fmt.Fprintf(b, "<?xml version=\"1.0\" standalone=\"no\"?>\n<xournal creator=\"%s\" fileversion=\"4\">\n", creator)
	b.WriteString("<title>Xournal++ document - see https://xournalpp.github.io/</title>\n")
	lastPDF := ""
	for _, p := range d.Pages {
		fmt.Fprintf(b, "<page width=\"%s\" height=\"%s\">\n", num(p.Width), num(p.Height))
		if bg := p.Background; bg.Type == "pdf" && bg.PDFPath != "" {
			// Attribute order matters to Xournal++; the file is named on
			// the first page of each run that uses it.
			b.WriteString("<background")
			writeAttr(b, "type", "pdf")
			if bg.PDFPath != lastPDF {
				writeAttr(b, "domain", "absolute")
				writeAttr(b, "filename", bg.PDFPath)
				lastPDF = bg.PDFPath
			}
			writeAttr(b, "pageno", strconv.Itoa(bg.PDFPage+1))
			b.WriteString("/>\n")
		} else {
			writeBackground(b, p.Background)
		}
		if len(p.Layers) == 0 {
			b.WriteString("<layer/>\n")
		}
		for _, l := range p.Layers {
			b.WriteString("<layer")
			if l.Name != "" {
				writeAttr(b, "name", l.Name)
			}
			b.WriteString(">\n")
			for _, it := range l.Items {
				switch it := it.(type) {
				case *Stroke:
					writeStroke(b, it)
				case *RawElement:
					b.WriteString("<" + it.Name)
					for _, a := range it.Attrs {
						writeAttr(b, a.Name.Local, a.Value)
					}
					b.WriteString(">" + it.Inner + "</" + it.Name + ">\n")
				}
			}
			b.WriteString("</layer>\n")
		}
		b.WriteString("</page>\n")
	}
	b.WriteString("</xournal>\n")
	return b.Flush()
}

func writeAttr(b *bufio.Writer, name, value string) {
	var esc bytes.Buffer
	xml.EscapeText(&esc, []byte(value))
	b.WriteString(" " + name + "=\"")
	b.Write(esc.Bytes())
	b.WriteString("\"")
}

func writeBackground(b *bufio.Writer, bg Background) {
	b.WriteString("<background")
	if bg.Type == "pixmap" && bg.ImagePath != "" {
		writeAttr(b, "type", "pixmap")
		writeAttr(b, "domain", "absolute")
		writeAttr(b, "filename", bg.ImagePath)
		if bg.Soften > 0 {
			writeAttr(b, "soften", num(bg.Soften))
		}
		b.WriteString("/>\n")
		return
	}
	if bg.Type != "solid" {
		// PDF/pixmap: attribute order matters to Xournal++; keep it verbatim.
		for _, a := range bg.Raw {
			writeAttr(b, a.Name.Local, a.Value)
		}
		b.WriteString("/>\n")
		return
	}
	for _, a := range bg.Raw {
		if a.Name.Local == "name" {
			writeAttr(b, "name", a.Value)
		}
	}
	writeAttr(b, "type", "solid")
	writeAttr(b, "color", colorString(bg.Color))
	writeAttr(b, "style", string(bg.Style))
	for _, a := range bg.Raw {
		if a.Name.Local == "config" && string(bg.Style) == rawStyle(bg.Raw) {
			writeAttr(b, "config", a.Value)
		}
	}
	b.WriteString("/>\n")
}

func rawStyle(attrs []xml.Attr) string {
	for _, a := range attrs {
		if a.Name.Local == "style" {
			return a.Value
		}
	}
	return ""
}

func writeStroke(b *bufio.Writer, s *Stroke) {
	b.WriteString("<stroke")
	writeAttr(b, "tool", string(s.Tool))
	writeAttr(b, "color", colorString(s.Color))
	b.WriteString(" width=\"" + num(s.Width))
	if s.Widths != nil {
		for i := 0; i < len(s.Points)-1; i++ {
			b.WriteString(" " + num(s.WidthAt(i)))
		}
	}
	b.WriteString("\"")
	if s.Fill.A > 0 {
		// Xournal++ can only fill with the outline colour (at this alpha);
		// when the fill is a different colour, give it a light tint so text
		// inside stays readable there. dot! uses the exact fillColor.
		alpha := int(s.Fill.A)
		if rgb := s.Fill; rgb.R != s.Color.R || rgb.G != s.Color.G || rgb.B != s.Color.B {
			alpha = min(alpha, 48)
		}
		writeAttr(b, "fill", strconv.Itoa(alpha))
		writeAttr(b, "fillColor", colorString(s.Fill))
	}
	if s.Style != "" {
		writeAttr(b, "style", s.Style)
	}
	if s.Shape != "" && len(s.Ctrl) == 3 {
		writeAttr(b, "inkShape", s.Shape)
		var c []string
		for _, p := range s.Ctrl {
			c = append(c, num(p.X), num(p.Y))
		}
		writeAttr(b, "inkCtrl", strings.Join(c, " "))
	}
	hasCap := false
	for _, a := range s.Extra {
		hasCap = hasCap || a.Name.Local == "capStyle"
		writeAttr(b, a.Name.Local, a.Value)
	}
	if !hasCap {
		writeAttr(b, "capStyle", "round")
	}
	b.WriteString(">")
	for i, p := range s.Points {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(num(p.X) + " " + num(p.Y))
	}
	b.WriteString("</stroke>\n")
}

// FormatNum formats a coordinate the way .xopp files store them.
func FormatNum(v float32) string { return num(v) }

// ColorString formats a colour as #rrggbbaa, as .xopp stores it.
func ColorString(c color.NRGBA) string { return colorString(c) }
