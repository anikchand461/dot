package canvas

import (
	"image"
	"image/draw"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"

	"dot/internal/document"
)

// When pages are small on screen (zoomed out over a long notebook), each
// is drawn from a cached picture instead of re-drawing every stroke every
// frame. Pictures are rendered on a background goroutine at a few fixed
// widths; a page being edited is drawn as vectors until its picture is
// refreshed (after the edits pause).

const (
	rasterBelow = 0.5                    // px per pt: smaller pages use pictures
	rasterPause = 300 * time.Millisecond // wait for edits to settle
)

var rasterWidths = []int{96, 192, 384}

type pageBitmap struct {
	img       paint.ImageOp
	size      image.Point
	ver       uint64 // page version the picture shows
	w         int
	pending   bool
	seenVer   uint64
	changedAt time.Time
	lastUsed  uint64
}

type rasterJob struct {
	key  *document.Page
	snap *document.Page
	ver  uint64
	w    int
}

var raster = struct {
	sync.Mutex
	m    map[*document.Page]*pageBitmap
	jobs chan rasterJob
	wake func()
}{m: map[*document.Page]*pageBitmap{}}

func rasterWorker() {
	var win *headless.Window
	var size image.Point
	sh := text.NewShaper(text.WithCollection(collection()))
	render := func(job rasterJob) (*image.RGBA, image.Point) {
		p := job.snap
		s := float32(job.w) / p.Width
		h := int(p.Height*s + 0.5)
		if win == nil || size != image.Pt(job.w, h) {
			if win != nil {
				win.Release()
				win = nil
			}
			w, err := headless.NewWindow(job.w, h)
			if err != nil {
				return nil, image.Point{}
			}
			win, size = w, image.Pt(job.w, h)
		}
		var ops op.Ops
		t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(&ops)
		DrawPageLOD(&ops, p, nil, sh, 0.8/s)
		t.Pop()
		if win.Frame(&ops) != nil {
			return nil, image.Point{}
		}
		img := image.NewRGBA(image.Rectangle{Max: size})
		if win.Screenshot(img) != nil {
			return nil, image.Point{}
		}
		out := image.NewRGBA(img.Bounds())
		draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
		return out, size
	}
	for job := range raster.jobs {
		img, sz := render(job)
		raster.Lock()
		if e := raster.m[job.key]; e != nil {
			e.pending = false // also on failure, so it can be retried
			if img != nil {
				e.img, e.size, e.ver, e.w = paint.NewImageOp(img), sz, job.ver, job.w
			}
		}
		wake := raster.wake
		raster.Unlock()
		if wake != nil {
			wake()
		}
	}
}

// pageRaster returns a picture of p good for scale (px/pt), requesting
// one if needed. ok is false when the caller should draw vectors.
func (c *Canvas) pageRaster(p *document.Page, scale float32) (paint.ImageOp, image.Point, bool, time.Time) {
	need := int(p.Width * scale)
	w := rasterWidths[len(rasterWidths)-1]
	for _, rw := range rasterWidths {
		if rw >= need {
			w = rw
			break
		}
	}
	raster.Lock()
	defer raster.Unlock()
	if raster.jobs == nil {
		raster.jobs = make(chan rasterJob, 256)
		go rasterWorker()
	}
	raster.wake = c.Wake
	e := raster.m[p]
	if e == nil {
		e = &pageBitmap{seenVer: p.Version}
		raster.m[p] = e
	}
	e.lastUsed = c.frame
	if e.seenVer != p.Version {
		e.seenVer, e.changedAt = p.Version, c.now
	}
	fresh := e.ver == p.Version && e.size.X > 0 && e.w >= w
	if !fresh && !e.pending && c.now.Sub(e.changedAt) >= rasterPause {
		e.pending = true
		raster.jobs <- rasterJob{key: p, snap: p.Snapshot(), ver: p.Version, w: w}
	}
	return e.img, e.size, fresh, e.changedAt.Add(rasterPause)
}

// evictRasters drops pictures not shown for a while.
func (c *Canvas) evictRasters() {
	raster.Lock()
	for p, e := range raster.m {
		if c.frame-e.lastUsed > cacheKeep && !e.pending {
			delete(raster.m, p)
		}
	}
	raster.Unlock()
}

func resetRasters() {
	raster.Lock()
	for p, e := range raster.m {
		if !e.pending {
			delete(raster.m, p)
		}
	}
	raster.Unlock()
}
