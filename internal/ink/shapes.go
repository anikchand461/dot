package ink

import (
	"math"

	"gioui.org/f32"
)

type Shape int

const (
	NoShape Shape = iota
	Line
	Arrow
	Rect
	Ellipse
	Triangle
	Polygon
	DoubleArrow
	RoundRect
	Diamond
	Pentagon
	Hexagon
	Star
)

var ShapeNames = []string{"", "Line", "Arrow", "Rectangle", "Ellipse", "Triangle", "Polygon",
	"Double arrow", "Rounded rectangle", "Diamond", "Pentagon", "Hexagon", "Star"}

// Gallery is the order shapes appear in the Shapes panel.
var Gallery = []Shape{Line, Arrow, DoubleArrow, Rect, RoundRect, Ellipse, Triangle, Diamond, Pentagon, Hexagon, Star}

// Closed reports whether a shape has an inside that can be filled.
func (k Shape) Closed() bool { return k != Line && k != Arrow && k != DoubleArrow && k != NoShape }

// Constrain makes b give a square box (or a 45°-snapped line) from a,
// as when Shift is held.
func Constrain(kind Shape, a, b f32.Point) f32.Point {
	d := b.Sub(a)
	if !kind.Closed() {
		ang := math.Round(math.Atan2(float64(d.Y), float64(d.X))/(math.Pi/4)) * (math.Pi / 4)
		l := float64(hypot(d))
		return a.Add(f32.Pt(float32(l*math.Cos(ang)), float32(l*math.Sin(ang))))
	}
	m := max(abs(d.X), abs(d.Y))
	sx, sy := float32(1), float32(1)
	if d.X < 0 {
		sx = -1
	}
	if d.Y < 0 {
		sy = -1
	}
	return a.Add(f32.Pt(m*sx, m*sy))
}

// arrowHead returns the two barb points of an arrow ending at b, coming from a.
func arrowHead(a, b f32.Point) (f32.Point, f32.Point, bool) {
	d := b.Sub(a)
	l := hypot(d)
	if l < 1e-3 {
		return b, b, false
	}
	u := d.Div(l)
	head := min(l*0.35, 14)
	back := b.Sub(u.Mul(head))
	n := f32.Pt(-u.Y, u.X).Mul(head * 0.55)
	return back.Add(n), back.Sub(n), true
}

// regular returns an n-gon (or a star when inner > 0) inscribed in the box.
func regular(a, b f32.Point, n int, inner float32) []f32.Point {
	c := a.Add(b).Mul(0.5)
	rx, ry := abs(b.X-a.X)/2, abs(b.Y-a.Y)/2
	steps := n
	if inner > 0 {
		steps = 2 * n
	}
	pts := make([]f32.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		ang := -math.Pi/2 + 2*math.Pi*float64(i)/float64(steps)
		r := float32(1)
		if inner > 0 && i%2 == 1 {
			r = inner
		}
		pts = append(pts, c.Add(f32.Pt(rx*r*float32(math.Cos(ang)), ry*r*float32(math.Sin(ang)))))
	}
	return pts
}

func roundRect(a, b f32.Point) []f32.Point {
	x0, y0, x1, y1 := min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y)
	r := min(x1-x0, y1-y0) * 0.18
	var pts []f32.Point
	corner := func(cx, cy float32, from float64) {
		for i := 0; i <= 8; i++ {
			ang := from + math.Pi/2*float64(i)/8
			pts = append(pts, f32.Pt(cx+r*float32(math.Cos(ang)), cy+r*float32(math.Sin(ang))))
		}
	}
	corner(x1-r, y0+r, -math.Pi/2)
	corner(x1-r, y1-r, 0)
	corner(x0+r, y1-r, math.Pi/2)
	corner(x0+r, y0+r, math.Pi)
	return append(pts, pts[0])
}

