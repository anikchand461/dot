// Command icons generates dot!'s logo and app icons from assets/logo.png
// (a black mark on a transparent background). Run scripts/make-icons.sh.
//
// Outputs:
//
//	internal/ui/assets/logo-mark.png   white mark used inside the app
//	assets/icon-1024.png               app icon (white mark on purple)
//	assets/icon-{16..512}.png          sizes for Linux and Windows
//	assets/dot.ico                     Windows icon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	xdraw "golang.org/x/image/draw"
)

func load(p string) image.Image {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	return img
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func save(p string, img image.Image) {
	if err := os.WriteFile(p, encode(img), 0o644); err != nil {
		panic(err)
	}
}

// inkBounds is the bounding box of the non-transparent pixels.
func inkBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	r := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x1000 {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}

func tint(src image.Image, c color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBA{c.R, c.G, c.B, uint8(uint32(c.A) * (a >> 8) / 255)})
		}
	}
	return out
}

func scaled(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func roundedSquare(size, inset, radius int, top, bottom color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	lo, hi, r := float64(inset), float64(size-inset), float64(radius)
	for y := 0; y < size; y++ {
		t := (float64(y) - lo) / (hi - lo)
		c := color.NRGBA{
			uint8(float64(top.R) + (float64(bottom.R)-float64(top.R))*t),
			uint8(float64(top.G) + (float64(bottom.G)-float64(top.G))*t),
			uint8(float64(top.B) + (float64(bottom.B)-float64(top.B))*t), 255}
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			cx := math.Max(lo+r, math.Min(hi-r, fx))
			cy := math.Max(lo+r, math.Min(hi-r, fy))
			a := math.Max(0, math.Min(1, r-math.Hypot(fx-cx, fy-cy)+0.5))
			if fx < lo || fx > hi || fy < lo || fy > hi {
				a = 0
			}
			if a > 0 {
				cc := c
				cc.A = uint8(255 * a)
				img.SetNRGBA(x, y, cc)
			}
		}
	}
	return img
}

// writeICO packs PNG images into a Windows .ico file.
func writeICO(path string, imgs []image.Image) {
	var hdr, data bytes.Buffer
	binary.Write(&hdr, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for _, img := range imgs {
		p := encode(img)
		w := img.Bounds().Dx()
		dim := uint8(w)
		if w >= 256 {
			dim = 0
		}
		binary.Write(&hdr, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(p)), uint32(offset)})
		data.Write(p)
		offset += len(p)
	}
	os.WriteFile(path, append(hdr.Bytes(), data.Bytes()...), 0o644)
}

func main() {
	src := load("assets/logo.png")
	b := inkBounds(src)
	pad := max(b.Dx(), b.Dy()) / 60
	b = b.Inset(-pad).Intersect(src.Bounds())
	mark := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(mark, mark.Bounds(), src, b.Min, draw.Src)

	// In-app mark: white (the app is dark), 256 px tall.
	h := 256
	w := h * mark.Bounds().Dx() / mark.Bounds().Dy()
	save("internal/ui/assets/logo-mark.png", scaled(tint(mark, color.NRGBA{255, 255, 255, 255}), w, h))

	// App icon: white mark on the dot! purple rounded square.
	const S = 1024
	icon := roundedSquare(S, 100, 185, color.NRGBA{0x9b, 0x7f, 0xff, 255}, color.NRGBA{0x55, 0x3e, 0xd8, 255})
	mw := 520
	mh := mw * mark.Bounds().Dy() / mark.Bounds().Dx()
	m := scaled(tint(mark, color.NRGBA{255, 255, 255, 255}), mw, mh)
	draw.Draw(icon, m.Bounds().Add(image.Pt((S-mw)/2, (S-mh)/2)), m, image.Point{}, draw.Over)
	save("assets/icon-1024.png", icon)

	var ico []image.Image
	for _, s := range []int{16, 32, 48, 64, 128, 256, 512} {
		img := scaled(icon, s, s)
		save(fmt.Sprintf("assets/icon-%d.png", s), img)
		if s <= 256 {
			ico = append(ico, img)
		}
	}
	writeICO("assets/dot.ico", ico)
	fmt.Println("icons written")
}
