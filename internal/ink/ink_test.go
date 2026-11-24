package ink

import (
	"math"
	"testing"

	"gioui.org/f32"
)

func TestCurveSensitivity(t *testing.T) {
	soft := Config{Sensitivity: 1}
	firm := Config{Sensitivity: 0}
	if !(soft.Curve(0.3) > 0.5 && firm.Curve(0.3) < 0.1) {
		t.Fatalf("curve not monotone in sensitivity: soft=%v firm=%v", soft.Curve(0.3), firm.Curve(0.3))
	}
	if soft.Curve(1) != 1 || firm.Curve(0) != 0 {
		t.Fatal("curve endpoints must be fixed")
	}
}

func TestPressureChangesWidth(t *testing.T) {
	e := Engine{Cfg: Config{Sensitivity: 0.5}, Base: 2}
	e.Begin(f32.Pt(0, 0), 0.1, true)
	for i := 1; i < 40; i++ {
		e.Add(f32.Pt(float32(i), 0), 1, 0.1)
	}
	if e.Widths[len(e.Widths)-1] < 3*e.Widths[0] {
		t.Fatalf("pressure 0.1 -> 1 should widen a lot: %v -> %v", e.Widths[0], e.Widths[len(e.Widths)-1])
	}
}

func circlePts(n int, r float32, wobble float32) []f32.Point {
	var pts []f32.Point
	for i := 0; i <= n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		rr := r + wobble*float32(math.Sin(a*5))
		pts = append(pts, f32.Pt(100+rr*float32(math.Cos(a)), 100+rr*float32(math.Sin(a))))
	}
	return pts
}

func TestRecognize(t *testing.T) {
	var line []f32.Point
	for i := 0; i <= 50; i++ {
		line = append(line, f32.Pt(float32(i)*2, float32(i)*0.05+float32(i%3)*0.3))
	}
	var rect []f32.Point
	for _, c := range [][2]f32.Point{{f32.Pt(0, 0), f32.Pt(100, 0)}, {f32.Pt(100, 0), f32.Pt(100, 60)}, {f32.Pt(100, 60), f32.Pt(0, 60)}, {f32.Pt(0, 60), f32.Pt(0, 2)}} {
		for i := 0; i < 20; i++ {
			rect = append(rect, c[0].Add(c[1].Sub(c[0]).Mul(float32(i)/20)))
		}
	}
	tri := []f32.Point{}
	for _, c := range [][2]f32.Point{{f32.Pt(0, 100), f32.Pt(50, 0)}, {f32.Pt(50, 0), f32.Pt(100, 100)}, {f32.Pt(100, 100), f32.Pt(2, 99)}} {
		for i := 0; i < 20; i++ {
			tri = append(tri, c[0].Add(c[1].Sub(c[0]).Mul(float32(i)/20)))
		}
	}
	var squiggle []f32.Point
	for i := 0; i < 60; i++ {
		squiggle = append(squiggle, f32.Pt(float32(i)*3, 20*float32(math.Sin(float64(i)/3))))
	}
	cases := []struct {
		name string
		pts  []f32.Point
		want Shape
	}{
		{"line", line, Line},
		{"circle", circlePts(60, 40, 2), Ellipse},
		{"rect", rect, Rect},
		{"triangle", tri, Triangle},
		{"squiggle", squiggle, NoShape},
	}
	for _, c := range cases {
		if got, _ := Recognize(c.pts); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, ShapeNames[got], ShapeNames[c.want])
		}
	}
}

func TestResampleSmooth(t *testing.T) {
	pts := []f32.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}
	out, ws := Resample(pts, []float32{1, 1, 1}, 1)
	if len(out) < 15 || len(out) != len(ws) {
		t.Fatalf("expected subdivided output, got %d points", len(out))
	}
	if out[len(out)-1] != pts[2] {
		t.Fatal("resample must end on the last point")
	}
}

// The live preview must match the final stroke (no jump on pen lift).
func TestLiveMatchesFinish(t *testing.T) {
	e := Engine{Cfg: Config{Sensitivity: 0.5, Smoothing: 0.3}, Base: 2, Style: Ballpoint}
	e.Begin(f32.Pt(0, 0), 0.5, true)
	var last f32.Point
	for i := 1; i < 50; i++ {
		last = f32.Pt(float32(i)*3, 20*float32(math.Sin(float64(i)/4)))
		e.Add(last, 0.5, 0.1)
		e.Live(last)
	}
	live, _ := e.Live(e.Points[len(e.Points)-1])
	final, _ := Resample(e.Points, e.Widths, liveSpacing)
	if len(live) != len(final) {
		t.Fatalf("live has %d points, final %d", len(live), len(final))
	}
	for i := range live {
		if d := live[i].Sub(final[i]); d.X*d.X+d.Y*d.Y > 1e-6 {
			t.Fatalf("point %d differs: %v vs %v", i, live[i], final[i])
		}
	}
}

func TestBuildShapes(t *testing.T) {
	for _, k := range Gallery {
		pts := Build(k, f32.Pt(10, 10), f32.Pt(110, 70))
		if len(pts) < 2 {
			t.Fatalf("%s: no points", ShapeNames[k])
		}
		if k.Closed() && pts[0] != pts[len(pts)-1] {
			t.Fatalf("%s should be closed", ShapeNames[k])
		}
	}
	if b := Constrain(Rect, f32.Pt(0, 0), f32.Pt(50, 20)); b != f32.Pt(50, 50) {
		t.Fatalf("shift should make a square, got %v", b)
	}
}

func TestCurve(t *testing.T) {
	a, b := f32.Pt(0, 0), f32.Pt(100, 0)
	m := f32.Pt(50, 40)
	c := ControlFor(a, m, b)
	if got := MidPoint(a, c, b); got.Sub(m).X > 0.01 || got.Sub(m).Y > 0.01 {
		t.Fatalf("curve should pass through the bend handle: %v", got)
	}
	pts := Curve(Arrow, a, c, b)
	if pts[0] != a || len(pts) < 30 {
		t.Fatalf("bad arrow curve: %d points", len(pts))
	}
}
