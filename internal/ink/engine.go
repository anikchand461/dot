// Package ink turns raw pointer samples into smooth, pressure-shaped
// strokes. It is pure Go and independent of the UI.
//
// Pipeline per sample:
//
//	position  -> stabiliser (EMA, strength = Smoothing)
//	pressure  -> EMA -> response curve (Sensitivity) -> pen style range
//	direction -> nib angle (calligraphy only)
//
// On Finish the polyline is resampled along a Catmull-Rom spline so
// corners become curves, and the ends are tapered.
package ink

import (
	"math"

	"gioui.org/f32"
)

type Style int

const (
	Fountain    Style = iota // strong pressure response
	Ballpoint                // nearly constant width
	Brush                    // very wide dynamic range
	Calligraphy              // width depends on stroke direction (45° nib)
)

var StyleNames = []string{"Fountain", "Ballpoint", "Brush", "Calligraphy"}

// Config is the user-tunable feel of the pen.
type Config struct {
	Sensitivity float32 // 0 = needs a hard press, 1 = feather light
	Smoothing   float32 // 0 = raw input, 1 = heavy stabiliser
	Taper       bool    // thin start and end
}

func DefaultConfig() Config { return Config{Sensitivity: 0.6, Smoothing: 0.45, Taper: true} }

// widthRange is the min/max width factor (times base width) per style.
func (s Style) widthRange() (lo, hi float32) {
	switch s {
	case Ballpoint:
		return 0.8, 1.15
	case Brush:
		return 0.06, 2.8
	case Calligraphy:
		return 0.55, 1.35
	}
	return 0.15, 1.9
}

// Curve maps raw pressure 0..1 through a gamma curve set by sensitivity:
// sensitivity 1 -> gamma ~0.3 (light touch is already thick),
// sensitivity 0 -> gamma ~3.3 (needs a firm press).
func (c Config) Curve(p float32) float32 {
	p = clamp(p, 0, 1)
	gamma := math.Pow(2, float64(0.5-c.Sensitivity)*3.4)
	return float32(math.Pow(float64(p), gamma))
}

// Engine builds one stroke.
type Engine struct {
	Cfg   Config
	Style Style
	Base  float32 // nominal width in pt

	Points []f32.Point
	Widths []float32

	pos      f32.Point // stabilised position
	pressure float32   // smoothed pressure
	real     bool
	length   float32 // arc length so far

	// Incremental spline for the live preview: segments before the last
	// two input points never change, so they are smoothed once and kept.
	sPts  []f32.Point
	sWs   []float32
	sDone int
}

// liveSpacing matches Finish, so the stroke doesn't change shape on lift.
const liveSpacing = 1.2

// Begin starts a stroke. real reports whether pressure came from hardware.
func (e *Engine) Begin(pt f32.Point, pressure float32, real bool) {
	e.Points, e.Widths = e.Points[:0], e.Widths[:0]
	e.sPts, e.sWs, e.sDone = e.sPts[:0], e.sWs[:0], 0
	e.pos, e.pressure, e.real, e.length = pt, pressure, real, 0
	e.Points = append(e.Points, pt)
	e.Widths = append(e.Widths, e.width(pressure, f32.Point{}))
}

// Add feeds the next raw sample. minDist (pt) drops jitter-level moves.
func (e *Engine) Add(pt f32.Point, pressure float32, minDist float32) {
	alpha := 1 - 0.75*clamp(e.Cfg.Smoothing, 0, 1)
	e.pos = e.pos.Add(pt.Sub(e.pos).Mul(alpha))
	beta := float32(0.35)
	if !e.real {
		beta = 0.2
	}
	e.pressure += (pressure - e.pressure) * beta

	last := e.Points[len(e.Points)-1]
	d := e.pos.Sub(last)
	dist := hypot(d)
	if dist < minDist {
		return
	}
	e.length += dist
	e.Points = append(e.Points, e.pos)
	e.Widths = append(e.Widths, e.width(e.pressure, d.Div(dist)))
}

func (e *Engine) width(p float32, dir f32.Point) float32 {
	lo, hi := e.Style.widthRange()
	w := e.Base * (lo + (hi-lo)*e.Cfg.Curve(p))
	if e.Style == Calligraphy && dir != (f32.Point{}) {
		// A flat nib held at 45°: thin along the nib, full across it.
		nib := f32.Pt(float32(math.Sqrt2/2), -float32(math.Sqrt2/2))
		cross := float32(math.Abs(float64(dir.X*nib.Y - dir.Y*nib.X)))
		w *= 0.15 + 0.85*cross
	}
	if e.Cfg.Taper && e.Style != Ballpoint {
		// Start taper over the first 6pt.
		w *= 0.35 + 0.65*clamp(e.length/6, 0, 1)
	}
	return w
}

