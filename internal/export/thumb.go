package export

import (
	"image"
	"image/draw"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/text"

	"dot/internal/canvas"
	"dot/internal/document"
)

// One offscreen GPU surface is reused for all thumbnails (creating a GPU
// context per thumbnail would be slow).
var thumbs struct {
	sync.Mutex
	win    *headless.Window
	size   image.Point
	shaper *text.Shaper
	idle   *time.Timer
}

// releaseAfter frees the offscreen GPU surface once thumbnails have not
// been needed for a while, so an idle app holds no extra GPU memory.
const releaseAfter = 15 * time.Second

func releaseIdle() {
	thumbs.Lock()
	defer thumbs.Unlock()
	if thumbs.win != nil {
		thumbs.win.Release()
		thumbs.win = nil
	}
}

// Thumbnail renders page p scaled to fit a w×h image (the page keeps its
// aspect ratio; the rest is transparent).
func Thumbnail(p *document.Page, w, h int) (image.Image, error) {
	thumbs.Lock()
	defer thumbs.Unlock()
	if thumbs.idle == nil {
		thumbs.idle = time.AfterFunc(releaseAfter, releaseIdle)
	} else {
		thumbs.idle.Reset(releaseAfter)
	}
	size := image.Pt(w, h)
	if thumbs.win == nil || thumbs.size != size {
		if thumbs.win != nil {
			thumbs.win.Release()
		}
		win, err := headless.NewWindow(w, h)
		if err != nil {
			return nil, err
		}
		thumbs.win, thumbs.size = win, size
	}
	if b := p.Background; b.Type == "pdf" && b.PDFPath != "" {
		canvas.RenderPDFNow(b.PDFPath, b.PDFPage, 54)
	}
	s := min(float32(w)/p.Width, float32(h)/p.Height)
	var ops op.Ops
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(&ops)
	if thumbs.shaper == nil {
		thumbs.shaper = canvas.NewShaper()
	}
	canvas.DrawPageLOD(&ops, p, nil, thumbs.shaper, 0.8/s) // detail down to ~1 px
	t.Pop()
	if err := thumbs.win.Frame(&ops); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := thumbs.win.Screenshot(img); err != nil {
		return nil, err
	}
	// Crop to the page itself.
	pw, ph := int(p.Width*s+0.5), int(p.Height*s+0.5)
	out := image.NewRGBA(image.Rect(0, 0, pw, ph))
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
	return out, nil
}
