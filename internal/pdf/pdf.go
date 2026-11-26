// Package pdf reads, renders and writes PDFs with PDFium (the engine in
// Chrome), compiled to WebAssembly and run in-process by wazero: no C
// libraries, no installs, identical on macOS and Windows.
package pdf

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/structs"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

var eng struct {
	sync.Mutex
	pool pdfium.Pool
	inst pdfium.Pdfium
	err  error
	docs map[string]references.FPDF_DOCUMENT // open source PDFs by path
}

// instance starts PDFium on first use. Compiled WebAssembly is cached on
// disk, so only the very first launch pays the compile cost.
// Callers must hold eng.
func instance() (pdfium.Pdfium, error) {
	if eng.inst != nil || eng.err != nil {
		return eng.inst, eng.err
	}
	// PDFium is built with WebAssembly exception handling.
	rc := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	if dir, err := os.UserCacheDir(); err == nil {
		if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(dir, "dot", "pdfium")); err == nil {
			rc = rc.WithCompilationCache(cache)
		}
	}
	pool, err := webassembly.Init(webassembly.Config{
		Context: context.Background(), MinIdle: 1, MaxIdle: 1, MaxTotal: 1, RuntimeConfig: rc,
	})
	if err != nil {
		eng.err = fmt.Errorf("start PDF engine: %w", err)
		return nil, eng.err
	}
	inst, err := pool.GetInstance(time.Minute)
	if err != nil {
		eng.err = fmt.Errorf("start PDF engine: %w", err)
		return nil, eng.err
	}
	eng.pool, eng.inst, eng.docs = pool, inst, map[string]references.FPDF_DOCUMENT{}
	return inst, nil
}

// open returns the (cached) document for path. Callers must hold eng.
func open(path string) (pdfium.Pdfium, references.FPDF_DOCUMENT, error) {
	inst, err := instance()
	if err != nil {
		return nil, "", err
	}
	if d, ok := eng.docs[path]; ok {
		return inst, d, nil
	}
	data, err := os.ReadFile(path) // bytes, not a path: works on any drive
	if err != nil {
		return nil, "", err
	}
	res, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	eng.docs[path] = res.Document
	return inst, res.Document, nil
}

// PageSizes returns each page's size in points (as displayed, i.e. with
// page rotation applied).
func PageSizes(path string) ([][2]float32, error) {
	eng.Lock()
	defer eng.Unlock()
	inst, doc, err := open(path)
	if err != nil {
		return nil, err
	}
	n, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc})
	if err != nil {
		return nil, err
	}
	sizes := make([][2]float32, n.PageCount)
	for i := range sizes {
		s, err := inst.FPDF_GetPageSizeByIndexF(&requests.FPDF_GetPageSizeByIndexF{Document: doc, Index: i})
		if err != nil {
			return nil, err
		}
		sizes[i] = [2]float32{s.Size.Width, s.Size.Height}
	}
	return sizes, nil
}

// Render rasterises one page (0-based) at dpi. The image is a copy the
// caller owns.
func Render(path string, page, dpi int) (*image.RGBA, error) {
	eng.Lock()
	defer eng.Unlock()
	inst, doc, err := open(path)
	if err != nil {
		return nil, err
	}
	res, err := inst.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI:  dpi,
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: page}},
	})
	if err != nil {
		return nil, err
	}
	defer res.CleanupFunc() // the pixels live in WebAssembly memory until then
	src := res.Result.RenderedImage
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out, nil
}

// Source says where an output page comes from: page Page (0-based) of the
// PDF at Path, or (Path == "") the same page of the plain notebook PDF.
type Source struct {
	Path string
	Page int
}

// Overlay is what gets drawn on top of an original PDF page, in points
// with a top-left origin (like the notebook).
type Overlay struct {
	Fills  []Fill
	Lines  []Line
	Texts  []Text
	Images []Image
}

// Line is a round-capped polyline of one width (pressure strokes are
// split into runs of near-constant width by the caller).
type Line struct {
	Points [][2]float32
	Width  float32
	Color  color.NRGBA
}

// Fill paints the inside of closed outlines, as one shape with the
// non-zero rule (overlapping outlines are painted once).
type Fill struct {
	Paths [][][2]float32
	Color color.NRGBA
}

type Text struct {
	X, Y, Size float32 // top-left of the first line
	Font       string  // a standard PDF font, e.g. "Times-BoldItalic"
	Color      color.NRGBA
	Lines      []string
	Offsets    []float32 // per-line x offset (centred labels); may be nil
	// Embedded TrueType font (handwriting fonts) and a key to load it once.
	FontKey  string
	FontData []byte
}

