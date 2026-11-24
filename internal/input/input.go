// Package input turns pointer samples into ink widths.
//
// Gio's pointer.Event carries no pressure or tilt, so stroke width comes
// from one of two sources:
//   - a platform hook (Native) that reads the stylus straight from the OS
//     event stream (NSEvent on macOS, GetPointerPenInfo on Windows), or
//   - a pure-Go velocity model: fast strokes get thinner, like a real nib.
package input

import (
	"math"
	"os"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
)

// Reading is one hardware stylus sample.
type Reading struct {
	Pressure     float32 // 0..1
	TiltX, TiltY float32 // -1..1
	// T is the OS event timestamp, on the same clock Gio uses for
	// pointer.Event.Time on that platform.
	T time.Duration
}

// Native is a platform stylus hook.
type Native interface {
	// Attach is called with every app.ViewEvent so the hook can bind to
	// the native window.
	Attach(e app.ViewEvent)
	// At returns the stylus reading taken from the OS event with
	// timestamp t, i.e. the event that produced Gio's pointer.Event.
	At(t time.Duration) (Reading, bool)
	Name() string
}

// tolerance is how far apart a reading and a pointer event may be and
// still be matched. Gio stamps events with the OS event time, so matches
// are normally exact; the slack covers rounding (1ms on Windows).
const tolerance = 4 * time.Millisecond

// ring is a fixed-size buffer of recent readings, looked up by timestamp.
// Gio queues pointer events until the next frame, so "latest" would give
// every event in a frame the same pressure.
type ring struct {
	buf [256]Reading
	n   int
}

func (r *ring) add(rd Reading) { r.buf[r.n%len(r.buf)] = rd; r.n++ }

func (r *ring) at(t time.Duration) (Reading, bool) {
	best, bestD := Reading{}, tolerance+1
	for i := 0; i < min(r.n, len(r.buf)); i++ {
		d := r.buf[i].T - t
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = r.buf[i], d
		}
	}
	return best, bestD <= tolerance
}

// Provider is the InputProvider: for each pointer sample it reports a
// pressure in 0..1, real when a stylus is present, otherwise simulated
// from speed (slow = pressing harder, fast = lighter), so mouse and
// trackpad strokes still get natural thick/thin variation.
type Provider struct {
	native Native

	// PressureSeen is set once a real stylus reading has arrived.
	PressureSeen bool

	last  f32.Point
	lastT time.Duration
	sim   float32
	wake  func()
}

func NewProvider() *Provider { return &Provider{native: newNative()} }

func (p *Provider) Attach(e app.ViewEvent) { p.native.Attach(e) }

// Mode describes the active pressure source for the status bar.
func (p *Provider) Mode() string {
	if p.PressureSeen {
		return "Stylus pressure"
	}
	return "Simulated pressure"
}

// Begin starts a stroke at pos (pt).
func (p *Provider) Begin(pos f32.Point, t time.Duration) (pressure float32, real bool) {
	p.last, p.lastT, p.sim = pos, t, 0.55
	return p.Next(pos, t)
}

// Next returns the pressure for the next sample.
func (p *Provider) Next(pos f32.Point, t time.Duration) (pressure float32, real bool) {
	if r, ok := p.native.At(t); ok {
		p.PressureSeen = true
		p.last, p.lastT = pos, t
		return r.Pressure, true
	}
	dt := float32((t - p.lastT).Seconds())
	d := pos.Sub(p.last)
	p.last, p.lastT = pos, t
	if dt > 0 {
		speed := float32(math.Hypot(float64(d.X), float64(d.Y))) / dt // pt/s
		p.sim = clamp(0.8-speed/1400, 0.2, 0.8)
	}
	return p.sim, false
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }

// Trackpad gestures from native hooks (Gio has no pinch events). They are
// queued per window view and taken by that window's canvas next frame.
type gestureQueue struct {
	zoom   float32 // accumulated pinch factor, 1 = none
	smart  int     // two-finger double taps
	wake   func()
	waking bool // a wake is already scheduled
}

var gestures struct {
	sync.Mutex
	byView map[uintptr]*gestureQueue
}

func queue(view uintptr) *gestureQueue {
	if gestures.byView == nil {
		gestures.byView = map[uintptr]*gestureQueue{}
	}
	q := gestures.byView[view]
	if q == nil {
		q = &gestureQueue{zoom: 1}
		gestures.byView[view] = q
	}
	return q
}

// viewer is implemented by native hooks that know their window's view.
type viewer interface{ View() uintptr }

func (p *Provider) view() uintptr {
	if v, ok := p.native.(viewer); ok {
		return v.View()
	}
	return 0
}

// SetWake registers a function that schedules a frame for this window.
func (p *Provider) SetWake(f func()) {
	p.wake = f
}

func pushGesture(view uintptr, magnify float32, smart bool) {
	if debug {
		println("dot gesture: view", view, "magnify", magnify, "smart", smart)
	}
	gestures.Lock()
	q := queue(view)
	if smart {
		q.smart++
	} else {
		q.zoom *= 1 + magnify
	}
	wake := q.wake
	schedule := wake != nil && !q.waking
	q.waking = q.waking || schedule
	gestures.Unlock()
	// Never call wake (window.Invalidate) here: native hooks run on the
	// main thread inside AppKit's event dispatch, and Invalidate waits for
	// that same event loop — calling it inline deadlocks the app.
	if schedule {
		go wake()
	}
}

var debug = os.Getenv("DOT_DEBUG") != ""

// TakeGestures returns and clears this window's pending pinch zoom and
// smart-zoom taps.
func (p *Provider) TakeGestures() (zoom float32, smart int) {
	view := p.view()
	if view == 0 {
		return 1, 0
	}
	gestures.Lock()
	defer gestures.Unlock()
	q := queue(view)
	q.wake = p.wake
	zoom, smart = q.zoom, q.smart
	q.zoom, q.smart, q.waking = 1, 0, false
	return zoom, smart
}
