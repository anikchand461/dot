package canvas

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"

	"dot/internal/document"
)

// Background pictures are decoded once, shrunk to at most maxBG pixels on
// the long side (plenty for a page, and kind to memory), and shared by all
// windows.
const maxBG = 2400

type bgImage struct {
	op     paint.ImageOp
	size   image.Point
	failed bool
}

var bgImages = struct {
	sync.Mutex
	m map[string]*bgImage
}{m: map[string]*bgImage{}}

// LoadBackgroundImage returns the decoded (and downscaled) picture at path.
func LoadBackgroundImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if long := max(b.Dx(), b.Dy()); long > maxBG {
		s := float64(maxBG) / float64(long)
		dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*s), int(float64(b.Dy())*s)))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	return img, nil
}

func bgFor(path string) *bgImage {
	bgImages.Lock()
	defer bgImages.Unlock()
	if e, ok := bgImages.m[path]; ok {
		return e
	}
	e := &bgImage{}
	if img, err := LoadBackgroundImage(path); err != nil {
		e.failed = true
	} else {
		e.op, e.size = paint.NewImageOp(img), img.Bounds().Size()
	}
	bgImages.m[path] = e
	return e
}

// CoverRect is where a w×h picture goes to fill a pw×ph page, cropping
// the overflow (like CSS background-size: cover).
func CoverRect(w, h int, pw, ph float32) (x, y, dw, dh float32) {
	s := max(pw/float32(w), ph/float32(h))
	dw, dh = float32(w)*s, float32(h)*s
	return (pw - dw) / 2, (ph - dh) / 2, dw, dh
}

// drawImageBackground fills the page with its picture, then softens it.
func drawImageBackground(ops *op.Ops, p *document.Page) {
	b := p.Background
	e := bgFor(b.ImagePath)
	if !e.failed && e.size.X > 0 {
		x, y, dw, dh := CoverRect(e.size.X, e.size.Y, p.Width, p.Height)
		t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(dw/float32(e.size.X), dh/float32(e.size.Y))).Offset(f32.Pt(x, y))).Push(ops)
		img := e.op
		img.Filter = paint.FilterLinear
		img.Add(ops)
		paint.PaintOp{}.Add(ops)
		t.Pop()
	}
	if b.Soften > 0 {
		c := b.Color
		c.A = uint8(min(0.95, b.Soften) * 255)
		paint.FillShape(ops, c, clip.Outline{Path: rectPath(ops, 0, 0, p.Width, p.Height)}.Op())
	}
}