// Build returns the polyline for a shape dragged from a to b.
func Build(kind Shape, a, b f32.Point) []f32.Point {
	switch kind {
	case Line:
		return []f32.Point{a, b}
	case Arrow:
		h1, h2, ok := arrowHead(a, b)
		if !ok {
			return []f32.Point{a, b}
		}
		return []f32.Point{a, b, h1, b, h2}
	case DoubleArrow:
		h1, h2, ok := arrowHead(a, b)
		t1, t2, _ := arrowHead(b, a)
		if !ok {
			return []f32.Point{a, b}
		}
		return []f32.Point{t1, a, t2, a, b, h1, b, h2}
	case RoundRect:
		return roundRect(a, b)
	case Triangle:
		x0, x1 := min(a.X, b.X), max(a.X, b.X)
		y0, y1 := min(a.Y, b.Y), max(a.Y, b.Y)
		top := f32.Pt((x0+x1)/2, y0)
		return []f32.Point{top, {X: x1, Y: y1}, {X: x0, Y: y1}, top}
	case Diamond:
		return regular(a, b, 4, 0)
	case Pentagon:
		return regular(a, b, 5, 0)
	case Hexagon:
		return regular(a, b, 6, 0)
	case Star:
		return regular(a, b, 5, 0.42)
	case Rect:
		return []f32.Point{a, {X: b.X, Y: a.Y}, b, {X: a.X, Y: b.Y}, a}
	case Ellipse:
		c := a.Add(b).Mul(0.5)
		r := b.Sub(a).Mul(0.5)
		return ellipse(c, abs(r.X), abs(r.Y))
	}
	return []f32.Point{a, b}
}

func ellipse(c f32.Point, rx, ry float32) []f32.Point {
	const n = 72
	pts := make([]f32.Point, 0, n+1)
	for i := 0; i <= n; i++ {
		t := 2 * math.Pi * float64(i) / n
		pts = append(pts, c.Add(f32.Pt(rx*float32(math.Cos(t)), ry*float32(math.Sin(t)))))
	}
	return pts
}

// Recognize tries to turn a hand-drawn stroke into a clean shape
// ("draw and hold"). It returns NoShape if nothing fits well.
func Recognize(pts []f32.Point) (Shape, []f32.Point) {
	if len(pts) < 3 {
		return NoShape, nil
	}
	minP, maxP := pts[0], pts[0]
	var pathLen float32
	for i, p := range pts {
		minP = f32.Pt(min(minP.X, p.X), min(minP.Y, p.Y))
		maxP = f32.Pt(max(maxP.X, p.X), max(maxP.Y, p.Y))
		if i > 0 {
			pathLen += hypot(p.Sub(pts[i-1]))
		}
	}
	diag := hypot(maxP.Sub(minP))
	if diag < 8 {
		return NoShape, nil
	}
	first, last := pts[0], pts[len(pts)-1]
	chord := hypot(last.Sub(first))

	// Straight line: every point close to the first-last chord.
	if chord > 0.8*diag {
		var dev float32
		for _, p := range pts {
			dev = max(dev, distToLine(p, first, last))
		}
		if dev < 0.07*chord {
			return Line, []f32.Point{first, snapAngle(first, last)}
		}
	}

	if chord > 0.3*diag || pathLen < 2*diag {
		return NoShape, nil // open curve
	}

	// Closed: count corners after simplification.
	poly := rdp(pts, 0.08*diag)
	if len(poly) > 1 && hypot(poly[0].Sub(poly[len(poly)-1])) < 0.3*diag {
		poly = poly[:len(poly)-1]
	}
	switch len(poly) {
	case 3:
		return Triangle, append(poly, poly[0])
	case 4:
		if axisAligned(poly) {
			return Rect, Build(Rect, minP, maxP)
		}
		return Polygon, append(poly, poly[0])
	}

	// Ellipse: radius, normalised by the bounding box, stays near 1.
	c := minP.Add(maxP).Mul(0.5)
	rx, ry := (maxP.X-minP.X)/2, (maxP.Y-minP.Y)/2
	if rx < 1 || ry < 1 {
		return NoShape, nil
	}
	var errSum float32
	for _, p := range pts {
		d := p.Sub(c)
		r := float32(math.Hypot(float64(d.X/rx), float64(d.Y/ry)))
		errSum += abs(r - 1)
	}
	if errSum/float32(len(pts)) > 0.17 {
		return NoShape, nil
	}
	if abs(rx-ry) < 0.12*max(rx, ry) { // near circle -> circle
		r := (rx + ry) / 2
		rx, ry = r, r
	}
	return Ellipse, ellipse(c, rx, ry)
}

