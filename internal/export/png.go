package export

import (
	"image"
	"image/png"
	"io"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/op"

	"dot/internal/canvas"
	"dot/internal/document"
)

// PNG renders one page with the same GPU renderer as the screen, at
// scale pixels per pt (scale 2 ≈ 144 dpi).
func PNG(w io.Writer, p *document.Page, scale float32) error {
	size := image.Pt(int(p.Width*scale+0.5), int(p.Height*scale+0.5))
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		return err
	}
	defer win.Release()
	if b := p.Background; b.Type == "pdf" && b.PDFPath != "" {
		canvas.RenderPDFNow(b.PDFPath, b.PDFPage, int(72*scale))
	}
	var ops op.Ops
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(scale, scale))).Push(&ops)
	canvas.DrawPage(&ops, p, nil)
	t.Pop()
	if err := win.Frame(&ops); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		return err
	}
	return png.Encode(w, img)
}
