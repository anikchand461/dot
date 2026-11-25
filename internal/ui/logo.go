package ui

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/paint"
)

// The dot! mark (a dot drawn in pen strokes), white on transparent.
//
//go:embed assets/logo-mark.png
var logoPNG []byte

var logo struct {
	once sync.Once
	op   paint.ImageOp
	dark paint.ImageOp // the same mark in black, for the outline
	size image.Point
}

// logoMark draws the mark h pixels tall and returns its size.
func logoMark(gtx C, h int) D {
	logo.once.Do(func() {
		if img, err := png.Decode(bytes.NewReader(logoPNG)); err == nil {
			logo.op, logo.size = paint.NewImageOp(img), img.Bounds().Size()
			b := img.Bounds()
			dark := image.NewNRGBA(b)
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					_, _, _, a := img.At(x, y).RGBA()
					dark.SetNRGBA(x, y, color.NRGBA{0, 0, 0, uint8(a >> 8)})
				}
			}
			logo.dark = paint.NewImageOp(dark)
		}
	})
	if logo.size.Y == 0 || h == 0 {
		return D{}
	}
	s := float32(h) / float32(logo.size.Y)
	w := int(float32(logo.size.X) * s)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(gtx.Ops)
	img := logo.op
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// logoMarkOutlined draws the mark with a thin dark outline and no
// background, so it reads on white and black paper alike.
func logoMarkOutlined(gtx C, h int) D {
	logoMark(gtx, 0) // load
	if logo.size.Y == 0 {
		return D{}
	}
	s := float32(h) / float32(logo.size.Y)
	w := int(float32(logo.size.X) * s)
	draw := func(img paint.ImageOp, dx, dy float32) {
		t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(dx, dy))).Push(gtx.Ops)
		img.Filter = paint.FilterLinear
		img.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		t.Pop()
	}
	r := float32(gtx.Dp(1.2))
	for _, d := range [][2]float32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-.7, -.7}, {.7, -.7}, {-.7, .7}, {.7, .7}} {
		draw(logo.dark, d[0]*r, d[1]*r)
	}
	draw(logo.op, 0, 0)
	return D{Size: image.Pt(w, h)}
}

// The GitHub mark, white on transparent.
//
//go:embed assets/github-mark.png
var githubPNG []byte

// RepoURL is dot!'s source code and downloads.
const RepoURL = "https://github.com/anikchand461/dot"

var github struct {
	once   sync.Once
	mask   image.Image
	tinted map[color.NRGBA]paint.ImageOp
}

// githubGlyph draws the GitHub mark in col.
func githubGlyph(gtx C, sz int, col color.NRGBA) {
	github.once.Do(func() {
		github.mask, _ = png.Decode(bytes.NewReader(githubPNG))
		github.tinted = map[color.NRGBA]paint.ImageOp{}
	})
	if github.mask == nil {
		return
	}
	img, ok := github.tinted[col]
	if !ok {
		b := github.mask.Bounds()
		t := image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				_, _, _, a := github.mask.At(x, y).RGBA()
				t.SetNRGBA(x, y, color.NRGBA{col.R, col.G, col.B, uint8(uint32(col.A) * (a >> 8) / 255)})
			}
		}
		img = paint.NewImageOp(t)
		github.tinted[col] = img
	}
	s := float32(sz) / float32(github.mask.Bounds().Dx())
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
