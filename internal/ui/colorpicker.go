package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// colorPicker is an HSV colour wheel (hue around, saturation outwards)
// with a brightness slider and a hex field.
type colorPicker struct {
	h, s, v float32

	bright    widget.Float
	hex       widget.Editor
	dragging  bool
	wasBright bool

	// Set by Layout for the caller: Changed while the colour moves,
	// Committed when an interaction finishes (add to recents, undo step).
	Changed, Committed bool

	img     paint.ImageOp
	imgSize int
	imgV    float32
}

func newColorPicker() *colorPicker {
	cp := &colorPicker{v: 1}
	cp.bright.Value = 1
	cp.hex.SingleLine, cp.hex.Submit = true, true
	return cp
}

// Sync shows c unless the user is in the middle of picking.
func (cp *colorPicker) Sync(c color.NRGBA) {
	if cp.dragging || cp.bright.Dragging() {
		return
	}
	if cur := cp.Color(); cur.R != c.R || cur.G != c.G || cur.B != c.B {
		cp.h, cp.s, cp.v = rgbToHSV(c)
	}
	cp.bright.Value = cp.v
}

func (cp *colorPicker) Color() color.NRGBA { return hsvToRGB(cp.h, cp.s, cp.v) }

func (cp *colorPicker) Layout(gtx C, th *material.Theme, focused bool) D {
	cp.Changed, cp.Committed = false, false
	size := min(gtx.Constraints.Max.X, gtx.Dp(200))
	r := float32(size) / 2

	// Wheel input.
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: cp, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press, pointer.Drag:
			cp.dragging = true
			d := e.Position.Sub(f32.Pt(r, r))
			cp.h = float32(math.Mod(math.Atan2(float64(d.Y), float64(d.X))/(2*math.Pi)+1, 1))
			cp.s = min(1, float32(math.Hypot(float64(d.X), float64(d.Y)))/r)
			cp.Changed = true
		case pointer.Release, pointer.Cancel:
			cp.dragging, cp.Committed = false, true
		}
	}
	if cp.bright.Update(gtx) {
		cp.v, cp.Changed = cp.bright.Value, true
	}
	if cp.wasBright && !cp.bright.Dragging() {
		cp.Committed = true
	}
	cp.wasBright = cp.bright.Dragging()
	for {
		ev, ok := cp.hex.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			if c, ok := parseHex(cp.hex.Text()); ok {
				cp.h, cp.s, cp.v = rgbToHSV(c)
				cp.bright.Value = cp.v
				cp.Changed, cp.Committed = true, true
			}
		}
	}
	if !focused && !gtx.Source.Focused(&cp.hex) {
		if want := hexString(cp.Color()); cp.hex.Text() != want {
			cp.hex.SetText(want)
		}
	}

	// Regenerate the wheel bitmap only when size or brightness changes.
	if cp.imgSize != size || cp.imgV != cp.v {
		cp.img = paint.NewImageOp(wheelImage(size, cp.v))
		cp.imgSize, cp.imgV = size, cp.v
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				sz := image.Pt(size, size)
				cp.img.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				// Marker.
				ang := float64(cp.h) * 2 * math.Pi
				m := f32.Pt(r+float32(math.Cos(ang))*cp.s*r, r+float32(math.Sin(ang))*cp.s*r)
				mr := gtx.Dp(8)
				box := image.Rect(int(m.X)-mr, int(m.Y)-mr, int(m.X)+mr, int(m.Y)+mr)
				paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 0x90}, clip.Ellipse(box.Inset(-1)).Op(gtx.Ops))
				paint.FillShape(gtx.Ops, cWhite, clip.Ellipse(box).Op(gtx.Ops))
				paint.FillShape(gtx.Ops, cp.Color(), clip.Ellipse(box.Inset(gtx.Dp(2.5))).Op(gtx.Ops))
				area := clip.Ellipse(image.Rectangle{Max: sz}).Push(gtx.Ops)
				event.Op(gtx.Ops, cp)
				pointer.CursorCrosshair.Add(gtx.Ops)
				area.Pop()
				return D{Size: sz}
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				s := material.Slider(th, &cp.bright)
				s.Color = cAccent
				return s.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					d := gtx.Dp(28)
					rr := image.Rectangle{Max: image.Pt(d, d)}
					paint.FillShape(gtx.Ops, cBorder, clip.UniformRRect(rr, gtx.Dp(7)).Op(gtx.Ops))
					paint.FillShape(gtx.Ops, cp.Color(), clip.UniformRRect(rr.Inset(1), gtx.Dp(6)).Op(gtx.Ops))
					return D{Size: rr.Max}
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(96), gtx.Dp(96)
						m := op.Record(gtx.Ops)
						dims := layout.Inset{Top: 6, Bottom: 6, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
							ed := material.Editor(th, &cp.hex, "#RRGGBB")
							ed.Color, ed.HintColor, ed.TextSize = cText, cMuted, unit.Sp(13)
							return ed.Layout(gtx)
						})
						call := m.Stop()
						paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(7)).Op(gtx.Ops))
						call.Add(gtx.Ops)
						return dims
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
						l := material.Caption(th, "Enter ↵ to apply")
						l.Color = cMuted
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

func wheelImage(size int, v float32) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Hypot(dx, dy)
			if d > r+0.5 {
				continue
			}
			h := math.Mod(math.Atan2(dy, dx)/(2*math.Pi)+1, 1)
			c := hsvToRGB(float32(h), float32(min(1, d/r)), v)
			c.A = uint8(255 * math.Max(0, math.Min(1, r+0.5-d)))
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func hsvToRGB(h, s, v float32) color.NRGBA {
	h6 := float64(h) * 6
	i := math.Floor(h6)
	f := float32(h6 - i)
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	var r, g, b float32
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	u := func(x float32) uint8 { return uint8(math.Round(float64(clamp(x, 0, 1)) * 255)) }
	return color.NRGBA{u(r), u(g), u(b), 0xff}
}

func rgbToHSV(c color.NRGBA) (h, s, v float32) {
	r, g, b := float32(c.R)/255, float32(c.G)/255, float32(c.B)/255
	mx, mn := max(r, g, b), min(r, g, b)
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	if d == 0 {
		return 0, s, v
	}
	switch mx {
	case r:
		h = (g - b) / d
		if h < 0 {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, v
}

func hexString(c color.NRGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

func parseHex(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}, true
}
