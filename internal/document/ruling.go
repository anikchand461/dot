package document

import (
	"image/color"

	"gioui.org/f32"
)

// Ruling geometry follows Xournal++'s defaults; screen and PDF export
// both draw from this.
const (
	RuleSpacing = 24.0
	RuleTop     = 80.0
	MarginX     = 72.0
	GridSpacing = 14.17 // 5 mm
	RuleWidth   = 0.5
)

type Ruling struct {
	Lines     [][2]f32.Point
	LineColor color.NRGBA
	Margin    [][2]f32.Point
	Dots      []f32.Point
	DotColor  color.NRGBA
}

// IsDark reports whether c is a dark paper colour (light ruling on top).
func IsDark(c color.NRGBA) bool {
	return int(c.R)*299+int(c.G)*587+int(c.B)*114 < 128*1000
}

func (p *Page) Ruling() Ruling {
	var r Ruling
	if p.Background.Type != "solid" {
		return r
	}
	dark := IsDark(p.Background.Color)
	blue, grey := color.NRGBA{0x40, 0xa0, 0xff, 0xff}, color.NRGBA{0xbd, 0xbd, 0xbd, 0xff}
	if dark {
		blue, grey = color.NRGBA{0x3a, 0x5a, 0x80, 0xff}, color.NRGBA{0x40, 0x40, 0x40, 0xff}
	}
	w, h := p.Width, p.Height
	switch p.Background.Style {
	case StyleRuled, StyleLined:
		r.LineColor = blue
		for y := float32(RuleTop); y < h; y += RuleSpacing {
			r.Lines = append(r.Lines, [2]f32.Point{{X: 0, Y: y}, {X: w, Y: y}})
		}
		if p.Background.Style == StyleLined {
			r.Margin = [][2]f32.Point{{{X: MarginX, Y: 0}, {X: MarginX, Y: h}}}
		}
	case StyleGraph:
		r.LineColor = grey
		for x := float32(GridSpacing); x < w; x += GridSpacing {
			r.Lines = append(r.Lines, [2]f32.Point{{X: x, Y: 0}, {X: x, Y: h}})
		}
		for y := float32(GridSpacing); y < h; y += GridSpacing {
			r.Lines = append(r.Lines, [2]f32.Point{{X: 0, Y: y}, {X: w, Y: y}})
		}
	case StyleDotted:
		r.DotColor = color.NRGBA{0x80, 0x80, 0x80, 0xff}
		if dark {
			r.DotColor = grey
		}
		for y := float32(GridSpacing); y < h; y += GridSpacing {
			for x := float32(GridSpacing); x < w; x += GridSpacing {
				r.Dots = append(r.Dots, f32.Pt(x, y))
			}
		}
	}
	return r
}

var MarginColor = color.NRGBA{0xff, 0x00, 0x80, 0xff}