type Image struct {
	Box  [4]float32 // left, top, right, bottom
	JPEG []byte
}

// Merge writes the final annotated PDF. plain is the notebook rendered by
// dot!'s own writer (used for pages without a PDF background). Pages with
// a PDF background are imported untouched — their text stays real,
// selectable text — and the overlay is drawn onto them as native PDF
// paths, text and images.
func Merge(w io.Writer, plain []byte, pages []Source, overlays []Overlay) error {
	eng.Lock()
	defer eng.Unlock()
	inst, err := instance()
	if err != nil {
		return err
	}
	pl, err := inst.OpenDocument(&requests.OpenDocument{File: &plain})
	if err != nil {
		return err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: pl.Document})
	out, err := inst.FPDF_CreateNewDocument(&requests.FPDF_CreateNewDocument{})
	if err != nil {
		return err
	}
	dst := out.Document
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: dst})

	for i, src := range pages {
		from, idx := pl.Document, i
		if src.Path != "" {
			_, sdoc, err := open(src.Path)
			if err != nil {
				return err
			}
			from, idx = sdoc, src.Page
		}
		if _, err := inst.FPDF_ImportPagesByIndex(&requests.FPDF_ImportPagesByIndex{
			Source: from, Destination: dst, PageIndices: []int{idx}, Index: i}); err != nil {
			return err
		}
	}
	for i, src := range pages {
		if src.Path == "" || i >= len(overlays) {
			continue
		}
		if err := stamp(inst, dst, i, overlays[i]); err != nil {
			return fmt.Errorf("page %d: %w", i+1, err)
		}
	}
	_, err = inst.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{Document: dst, FileWriter: w})
	return err
}

func pdfColor(c color.NRGBA) structs.FPDF_COLOR {
	return structs.FPDF_COLOR{R: uint(c.R), G: uint(c.G), B: uint(c.B), A: uint(c.A)}
}