// Live returns the stroke as it should look right now: the same
// Catmull-Rom smoothing Finish applies, plus a short tail to the raw pen
// position so the ink stays under the nib (the stabiliser lags slightly).
func (e *Engine) Live(raw f32.Point) ([]f32.Point, []float32) {
	n := len(e.Points)
	for ; e.sDone <= n-3; e.sDone++ {
		e.sPts, e.sWs = segment(e.Points, e.Widths, e.sDone, liveSpacing, e.sPts, e.sWs)
	}
	pts := append(e.sPts[:len(e.sPts):len(e.sPts)], f32.Point{})[:len(e.sPts)]
	ws := append(e.sWs[:len(e.sWs):len(e.sWs)], 0)[:len(e.sWs)]
	for i := e.sDone; i < n-1; i++ {
		pts, ws = segment(e.Points, e.Widths, i, liveSpacing, pts, ws)
	}
	pts, ws = append(pts, e.Points[n-1]), append(ws, e.Widths[n-1])
	if d := raw.Sub(e.Points[n-1]); hypot(d) > 0.3 {
		pts, ws = append(pts, raw), append(ws, e.Widths[n-1])
	}
	return pts, ws
}

// Finish returns the final stroke: spline-resampled, end-tapered.
func (e *Engine) Finish(release f32.Point) ([]f32.Point, []float32) {
	if d := release.Sub(e.Points[len(e.Points)-1]); hypot(d) > 0.3 {
		e.Points = append(e.Points, release)
		e.Widths = append(e.Widths, e.Widths[len(e.Widths)-1])
	}
	pts, ws := Resample(e.Points, e.Widths, 1.2)
	if e.Cfg.Taper && e.Style != Ballpoint && !e.real {
		TaperEnd(pts, ws, 8)
	}
	return pts, ws
}

// Resample subdivides each segment along a centripetal-ish Catmull-Rom
// spline so that no step is longer than spacing (pt).
func Resample(pts []f32.Point, ws []float32, spacing float32) ([]f32.Point, []float32) {
	if len(pts) < 3 {
		return append([]f32.Point(nil), pts...), append([]float32(nil), ws...)
	}
	out := make([]f32.Point, 0, len(pts)*2)
	ow := make([]float32, 0, len(pts)*2)
	n := len(pts)
	for i := 0; i < n-1; i++ {
		out, ow = segment(pts, ws, i, spacing, out, ow)
	}
	out = append(out, pts[n-1])
	ow = append(ow, ws[n-1])
	return out, ow
}

// segment appends the spline samples of segment i (pts[i] up to, not
// including, pts[i+1]).
func segment(pts []f32.Point, ws []float32, i int, spacing float32, out []f32.Point, ow []float32) ([]f32.Point, []float32) {
	n := len(pts)
	at := func(i int) f32.Point { return pts[max(0, min(n-1, i))] }
	p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
	steps := int(math.Ceil(float64(hypot(p2.Sub(p1)) / spacing)))
	steps = max(1, min(24, steps))
	for s := 0; s < steps; s++ {
		t := float32(s) / float32(steps)
		out = append(out, catmull(p0, p1, p2, p3, t))
		ow = append(ow, ws[i]+(ws[i+1]-ws[i])*t)
	}
	return out, ow
}

func catmull(p0, p1, p2, p3 f32.Point, t float32) f32.Point {
	t2, t3 := t*t, t*t*t
	f := func(a, b, c, d float32) float32 {
		return 0.5 * (2*b + (-a+c)*t + (2*a-5*b+4*c-d)*t2 + (-a+3*b-3*c+d)*t3)
	}
	return f32.Pt(f(p0.X, p1.X, p2.X, p3.X), f(p0.Y, p1.Y, p2.Y, p3.Y))
}

// TaperEnd narrows the last `over` pt of the stroke down to 25%.
func TaperEnd(pts []f32.Point, ws []float32, over float32) {
	var acc float32
	for i := len(pts) - 1; i > 0 && acc < over; i-- {
		ws[i] *= 0.25 + 0.75*clamp(acc/over, 0, 1)
		acc += hypot(pts[i].Sub(pts[i-1]))
	}
}

func hypot(p f32.Point) float32 { return float32(math.Hypot(float64(p.X), float64(p.Y))) }

func clamp(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }
