package canvas

import (
	"image/color"
	"path/filepath"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/paint"

	"dot/internal/document"
	"dot/internal/pdf"
)

// PDF page backgrounds are rasterised by PDFium off the UI goroutine.
// The resolution follows the zoom in steps, so text stays crisp when you
// zoom in, and only recently visible pages stay in memory.

type pdfKey struct {
	path string
	page int
}

type pdfEntry struct {
	img      paint.ImageOp
	dpi      int // resolution of img; 0 = none yet
	pending  int // resolution being rendered; 0 = idle
	failed   bool
	gen      int // bumps on every finished render
	lastUsed uint64
}

// pdfs is shared by all windows; each canvas compares generations to know
// which of its pages to refresh.
var pdfs = struct {
	sync.Mutex
	m map[pdfKey]*pdfEntry
}{m: map[pdfKey]*pdfEntry{}}

var dpiSteps = []int{72, 108, 144, 216, 288}

const pdfKeep = 10 // rendered pages kept in memory

// wantPDF asks for page key at a resolution suiting px-per-pt scale.
func (c *Canvas) wantPDF(key pdfKey, scale float32) {
	want := dpiSteps[len(dpiSteps)-1]
	for _, d := range dpiSteps {
		if float32(d) >= scale*72*0.9 {
			want = d
			break
		}
	}
	pdfs.Lock()
	e := pdfs.m[key]
	if e == nil {
		e = &pdfEntry{}
		pdfs.m[key] = e
	}
	e.lastUsed = c.frame
	start := !e.failed && e.dpi < want && e.pending < want
	if start {
		e.pending = want
	}
	pdfs.Unlock()
	if start {
		go renderPDF(key, want, c.Wake)
	}
}

func renderPDF(key pdfKey, dpi int, wake func()) {
	img, err := pdf.Render(key.path, key.page, dpi)
	pdfs.Lock()
	if e := pdfs.m[key]; e != nil {
		if err != nil {
			e.failed = true
		} else if dpi >= e.dpi {
			e.img, e.dpi = paint.NewImageOp(img), dpi
		}
		if e.pending == dpi {
			e.pending = 0
		}
		e.gen++
	}
	pdfs.Unlock()
	if wake != nil {
		wake()
	}
}

// RenderPDFNow renders synchronously (for PNG export).
func RenderPDFNow(path string, page, dpi int) {
	key := pdfKey{path, page}
	pdfs.Lock()
	e := pdfs.m[key]
	if e == nil {
		e = &pdfEntry{}
		pdfs.m[key] = e
	}
	have := e.dpi
	pdfs.Unlock()
	if have < dpi {
		renderPDF(key, dpi, nil)
	}
}

// syncPDFs runs each frame: it requests renders for visible PDF pages,
// refreshes pages whose render finished, and evicts old ones.
func (c *Canvas) syncPDFs(visible []*document.Page) {
	for _, p := range visible {
		if b := p.Background; b.Type == "pdf" && b.PDFPath != "" {
			c.wantPDF(pdfKey{b.PDFPath, b.PDFPage}, c.scale)
		}
	}
	if c.pdfSeen == nil {
		c.pdfSeen = map[pdfKey]int{}
	}
	pdfs.Lock()
	finished := map[pdfKey]bool{}
	for k, e := range pdfs.m {
		if e.gen != c.pdfSeen[k] {
			c.pdfSeen[k] = e.gen
			finished[k] = true
		}
	}
	if len(pdfs.m) > pdfKeep {
		for k, e := range pdfs.m {
			if len(pdfs.m) <= pdfKeep {
				break
			}
			if c.frame-e.lastUsed > 30 && e.pending == 0 {
				delete(pdfs.m, k)
			}
		}
	}
	pdfs.Unlock()
	if len(finished) == 0 {
		return
	}
	for _, p := range c.Doc.Pages {
		if b := p.Background; b.Type == "pdf" && finished[pdfKey{b.PDFPath, b.PDFPage}] {
			p.Touch() // re-record with the new image
		}
	}
}

// drawPDFBackground paints the page's PDF (white until it's rendered).
func drawPDFBackground(ops *op.Ops, p *document.Page) {
	b := p.Background
	pdfs.Lock()
	e := pdfs.m[pdfKey{b.PDFPath, b.PDFPage}]
	var img paint.ImageOp
	var failed bool
	if e != nil {
		img, failed = e.img, e.failed
	}
	pdfs.Unlock()
	if failed {
		msg := &document.RawElement{Name: "text"}
		msg.SetAttr("size", "11")
		msg.SetAttr("x", "24")
		msg.SetAttr("y", "24")
		msg.SetAttr("color", document.ColorString(color.NRGBA{0xc0, 0x30, 0x30, 0xff}))
		msg.SetText("PDF not found: " + filepath.Base(b.PDFPath))
		DrawText(ops, msg)
		return
	}
	sz := img.Size()
	if sz.X == 0 {
		return // still rendering
	}
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(p.Width/float32(sz.X), p.Height/float32(sz.Y)))).Push(ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(ops)
	paint.PaintOp{}.Add(ops)
}