func axisAligned(poly []f32.Point) bool {
	for i := range poly {
		d := poly[(i+1)%len(poly)].Sub(poly[i])
		a := math.Abs(math.Atan2(float64(d.Y), float64(d.X))) * 180 / math.Pi
		off := math.Min(math.Mod(a, 90), 90-math.Mod(a, 90))
		if off > 18 {
			return false
		}
	}
	return true
}

// snapAngle snaps b to the nearest 45° direction from a when within 5°.
func snapAngle(a, b f32.Point) f32.Point {
	d := b.Sub(a)
	ang := math.Atan2(float64(d.Y), float64(d.X))
	snap := math.Round(ang/(math.Pi/4)) * (math.Pi / 4)
	if math.Abs(ang-snap) > 5*math.Pi/180 {
		return b
	}
	l := float64(hypot(d))
	return a.Add(f32.Pt(float32(l*math.Cos(snap)), float32(l*math.Sin(snap))))
}

func distToLine(p, a, b f32.Point) float32 {
	d := b.Sub(a)
	l := hypot(d)
	if l == 0 {
		return hypot(p.Sub(a))
	}
	return abs((p.X-a.X)*d.Y-(p.Y-a.Y)*d.X) / l
}

// rdp is Ramer–Douglas–Peucker polyline simplification.
func rdp(pts []f32.Point, eps float32) []f32.Point {
	if len(pts) < 3 {
		return pts
	}
	var dmax float32
	idx := 0
	for i := 1; i < len(pts)-1; i++ {
		if d := distToLine(pts[i], pts[0], pts[len(pts)-1]); d > dmax {
			dmax, idx = d, i
		}
	}
	// A closed stroke has first≈last, so the chord is degenerate: split at
	// the point farthest from the start instead.
	if hypot(pts[len(pts)-1].Sub(pts[0])) < eps {
		dmax = 0
		for i := 1; i < len(pts)-1; i++ {
			if d := hypot(pts[i].Sub(pts[0])); d > dmax {
				dmax, idx = d, i
			}
		}
	}
	if dmax <= eps {
		return []f32.Point{pts[0], pts[len(pts)-1]}
	}
	l := rdp(pts[:idx+1], eps)
	r := rdp(pts[idx:], eps)
	return append(l[:len(l)-1], r...)
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }

// Curve builds a line/arrow from a to b bent through control c (a
// quadratic Bézier; c at the midpoint gives a straight line). Arrowheads
// follow the curve's direction at the ends.
func Curve(kind Shape, a, c, b f32.Point) []f32.Point {
	const n = 32
	at := func(t float32) f32.Point {
		u := 1 - t
		return a.Mul(u * u).Add(c.Mul(2 * u * t)).Add(b.Mul(t * t))
	}
	var body []f32.Point
	for i := 0; i <= n; i++ {
		body = append(body, at(float32(i)/n))
	}
	if kind == Line {
		return body
	}
	head := func(tip, from f32.Point) []f32.Point {
		h1, h2, ok := arrowHead(from, tip)
		if !ok {
			return nil
		}
		return []f32.Point{h1, tip, h2}
	}
	// Direction near each end, sampled a little way in for stability.
	end := head(b, at(0.9))
	var out []f32.Point
	if kind == DoubleArrow {
		if st := head(a, at(0.1)); st != nil {
			out = append(out, st[0], st[1], st[2], a)
		}
	}
	out = append(out, body...)
	if end != nil {
		out = append(out, end[0], b, end[2])
	}
	return out
}

// MidPoint is the point on the curve at t=0.5 (where the bend handle sits).
func MidPoint(a, c, b f32.Point) f32.Point {
	return a.Mul(0.25).Add(c.Mul(0.5)).Add(b.Mul(0.25))
}

// ControlFor returns the control point that makes the curve pass through m.
func ControlFor(a, m, b f32.Point) f32.Point {
	return m.Mul(2).Sub(a.Add(b).Mul(0.5))
}

// Bendable reports whether a shape is a line or arrow (curves can bend).
func (k Shape) Bendable() bool { return k == Line || k == Arrow || k == DoubleArrow }

// ShapeByName maps ShapeNames back to shapes.
func ShapeByName(name string) Shape {
	for i, n := range ShapeNames {
		if n == name && i > 0 {
			return Shape(i)
		}
	}
	return NoShape
}
