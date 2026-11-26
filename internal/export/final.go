package export

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"math"

	"gioui.org/f32"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/pdf"
)

// FinalPDF exports the notebook. Pages annotating a PDF keep the original
// page (its text stays selectable) with the ink drawn on top as PDF
// vectors; other pages are written by dot!'s own vector writer.
func FinalPDF(w io.Writer, d *document.Document) error {
	var sources []pdf.Source
	var overlays []pdf.Overlay
	hasPDF := false
	for _, p := range d.Pages {
		if b := p.Background; b.Type == "pdf" && b.PDFPath != "" {
			sources = append(sources, pdf.Source{Path: b.PDFPath, Page: b.PDFPage})
			overlays = append(overlays, overlay(p))
			hasPDF = true
		} else {
			sources = append(sources, pdf.Source{})
			overlays = append(overlays, pdf.Overlay{})
		}
	}
	if !hasPDF {
		return PDF(w, d)
	}
	var plain bytes.Buffer
	if err := PDF(&plain, d); err != nil {
		return err
	}
	return pdf.Merge(w, plain.Bytes(), sources, overlays)
}

func overlay(p *document.Page) pdf.Overlay {
	var ov pdf.Overlay
	for _, l := range p.Layers {
		if l.Hidden {
			continue
		}
		for _, it := range l.Items {
			switch it := it.(type) {
			case *document.Stroke:
				if it.Fill.A > 0 && len(it.Points) > 2 {
					var pts [][2]float32
					for _, p := range it.Points {
						pts = append(pts, [2]float32{p.X, p.Y})
					}
					ov.Fills = append(ov.Fills, pdf.Fill{Paths: [][][2]float32{pts}, Color: it.Fill})
				}
				if it.Color.A < 255 { // see strokePath
					ov.Fills = append(ov.Fills, pdf.Fill{Paths: outline(it), Color: it.Color})
				} else {
					ov.Lines = append(ov.Lines, runs(it)...)
				}
			case *document.RawElement:
				switch it.Name {
				case "text":
					size := it.Float("size")
					if size <= 0 {
						size = 12
					}
					t := pdf.Text{X: it.Float("x"), Y: it.Float("y"), Size: size,
						Font: base14[fontIndex(it.Attr("font"))], Color: it.Color()}
					if fam, bold, _ := canvas.ParseFont(it.Attr("font")); canvas.IsHandwriting(fam) {
						t.FontKey, t.FontData = handKey(fam, bold), canvas.FontFile(fam, bold)
					}
					for _, ln := range textLines(it, size) {
						t.Lines, t.Offsets = append(t.Lines, ln.text), append(t.Offsets, ln.dx)
					}
					ov.Texts = append(ov.Texts, t)
				case "image":
					if img := it.Image(); img != nil {
						ov.Images = append(ov.Images, pdf.Image{
							Box:  [4]float32{it.Float("left"), it.Float("top"), it.Float("right"), it.Float("bottom")},
							JPEG: toJPEG(img),
						})
					}
				}
			}
		}
	}
	return ov
}

// runs turns a stroke into round-capped lines: one per solid piece of its
// dash pattern (dots become tiny lines, which round caps draw as dots),
// each split where the pressure width drifts more than ±8%.
func runs(s *document.Stroke) []pdf.Line {
	var out []pdf.Line
	for _, pc := range s.Pieces() {
		pts := pc.Points
		if pc.Dot || len(pts) == 1 {
			p := pts[0]
			out = append(out, pdf.Line{Points: [][2]float32{{p.X, p.Y}, {p.X + 0.01, p.Y}}, Width: pc.Widths[0], Color: s.Color})
			continue
		}
		for start := 0; start < len(pts)-1; {
			w0 := pc.Widths[start]
			end := start + 1
			sum := w0 + pc.Widths[end]
			for end+1 < len(pts) && math.Abs(float64(pc.Widths[end+1]-w0)) <= 0.08*float64(w0) {
				end++
				sum += pc.Widths[end]
			}
			ln := pdf.Line{Width: sum / float32(end-start+1), Color: s.Color}
			for _, p := range pts[start : end+1] {
				ln.Points = append(ln.Points, [2]float32{p.X, p.Y})
			}
			out = append(out, ln)
			start = end
		}
	}
	return out
}

// outline returns the closed outlines that fill s (canvas.StrokeOutline),
// with curves flattened to short lines.
func outline(s *document.Stroke) [][][2]float32 {
	var o outliner
	canvas.StrokeOutline(&o, s)
	return o.paths
}

type outliner struct {
	paths [][][2]float32
	cur   f32.Point
}

func (o *outliner) MoveTo(to f32.Point) {
	o.paths = append(o.paths, [][2]float32{{to.X, to.Y}})
	o.cur = to
}

func (o *outliner) LineTo(to f32.Point) {
	if len(o.paths) == 0 {
		o.MoveTo(to)
		return
	}
	last := &o.paths[len(o.paths)-1]
	*last = append(*last, [2]float32{to.X, to.Y})
	o.cur = to
}

func (o *outliner) CubeTo(c0, c1, to f32.Point) {
	p0 := o.cur
	const steps = 6 // plenty for the quarter circles of caps and joints
	for i := 1; i <= steps; i++ {
		t := float32(i) / steps
		u := 1 - t
		o.LineTo(p0.Mul(u * u * u).Add(c0.Mul(3 * u * u * t)).Add(c1.Mul(3 * u * t * t)).Add(to.Mul(t * t * t)))
	}
}

func (o *outliner) Close() {}

// toJPEG flattens transparency onto white (JPEG has no alpha).
func toJPEG(img image.Image) []byte {
	rgb := image.NewRGBA(img.Bounds())
	draw.Draw(rgb, rgb.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(rgb, rgb.Bounds(), img, img.Bounds().Min, draw.Over)
	var buf bytes.Buffer
	jpeg.Encode(&buf, rgb, &jpeg.Options{Quality: 92})
	return buf.Bytes()
}