// stamp draws ov onto page i of doc.
func stamp(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, i int, ov Overlay) error {
	pg, err := inst.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: doc, Index: i})
	if err != nil {
		return err
	}
	defer inst.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: pg.Page})
	ref := requests.Page{ByReference: &pg.Page}
	sz, err := inst.FPDF_GetPageSizeByIndexF(&requests.FPDF_GetPageSizeByIndexF{Document: doc, Index: i})
	if err != nil {
		return err
	}
	h := sz.Size.Height
	insert := func(obj references.FPDF_PAGEOBJECT) error {
		_, err := inst.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: ref, PageObject: obj})
		return err
	}
	for _, fl := range ov.Fills {
		var obj references.FPDF_PAGEOBJECT
		for _, pts := range fl.Paths {
			if len(pts) < 3 {
				continue
			}
			p0 := pts[0]
			if obj == "" {
				path, err := inst.FPDFPageObj_CreateNewPath(&requests.FPDFPageObj_CreateNewPath{X: p0[0], Y: h - p0[1]})
				if err != nil {
					return err
				}
				obj = path.PageObject
			} else {
				inst.FPDFPath_MoveTo(&requests.FPDFPath_MoveTo{PageObject: obj, X: p0[0], Y: h - p0[1]})
			}
			for _, p := range pts[1:] {
				inst.FPDFPath_LineTo(&requests.FPDFPath_LineTo{PageObject: obj, X: p[0], Y: h - p[1]})
			}
			inst.FPDFPath_Close(&requests.FPDFPath_Close{PageObject: obj})
		}
		if obj == "" {
			continue
		}
		inst.FPDFPageObj_SetFillColor(&requests.FPDFPageObj_SetFillColor{PageObject: obj, FillColor: pdfColor(fl.Color)})
		inst.FPDFPath_SetDrawMode(&requests.FPDFPath_SetDrawMode{PageObject: obj, FillMode: enums.FPDF_FILLMODE_WINDING})
		if err := insert(obj); err != nil {
			return err
		}
	}
	for _, ln := range ov.Lines {
		if len(ln.Points) == 0 {
			continue
		}
		p0 := ln.Points[0]
		path, err := inst.FPDFPageObj_CreateNewPath(&requests.FPDFPageObj_CreateNewPath{X: p0[0], Y: h - p0[1]})
		if err != nil {
			return err
		}
		obj := path.PageObject
		pts := ln.Points[1:]
		if len(pts) == 0 { // a dot
			pts = [][2]float32{{p0[0] + 0.01, p0[1]}}
		}
		for _, p := range pts {
			if _, err := inst.FPDFPath_LineTo(&requests.FPDFPath_LineTo{PageObject: obj, X: p[0], Y: h - p[1]}); err != nil {
				return err
			}
		}
		inst.FPDFPageObj_SetStrokeColor(&requests.FPDFPageObj_SetStrokeColor{PageObject: obj, StrokeColor: pdfColor(ln.Color)})
		inst.FPDFPageObj_SetStrokeWidth(&requests.FPDFPageObj_SetStrokeWidth{PageObject: obj, StrokeWidth: ln.Width})
		inst.FPDFPageObj_SetLineCap(&requests.FPDFPageObj_SetLineCap{PageObject: obj, LineCap: enums.FPDF_LINECAP_ROUND})
		inst.FPDFPageObj_SetLineJoin(&requests.FPDFPageObj_SetLineJoin{PageObject: obj, LineJoin: enums.FPDF_LINEJOIN_ROUND})
		inst.FPDFPath_SetDrawMode(&requests.FPDFPath_SetDrawMode{PageObject: obj, FillMode: enums.FPDF_FILLMODE_NONE, Stroke: true})
		if err := insert(obj); err != nil {
			return err
		}
	}
	for _, im := range ov.Images {
		obj, err := inst.FPDFPageObj_NewImageObj(&requests.FPDFPageObj_NewImageObj{Document: doc})
		if err != nil {
			return err
		}
		if _, err := inst.FPDFImageObj_LoadJpegFileInline(&requests.FPDFImageObj_LoadJpegFileInline{
			ImageObject: obj.PageObject, FileData: im.JPEG}); err != nil {
			return err
		}
		b := im.Box
		inst.FPDFImageObj_SetMatrix(&requests.FPDFImageObj_SetMatrix{ImageObject: obj.PageObject,
			Transform: structs.FPDF_FS_MATRIX{A: b[2] - b[0], D: b[3] - b[1], E: b[0], F: h - b[3]}})
		if err := insert(obj.PageObject); err != nil {
			return err
		}
	}
	loaded := map[string]references.FPDF_FONT{}
	for _, t := range ov.Texts {
		var font references.FPDF_FONT
		if t.FontData != nil {
			if f, ok := loaded[t.FontKey]; ok {
				font = f
			} else if r, err := inst.FPDFText_LoadFont(&requests.FPDFText_LoadFont{Document: doc, Data: t.FontData, FontType: enums.FPDF_FONT_TRUETYPE}); err == nil {
				font, loaded[t.FontKey] = r.Font, r.Font
			}
		}
		for k, line := range t.Lines {
			if line == "" {
				continue
			}
			var objRef references.FPDF_PAGEOBJECT
			if font != "" {
				o, err := inst.FPDFPageObj_CreateTextObj(&requests.FPDFPageObj_CreateTextObj{Document: doc, Font: font, FontSize: t.Size})
				if err != nil {
					return err
				}
				objRef = o.PageObject
			} else {
				o, err := inst.FPDFPageObj_NewTextObj(&requests.FPDFPageObj_NewTextObj{Document: doc, Font: t.Font, FontSize: t.Size})
				if err != nil {
					return err
				}
				objRef = o.PageObject
			}
			obj := struct{ PageObject references.FPDF_PAGEOBJECT }{objRef}
			inst.FPDFText_SetText(&requests.FPDFText_SetText{PageObject: obj.PageObject, Text: line})
			inst.FPDFPageObj_SetFillColor(&requests.FPDFPageObj_SetFillColor{PageObject: obj.PageObject, FillColor: pdfColor(t.Color)})
			baseline := t.Y + t.Size*0.9 + float32(k)*t.Size*1.15
			dx := float32(0)
			if k < len(t.Offsets) {
				dx = t.Offsets[k]
			}
			inst.FPDFPageObj_Transform(&requests.FPDFPageObj_Transform{PageObject: obj.PageObject,
				Transform: structs.FPDF_FS_MATRIX{A: 1, D: 1, E: t.X + dx, F: h - baseline}})
			if err := insert(obj.PageObject); err != nil {
				return err
			}
		}
	}
	_, err = inst.FPDFPage_GenerateContent(&requests.FPDFPage_GenerateContent{Page: ref})
	return err
}
