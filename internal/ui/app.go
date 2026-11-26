// Package ui is dot!'s interface: window loop, floating tool dock,
// style panel, pages panel, page setup, menus and file handling.
package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"dot/internal/canvas"
	"dot/internal/clipboard"
	"dot/internal/document"
	"dot/internal/export"
	"dot/internal/ink"
	"dot/internal/input"
	"dot/internal/pdf"
	"dot/internal/settings"
)

const autosaveEvery = 20 * time.Second

var debugKeys = os.Getenv("DOT_DEBUG") != ""

var (
	penPalette = []color.NRGBA{
		{0x10, 0x10, 0x10, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0x8a, 0x8d, 0x96, 0xff}, {0xe5, 0x39, 0x35, 0xff},
		{0xff, 0x8c, 0x00, 0xff}, {0xff, 0xd6, 0x00, 0xff}, {0x00, 0xff, 0x00, 0xff}, {0x2e, 0xa0, 0x43, 0xff},
		{0x00, 0xc0, 0xff, 0xff}, {0x33, 0x66, 0xff, 0xff}, {0x5c, 0x4d, 0xff, 0xff}, {0x9c, 0x27, 0xb0, 0xff},
		{0xff, 0x00, 0xff, 0xff}, {0xff, 0x6e, 0xa7, 0xff}, {0x8d, 0x5b, 0x3a, 0xff}, {0x00, 0x96, 0x88, 0xff},
	}
	hiPalette = []color.NRGBA{
		{0xff, 0xe0, 0x00, 0x7f}, {0x7c, 0xff, 0x4f, 0x7f}, {0xff, 0x6e, 0xc7, 0x7f},
		{0x4f, 0xc3, 0xff, 0x7f}, {0xff, 0xa0, 0x40, 0x7f}, {0xb3, 0x88, 0xff, 0x7f},
	}
	textSizes = []float32{10, 12, 14, 18, 24, 32}

	// Paper colours: light, tinted and dark.
	paperPresets = [...]struct {
		name string
		c    color.NRGBA
	}{
		{"White", document.White}, {"Ivory", color.NRGBA{0xfb, 0xf8, 0xef, 0xff}},
		{"Cream", color.NRGBA{0xf8, 0xf3, 0xe6, 0xff}}, {"Legal pad", color.NRGBA{0xff, 0xf6, 0xb8, 0xff}},
		{"Mint", color.NRGBA{0xe4, 0xf4, 0xe8, 0xff}}, {"Sky", color.NRGBA{0xe3, 0xef, 0xfb, 0xff}},
		{"Blush", color.NRGBA{0xfb, 0xe8, 0xec, 0xff}}, {"Lavender", color.NRGBA{0xee, 0xe8, 0xfb, 0xff}},
		{"Light grey", color.NRGBA{0xe9, 0xe9, 0xec, 0xff}}, {"Slate", color.NRGBA{0x26, 0x28, 0x2e, 0xff}},
		{"Navy", color.NRGBA{0x14, 0x1f, 0x3a, 0xff}}, {"Forest", color.NRGBA{0x15, 0x2e, 0x24, 0xff}},
		{"Chalkboard", color.NRGBA{0x2f, 0x3d, 0x35, 0xff}}, {"Charcoal", color.NRGBA{0x1b, 0x1b, 0x1e, 0xff}},
		{"Black", document.Black},
	}
)

// paste handles ⌘V: whatever was copied most recently wins — an image or
// text from another app, or ink copied inside dot!.
func (a *App) paste() {
	sysNewer := clipboard.ChangeCount() != a.copyMark
	if sysNewer || !a.cv.HasClipboard() {
		if img := clipboard.Image(); img != nil {
			if err := a.cv.InsertImage(img); err != nil {
				a.notify("Couldn't paste image: " + err.Error())
			} else {
				a.notify("Image pasted · drag corners to resize · Crop in the bar")
			}
			return
		}
	}
	if a.cv.HasClipboard() && !sysNewer {
		a.cv.Paste()
		return
	}
	if t := strings.TrimSpace(clipboard.Text()); t != "" {
		a.cv.InsertText(t)
		return
	}
	if a.cv.HasClipboard() {
		a.cv.Paste()
		return
	}
	a.notify("Nothing to paste")
}

func (a *App) insertImageFile() {
	a.async(func() func() {
		rc, err := a.exp.ChooseFile("png", "jpg", "jpeg", "gif")
		if err != nil {
			return nil
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return func() { a.notify("Couldn't read image: " + err.Error()) }
		}
		return func() {
			if err := a.cv.InsertImage(data); err != nil {
				a.notify("Couldn't insert image: " + err.Error())
			}
		}
	})
}

// newFromDefaults starts a notebook with the user's default page
// (Page setup → "Use this page for new notebooks"), or A4 lined white.
func newFromDefaults(s settings.Settings) *document.Document {
	d := document.New()
	if dp := s.DefaultPage; dp.W > 0 && dp.H > 0 {
		d.Pages[0] = document.NewPage(dp.W, dp.H, document.Style(dp.Style),
			color.NRGBA{dp.Color[0], dp.Color[1], dp.Color[2], dp.Color[3]})
		if dp.ImagePath != "" {
			bg := &d.Pages[0].Background
			bg.Type, bg.ImagePath, bg.Soften = "pixmap", dp.ImagePath, dp.Soften
		}
	}
	return d
}

// addRecent puts c at the front of the recent colours (max 10).
func (a *App) addRecent(c color.NRGBA) {
	c.A = 0xff
	out := []color.NRGBA{c}
	for _, r := range a.recentColors {
		if r != c && len(out) < len(a.recentClicks) {
			out = append(out, r)
		}
	}
	a.recentColors = out
}

// applyToolColor sets the colour for the active tool (highlighters keep
// their transparency); with the select tool it recolours the selection.
func (a *App) applyToolColor(c color.NRGBA, commit bool) {
	cv := a.cv
	switch cv.Tool {
	case canvas.Highlighter:
		c.A = 0x7f
		cv.HighlighterColor = c
	case canvas.Select:
		if commit {
			cv.RecolorSelection(c)
		}
	default:
		c.A = 0xff
		cv.PenColor = c
		if cv.Editing() { // recolour the text being typed
			f, b, i, sz, _ := cv.TextStyle()
			cv.SetTextStyle(f, b, i, sz, c)
		}
	}
	if commit {
		a.addRecent(c)
	}
}

// applyFill sets the fill for new shapes and the Fill tool, keeping the
// chosen opacity; with the Select tool it fills the selected shapes.
func (a *App) applyFill(c color.NRGBA, commit bool) {
	cv := a.cv
	if c != (color.NRGBA{}) {
		alpha := cv.FillColor.A
		if alpha == 0 {
			alpha = 0xff
		}
		c.A = alpha
	}
	if cv.Tool == canvas.Select {
		if commit {
			cv.RefillSelection(c)
		}
	} else {
		cv.FillColor = c
		if cv.Tool != canvas.Shape && cv.Tool != canvas.Fill {
			cv.SetTool(canvas.Fill)
		}
	}
	if commit && c.A > 0 {
		a.addRecent(c)
	}
}

// setPaperColor previews a paper colour live while the wheel is dragged
// and records one undo step when the drag ends.
func (a *App) setPaperColor(c color.NRGBA, commit bool) {
	if a.paperOld == nil {
		a.paperPages = a.scopePages()
		for _, p := range a.paperPages {
			a.paperOld = append(a.paperOld, p.Format())
		}
	}
	for _, p := range a.paperPages {
		p.Background.Color = c
		p.Touch()
	}
	if !commit {
		return
	}
	cmd := &document.SetFormat{Pages: a.paperPages, Old: a.paperOld}
	for _, p := range a.paperPages {
		cmd.New = append(cmd.New, p.Format())
	}
	a.hist.Push(a.doc, cmd)
	a.paperOld, a.paperPages = nil, nil
	// Keep ink readable on the new paper.
	if document.IsDark(c) == document.IsDark(a.cv.PenColor) {
		a.cv.PenColor = penPalette[map[bool]int{true: 1, false: 0}[document.IsDark(c)]]
	}
}

type App struct {
	w   *app.Window
	th  *material.Theme
	exp *explorer.Explorer

	doc  *document.Document
	hist *document.History
	in   *input.Provider
	cv   *canvas.Canvas
	cfg  settings.Settings

	opts         Options
	id           int64
	path         string
	winTitle     string
	results      chan func()
	toast        string
	toastUntil   time.Time
	lastAutosave time.Time
	saving       bool   // a background save is running
	loading      string // name of a notebook being opened in the background
	lastFrame    atomic.Int64
	copyMark     int64 // system clipboard change count when ink was last copied

	showPages, showSetup, showMenu, showStyle, showHelp bool
	allPages                                            bool
	focus                                               bool // hide everything but the page and dock
	home                                                bool // the Home page (all notebooks) is showing
	homeUI                                              homeState
	homeLogo                                            widget.Clickable
	pointerAt                                           image.Point          // last pointer position in the window
	hoverSeen                                           map[string]time.Time // corner -> last time the pointer was near
	tourUI                                              tourState

	// Colour picking.
	penPicker, paperPicker         *colorPicker
	showPenPicker, showPaperPicker bool
	recentColors                   []color.NRGBA
	recentClicks                   [10]widget.Clickable
	customPen, customPaper         widget.Clickable
	paperPresetBtns                [len(paperPresets)]widget.Clickable
	paperOld                       []document.PageFormat
	paperPages                     []*document.Page

	tools                                                            [9]widget.Clickable
	menuBtn, undoBtn, redoBtn, pagesBtn, setupBtn, helpBtn, styleBtn widget.Clickable
	backdrop                                                         widget.Clickable
	menuItems                                                        [11]widget.Clickable
	saveFolderBtn, revealFolderBtn                                   widget.Clickable
	selCrop, cropDone, cropCancel                                    widget.Clickable
	panelFont, fmtFont                                               fontDrop
	boldBtn, italicBtn, fmtBold, fmtItalic, fmtSmaller, fmtBigger    widget.Clickable
	fmtColors                                                        [6]widget.Clickable
	recent                                                           [8]widget.Clickable
	colors                                                           [16]widget.Clickable
	penStyles                                                        [4]widget.Clickable
	shapeKinds                                                       [11]widget.Clickable
	lineStyles                                                       [4]widget.Clickable
	fillNone, customFill                                             widget.Clickable
	fillColors                                                       [16]widget.Clickable
	fillOpacity                                                      widget.Float
	fillPicker                                                       *colorPicker
	showFillPicker                                                   bool
	eraseModes                                                       [2]widget.Clickable
	textSizeBtns                                                     [6]widget.Clickable
	penW, hiW, erW, sensF, smoothF                                   widget.Float
	taperB, snapB                                                    widget.Bool
	selDelete, selDup, selCopy, selCut                               widget.Clickable
	selColors                                                        [8]widget.Clickable
	selectAll                                                        widget.Clickable
	zoomIn, zoomOut, fit                                             widget.Clickable
	pagePrev, pageNext, addPage                                      widget.Clickable
	pageUp, pageDown, pageDup, pageDel                               widget.Clickable
	paperStyles                                                      [5]widget.Clickable
	paperSizes                                                       [5]widget.Clickable
	orient                                                           [2]widget.Clickable
	scope                                                            [2]widget.Clickable
	customApply                                                      widget.Clickable
	closePages, closeSetup, setDefault, resetDefault                 widget.Clickable
	bgImageBtn, bgRemoveBtn                                          widget.Clickable
	softenF                                                          widget.Float
	softenDragging                                                   bool
	customW, customH                                                 widget.Editor
	thumbs, setupList, styleList                                     widget.List
	thumbClicks                                                      []widget.Clickable
}

// Options connect a window to the rest of the app.
type Options struct {
	// NewWindow opens another dot! window (with path, or empty).
	NewWindow func(path string)
}

var windowSeq atomic.Int64

// Run drives one window until it closes. path may be empty. Each window
// has its own document, undo history and tools.
func Run(w *app.Window, path string, opts Options) error {
	th := material.NewTheme()
	th.Shaper = canvas.Shaper() // includes the text-tool fonts (Sans, Serif, Mono)
	th.Palette = material.Palette{Fg: cText, Bg: cSurface, ContrastBg: cAccent, ContrastFg: cWhite}

	hist := &document.History{Limit: 1000}
	in := input.NewProvider()
	cfg := settings.Load()
	doc := newFromDefaults(cfg)
	a := &App{
		w: w, th: th, exp: explorer.NewExplorer(w), doc: doc, hist: hist, in: in,
		cv:           canvas.New(doc, hist, in, th),
		cfg:          cfg,
		results:      make(chan func(), 8),
		lastAutosave: time.Now(),
		penPicker:    newColorPicker(),
		fillPicker:   newColorPicker(),
		paperPicker:  newColorPicker(),
	}
	a.thumbs.Axis, a.setupList.Axis, a.styleList.Axis = layout.Vertical, layout.Vertical, layout.Vertical
	for _, e := range []*widget.Editor{&a.customW, &a.customH} {
		e.SingleLine, e.Filter = true, "0123456789."
	}
	a.customW.SetText("210")
	a.customH.SetText("297")
	a.applySettings()
	a.opts, a.id = opts, windowSeq.Add(1)
	in.SetWake(w.Invalidate) // trackpad gestures arrive outside Gio's event loop
	a.cv.Wake = w.Invalidate // PDF pages finish rendering in the background

	if path != "" {
		// Show the window at once; big notebooks load in the background.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			a.path = path // a new notebook at this path
		} else {
			a.openAsync(path, nil)
		}
	} else {
		a.home = true // start on Home: all your notebooks
		a.scanNotes()
	}

	go a.releaseMemoryWhenIdle()
	var ops op.Ops
	for {
		ev := w.Event()
		a.exp.ListenEvents(ev)
		switch e := ev.(type) {
		case app.DestroyEvent:
			settings.Save(a.collectSettings())
			return e.Err
		case app.ViewEvent:
			a.in.Attach(e)
		case *app.ClosingEvent:
			// Never lose work: save synchronously on close.
			if a.doc.Dirty {
				a.saveNow(a.savePath())
			}
		case app.FrameEvent:
			a.lastFrame.Store(time.Now().UnixNano())
			t0 := time.Now()
			gtx := app.NewContext(&ops, e)
			a.drainResults()
			a.layout(gtx)
			t1 := time.Now()
			e.Frame(gtx.Ops)
			if os.Getenv("DOT_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "frame layout=%v gpu=%v\n", t1.Sub(t0).Round(time.Microsecond), time.Since(t1).Round(time.Microsecond))
			}
		}
	}
}

func (a *App) applySettings() {
	s, cv := a.cfg, a.cv
	nrgba := func(v [4]uint8) color.NRGBA { return color.NRGBA{v[0], v[1], v[2], v[3]} }
	cv.PenColor, cv.PenWidth, cv.PenStyle = nrgba(s.PenColor), s.PenWidth, ink.Style(s.PenStyle)
	cv.Ink = ink.Config{Sensitivity: s.Sensitivity, Smoothing: s.Smoothing, Taper: s.Taper}
	cv.SnapShapes = s.SnapShapes
	cv.HighlighterColor, cv.HighlighterWidth = nrgba(s.HighlighterColor), s.HighlighterWidth
	cv.EraserRadius, cv.EraserMode = s.EraserRadius, canvas.EraserMode(s.EraserMode)
	cv.ShapeKind, cv.TextSize = ink.Shape(s.ShapeKind), s.TextSize
	if s.TextFont != "" {
		cv.TextFont = s.TextFont
	}
	cv.TextBold, cv.TextItalic = s.TextBold, s.TextItalic
	cv.LineStyle, cv.FillColor = s.LineStyle, nrgba(s.FillColor)
	a.showPages = s.ShowPages
	a.recentColors = a.recentColors[:0]
	for _, c := range s.RecentColors {
		a.recentColors = append(a.recentColors, nrgba(c))
	}
}

func (a *App) collectSettings() settings.Settings {
	s, cv := a.cfg, a.cv
	arr := func(c color.NRGBA) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }
	s.PenColor, s.PenWidth, s.PenStyle = arr(cv.PenColor), cv.PenWidth, int(cv.PenStyle)
	s.Sensitivity, s.Smoothing, s.Taper, s.SnapShapes = cv.Ink.Sensitivity, cv.Ink.Smoothing, cv.Ink.Taper, cv.SnapShapes
	s.HighlighterColor, s.HighlighterWidth = arr(cv.HighlighterColor), cv.HighlighterWidth
	s.EraserRadius, s.EraserMode = cv.EraserRadius, int(cv.EraserMode)
	s.ShapeKind, s.TextSize, s.ShowPages = int(cv.ShapeKind), cv.TextSize, a.showPages
	s.TextFont, s.TextBold, s.TextItalic = cv.TextFont, cv.TextBold, cv.TextItalic
	s.LineStyle, s.FillColor = cv.LineStyle, arr(cv.FillColor)
	s.RecentColors = s.RecentColors[:0]
	for _, c := range a.recentColors {
		s.RecentColors = append(s.RecentColors, arr(c))
	}
	return s
}

func (a *App) notify(msg string) {
	a.toast, a.toastUntil = msg, time.Now().Add(2500*time.Millisecond)
}

func (a *App) setDocument(d *document.Document, path string) {
	a.doc, a.path = d, path
	a.hist = &document.History{Limit: 1000}
	a.cv.History = a.hist
	a.cv.SetDocument(d)
	// Match the ink to the paper, as on dark Xournal++ notebooks.
	dark := document.IsDark(d.Pages[0].Background.Color)
	if dark && document.IsDark(a.cv.PenColor) {
		a.cv.PenColor = penPalette[1]
	} else if !dark && !document.IsDark(a.cv.PenColor) {
		a.cv.PenColor = penPalette[0]
	}
	if path != "" {
		a.cfg.AddRecent(path)
		settings.Save(a.collectSettings())
	}
	a.lastAutosave = time.Now()
}

func (a *App) openPath(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		a.openPDF(path)
		return nil
	}
	d, err := document.OpenFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	a.setDocument(d, path)
	a.notify("Opened " + filepath.Base(path))
	return nil
}

// openAsync loads a notebook off the UI thread (large files take a
// moment to parse) and shows it when ready. The open notebook is saved
// in the background first.
func (a *App) openAsync(path string, then func()) {
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		a.openPDF(path)
		return
	}
	a.saveBehind()
	a.loading = filepath.Base(path)
	a.async(func() func() {
		d, err := document.OpenFile(path)
		return func() {
			a.loading = ""
			if err != nil {
				a.notify("Couldn't open " + filepath.Base(path) + ": " + err.Error())
				return
			}
			a.setDocument(d, path)
			a.home = false
			a.notify("Opened " + filepath.Base(path))
			if then != nil {
				then()
			}
		}
	})
}

// saveBehind writes the current notebook in the background if it has
// unsaved changes (before switching to another one).
func (a *App) saveBehind() {
	if !a.doc.Dirty || isEmpty(a.doc) {
		return
	}
	path, snap := a.savePath(), a.doc.Snapshot()
	a.cfg.AddRecent(path)
	go func() {
		if data, err := encodeDoc(snap, path); err == nil {
			os.MkdirAll(filepath.Dir(path), 0o755)
			writeAtomic(path, data)
		}
	}()
}

// releaseMemoryWhenIdle hands freed memory back to the system once the
// window has been idle for a while (after heavy work such as zooming out
// over a long notebook), instead of keeping it reserved.
func (a *App) releaseMemoryWhenIdle() {
	released := true
	for range time.Tick(5 * time.Second) {
		idle := time.Since(time.Unix(0, a.lastFrame.Load()))
		switch {
		case idle < 8*time.Second:
			released = false
		case !released:
			debug.FreeOSMemory()
			released = true
		}
	}
}

// async runs slow work (native dialogs, disk I/O) off the UI goroutine and
// applies its result on the next frame.
func (a *App) async(work func() func()) {
	go func() {
		a.results <- work()
		a.w.Invalidate()
	}()
}

func (a *App) drainResults() {
	for {
		select {
		case f := <-a.results:
			if f != nil {
				f()
			}
		default:
			return
		}
	}
}

func (a *App) updateTitle() {
	name := a.baseName()
	if a.home {
		name = "Home"
	}
	if t := name + " — dot!"; t != a.winTitle {
		a.winTitle = t
		a.w.Option(app.Title(t))
	}
}

// ---- file actions ----

func (a *App) newNotebook() {
	if a.doc.Dirty {
		a.saveNow(a.savePath())
	}
	a.setDocument(newFromDefaults(a.cfg), "")
	a.notify("New notebook")
}

func (a *App) doOpen() {
	a.async(func() func() {
		rc, err := a.exp.ChooseFile("dott", "ink", "xopp", "xoj", "pdf")
		if err != nil {
			return nil
		}
		defer rc.Close()
		if f, ok := rc.(interface{ Name() string }); ok {
			name := f.Name()
			if strings.EqualFold(filepath.Ext(name), ".pdf") {
				return func() { a.openPDF(name) }
			}
			d, err := document.OpenFile(name)
			if err != nil {
				return func() { a.notify("Couldn't open: " + err.Error()) }
			}
			return func() {
				if a.doc.Dirty {
					a.saveNow(a.savePath())
				}
				a.setDocument(d, name)
				a.home = false
				a.notify("Opened " + filepath.Base(name))
			}
		}
		d, err := document.Load(rc)
		if err != nil {
			return func() { a.notify("Couldn't open: " + err.Error()) }
		}
		path := ""
		if f, ok := rc.(interface{ Name() string }); ok {
			path = f.Name()
		}
		return func() {
			if a.doc.Dirty {
				a.saveNow(a.savePath())
			}
			d.ResolvePDFs(path)
			a.setDocument(d, path)
			a.notify("Opened " + filepath.Base(path))
		}
	})
}

// chooseBackgroundImage puts a picture behind the page(s). A copy is kept
// in the save folder (.backgrounds) so the notebook never loses it.
func (a *App) chooseBackgroundImage() {
	a.async(func() func() {
		rc, err := a.exp.ChooseFile("png", "jpg", "jpeg", "gif")
		if err != nil {
			return nil
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return func() { a.notify("Couldn't read image: " + err.Error()) }
		}
		name := "image.png"
		if f, ok := rc.(interface{ Name() string }); ok {
			name = filepath.Base(f.Name())
		}
		dir := filepath.Join(a.cfg.Dir(), ".backgrounds")
		os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, time.Now().Format("20060102-150405-")+name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return func() { a.notify("Couldn't keep image: " + err.Error()) }
		}
		if _, err := canvas.LoadBackgroundImage(path); err != nil {
			os.Remove(path)
			return func() { a.notify("That image format isn't supported (use PNG, JPEG or GIF)") }
		}
		return func() {
			a.applyFormat(func(f *document.PageFormat) {
				f.Background.Type, f.Background.ImagePath = "pixmap", path
				if f.Background.Soften == 0 {
					f.Background.Soften = 0.2
				}
			})
			a.notify("Background image set · use Soften to make writing easier to read")
		}
	})
}

// chooseSaveFolder lets the user pick where notebooks are saved.
func (a *App) chooseSaveFolder() {
	a.async(func() func() {
		dir, err := a.exp.ChooseFolder()
		if err != nil || dir == "" {
			return nil
		}
		return func() {
			a.cfg.SaveDir = dir
			settings.Save(a.collectSettings())
			a.notify("New notebooks save to " + shortPath(dir))
		}
	})
}

// revealSaveFolder opens the save folder in Finder / Explorer.
func (a *App) revealSaveFolder() {
	dir := a.cfg.Dir()
	os.MkdirAll(dir, 0o755)
	openExternal(dir)
}

// openExternal opens a folder or web link with the system's default app.
func openExternal(target string) {
	switch runtime.GOOS {
	case "windows":
		if strings.HasPrefix(target, "http") {
			exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
		} else {
			exec.Command("explorer", target).Start()
		}
	case "darwin":
		exec.Command("open", target).Start()
	default:
		exec.Command("xdg-open", target).Start()
	}
}

// shortPath shows a path with ~ for the home folder.
func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (a *App) newWindow(path string) {
	if a.opts.NewWindow != nil {
		a.opts.NewWindow(path)
	}
}

// choosePDF asks for a PDF to annotate.
func (a *App) choosePDF() {
	a.async(func() func() {
		rc, err := a.exp.ChooseFile("pdf")
		if err != nil {
			return nil
		}
		defer rc.Close()
		path := ""
		if f, ok := rc.(interface{ Name() string }); ok {
			path = f.Name()
		} else { // no file path (sandboxed picker): keep a copy to annotate
			data, err := io.ReadAll(rc)
			if err != nil {
				return func() { a.notify("Couldn't read PDF: " + err.Error()) }
			}
			path = filepath.Join(a.cfg.Dir(), "PDF "+time.Now().Format("2006-01-02 15.04.05")+".pdf")
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return func() { a.notify("Couldn't keep PDF: " + err.Error()) }
			}
		}
		return func() { a.openPDF(path) }
	})
}

// openPDF starts annotating a PDF. Annotations live in a notebook next to
// your other notebooks (in the save folder, <name>.dott or .xopp) that points at the
// PDF; opening the same PDF again resumes it.
func (a *App) openPDF(path string) {
	if a.doc.Dirty {
		a.saveNow(a.savePath())
	}
	stem := filepath.Join(a.cfg.Dir(), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	notes := stem + a.newExt()
	for _, ext := range []string{".dott", ".ink", ".xopp"} {
		d, err := document.OpenFile(stem + ext)
		if err != nil || len(d.Pages) == 0 || d.Pages[0].Background.Type != "pdf" {
			continue
		}
		// A .dott holds its own copy of the PDF; match by name.
		if p := d.Pages[0].Background.PDFPath; p == path || filepath.Base(p) == filepath.Base(path) || strings.HasSuffix(p, "-"+filepath.Base(path)) {
			a.setDocument(d, stem+ext)
			a.home = false
			a.notify("Continuing your notes on " + filepath.Base(path))
			return
		}
	}
	// Never overwrite an unrelated notebook that happens to share the name.
	for i := 2; ; i++ {
		if _, err := os.Stat(notes); err != nil {
			break
		}
		notes = fmt.Sprintf("%s %d%s", stem, i, a.newExt())
	}
	a.notify("Opening " + filepath.Base(path) + "…")
	a.async(func() func() {
		sizes, err := pdf.PageSizes(path)
		if err != nil {
			return func() { a.notify("Couldn't open PDF: " + err.Error()) }
		}
		return func() {
			a.setDocument(document.NewPDFDocument(path, sizes), notes)
			a.home = false
			a.doSave(false, true) // create the notebook in the save folder right away
			a.notify(fmt.Sprintf("%s · %d pages · write anywhere, then ⌘E to export", filepath.Base(path), len(sizes)))
		}
	})
}

// savePath is where Save writes: the open file, or an autosave location
// in the save folder for untitled notebooks.
func (a *App) savePath() string {
	if a.path != "" {
		return a.path
	}
	// Unique per window, so two untitled windows never share a file.
	return filepath.Join(a.cfg.Dir(), fmt.Sprintf("Notebook %s-%d%s", time.Now().Format("2006-01-02 15.04.05"), a.id, a.newExt()))
}

// encodeFor serialises the notebook in the format path's extension asks
// for: .dott (self-contained, with a preview) or .xopp (Xournal++).
func (a *App) encodeFor(path string) ([]byte, error) { return encodeDoc(a.doc, path) }

// encodeDoc serialises d; safe to run on a background goroutine with a
// Snapshot.
func encodeDoc(d *document.Document, path string) ([]byte, error) {
	var buf bytes.Buffer
	if !document.IsDott(path) {
		err := document.Save(&buf, d)
		return buf.Bytes(), err
	}
	var preview []byte
	if img, err := export.Thumbnail(d.Pages[0], 300, 420); err == nil {
		var pb bytes.Buffer
		if png.Encode(&pb, img) == nil {
			preview = pb.Bytes()
		}
	}
	err := document.SaveDott(&buf, d, preview)
	return buf.Bytes(), err
}

// newExt is the file type for new notebooks: ".dott" or ".xopp".
func (a *App) newExt() string {
	if a.cfg.NewFormat == "xopp" {
		return ".xopp"
	}
	return ".dott"
}

// saveNow saves synchronously (used on close and before switching files).
func (a *App) saveNow(path string) {
	data, err := a.encodeFor(path)
	if err == nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		err = writeAtomic(path, data)
	}
	a.saved(path, err, "")
}

func (a *App) doSave(as bool, quiet bool) {
	a.lastAutosave = time.Now()
	if !as {
		if a.saving {
			return // one save at a time; the next autosave catches up
		}
		path := a.savePath()
		snap := a.doc.Snapshot() // cheap; encoding happens off the UI thread
		a.saving = true
		msg := "Saved"
		if quiet {
			msg = ""
		}
		if a.path == "" {
			msg = "Saved to " + shortPath(a.cfg.Dir())
		}
		doc := a.doc
		a.async(func() func() {
			data, err := encodeDoc(snap, path)
			if err == nil {
				os.MkdirAll(filepath.Dir(path), 0o755)
				err = writeAtomic(path, data)
			}
			return func() {
				a.saving = false
				if a.doc != doc { // switched notebooks meanwhile
					return
				}
				a.savedUpTo(path, err, msg, snap.Edits)
			}
		})
		return
	}
	// Save as: prepare both formats; the chosen file name's extension
	// decides which one is written.
	ext := filepath.Ext(a.path)
	if ext == "" || strings.EqualFold(ext, ".ink") {
		ext = a.newExt()
	}
	inkData, err1 := a.encodeFor("x.dott")
	xoppData, err2 := a.encodeFor("x.xopp")
	if err1 != nil || err2 != nil {
		a.notify("Save failed")
		return
	}
	a.async(func() func() {
		wc, err := a.exp.CreateFile(a.baseName() + ext)
		if err != nil {
			return nil
		}
		data := xoppData
		if f, ok := wc.(interface{ Name() string }); ok && document.IsDott(f.Name()) || !ok && ext == ".dott" {
			data = inkData
		}
		_, err = io.Copy(wc, bytes.NewReader(data))
		if cerr := wc.Close(); err == nil {
			err = cerr
		}
		p := a.path
		if f, ok := wc.(interface{ Name() string }); ok {
			p = f.Name()
		}
		return func() { a.saved(p, err, "Saved") }
	})
}

func (a *App) saved(path string, err error, msg string) {
	a.savedUpTo(path, err, msg, a.doc.Edits)
}

// savedUpTo marks the notebook saved, unless it was edited after the
// snapshot that was written (then it stays dirty for the next autosave).
func (a *App) savedUpTo(path string, err error, msg string, edits uint64) {
	if err != nil {
		a.notify("Save failed: " + err.Error())
		return
	}
	a.path = path
	a.doc.Dirty = a.doc.Edits != edits
	a.cfg.AddRecent(path)
	if msg != "" {
		a.notify(msg + " · " + filepath.Base(path))
	}
}

func (a *App) baseName() string {
	if a.path == "" {
		return "Untitled"
	}
	return strings.TrimSuffix(filepath.Base(a.path), filepath.Ext(a.path))
}

func (a *App) exportPDF() {
	var buf bytes.Buffer
	if err := export.FinalPDF(&buf, a.doc); err != nil {
		a.notify("Export failed: " + err.Error())
		return
	}
	a.exportBytes(a.baseName()+" (annotated).pdf", buf.Bytes(), "Exported PDF")
}

func (a *App) exportPNG() {
	var buf bytes.Buffer
	i := a.cv.Current()
	if err := export.PNG(&buf, a.doc.Pages[i], 3); err != nil {
		a.notify("Export failed: " + err.Error())
		return
	}
	a.exportBytes(fmt.Sprintf("%s page %d.png", a.baseName(), i+1), buf.Bytes(), "Exported page as PNG")
}

func (a *App) exportBytes(name string, data []byte, msg string) {
	a.async(func() func() {
		wc, err := a.exp.CreateFile(name)
		if err != nil {
			return nil
		}
		_, err = wc.Write(data)
		if cerr := wc.Close(); err == nil {
			err = cerr
		}
		return func() {
			if err != nil {
				a.notify("Export failed: " + err.Error())
			} else {
				a.notify(msg)
			}
		}
	})
}

// writeAtomic writes via a temp file + rename so a crash never leaves a
// half-written notebook.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- page actions ----

func (a *App) scopePages() []*document.Page {
	if a.allPages {
		return a.doc.Pages
	}
	return []*document.Page{a.doc.Pages[a.cv.Current()]}
}

func (a *App) applyFormat(mut func(f *document.PageFormat)) {
	cmd := &document.SetFormat{}
	for _, p := range a.scopePages() {
		old := p.Format()
		nf := old
		mut(&nf)
		cmd.Pages, cmd.Old, cmd.New = append(cmd.Pages, p), append(cmd.Old, old), append(cmd.New, nf)
	}
	a.hist.Do(a.doc, cmd)
}

func (a *App) setSize(w, h float32) {
	a.applyFormat(func(f *document.PageFormat) {
		if f.W > f.H { // keep landscape pages landscape
			f.W, f.H = max(w, h), min(w, h)
		} else {
			f.W, f.H = min(w, h), max(w, h)
		}
	})
}

func (a *App) insertPage(p *document.Page) {
	i := a.cv.Current() + 1
	a.hist.Do(a.doc, &document.InsertPage{Index: i, Page: p})
	a.cv.ScrollToPage(i)
}

func (a *App) newPageLikeCurrent() *document.Page {
	cur := a.doc.Pages[a.cv.Current()]
	return document.NewPage(cur.Width, cur.Height, cur.Background.StyleOr(document.StylePlain), cur.Background.Color)
}

func (a *App) undo() {
	a.cv.ClearSelection()
	if !a.hist.Undo(a.doc) {
		a.notify("Nothing to undo")
	}
}

func (a *App) redo() {
	a.cv.ClearSelection()
	if !a.hist.Redo(a.doc) {
		a.notify("Nothing to redo")
	}
}

// ---- frame ----

func (a *App) layout(gtx C) {
	a.handleKeys(gtx)
	a.handleClicks(gtx)
	a.updateTitle()

	// Autosave: notebooks are never more than ~20 s behind.
	if a.doc.Dirty {
		if due := a.lastAutosave.Add(autosaveEvery); gtx.Now.After(due) {
			a.doSave(false, true)
		} else {
			gtx.Execute(op.InvalidateCmd{At: due})
		}
	}

	if a.home {
		a.homeClicks(gtx)
		a.handleClicks(gtx) // menu items, toasts
		a.updateTitle()
		a.homeView(gtx)
		if gtx.Now.Before(a.toastUntil) {
			at(gtx, layout.S, layout.Inset{Bottom: 18}, a.toastView)
			gtx.Execute(op.InvalidateCmd{At: a.toastUntil})
		}
		return
	}
	if a.loading != "" {
		paint.Fill(gtx.Ops, canvas.DeskColor)
		at(gtx, layout.Center, layout.Inset{}, func(gtx C) D {
			return a.text(gtx, "Opening "+a.loading+"…", cMuted, 15)
		})
		return
	}
	a.startTour()
	// The page gets the whole window; every control floats on top.
	drainBlocker(gtx)
	a.trackPointer(gtx)
	paint.Fill(gtx.Ops, canvas.DeskColor)
	a.canvasArea(gtx)
	// A pass-through layer over everything sees where the pointer is
	// (to reveal corner controls) without taking clicks from anything.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &a.pointerAt)
	area.Pop()
	pass.Pop()
	if a.showMenu {
		a.menu(gtx)
	}
	if a.showHelp {
		a.help(gtx)
	}
	if a.tourUI.on {
		a.tour(gtx)
	}
}

func (a *App) trackPointer(gtx C) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &a.pointerAt, Kinds: pointer.Move | pointer.Drag | pointer.Enter | pointer.Leave})
		if !ok {
			return
		}
		if e, ok := ev.(pointer.Event); ok {
			if e.Kind == pointer.Leave {
				a.pointerAt = image.Pt(-1, -1)
			} else {
				a.pointerAt = image.Pt(int(e.Position.X), int(e.Position.Y))
			}
		}
	}
}

// hoverNear reports whether the pointer is in r, or was within the last
// moment (so controls don't flicker as you move onto them).
func (a *App) hoverNear(gtx C, key string, r image.Rectangle) bool {
	if a.hoverSeen == nil {
		a.hoverSeen = map[string]time.Time{}
	}
	if a.pointerAt.In(r) {
		a.hoverSeen[key] = gtx.Now
		return true
	}
	const linger = 700 * time.Millisecond
	if until := a.hoverSeen[key].Add(linger); gtx.Now.Before(until) {
		gtx.Execute(op.InvalidateCmd{At: until})
		return true
	}
	return false
}

// addPageAtEnd adds a page after the last one (scrolling past the end).
// Only when the last page has something on it: no runs of blank pages.
func (a *App) addPageAtEnd() {
	last := a.doc.Pages[len(a.doc.Pages)-1]
	empty := true
	for _, l := range last.Layers {
		if len(l.Items) > 0 {
			empty = false
		}
	}
	if empty {
		return
	}
	p := document.NewPage(last.Width, last.Height, last.Background.StyleOr(document.StylePlain), last.Background.Color)
	if last.Background.Type == "pixmap" {
		p.Background = last.Background
	}
	i := len(a.doc.Pages)
	a.hist.Do(a.doc, &document.InsertPage{Index: i, Page: p})
	a.cv.ScrollToPage(i)
	a.notify(fmt.Sprintf("Page %d added", i+1))
}

// typing reports whether a text field has focus, so single-letter
// shortcuts must not fire.
func (a *App) typing(gtx C) bool {
	return a.cv.Editing() || gtx.Source.Focused(&a.customW) || gtx.Source.Focused(&a.customH) ||
		gtx.Source.Focused(&a.penPicker.hex) || gtx.Source.Focused(&a.paperPicker.hex) ||
		gtx.Source.Focused(&a.fillPicker.hex) || gtx.Source.Focused(&a.homeUI.search) ||
		gtx.Source.Focused(&a.homeUI.renameEd)
}

func (a *App) handleKeys(gtx C) {
	sc := key.ModShortcut
	filters := []event.Filter{
		key.Filter{Required: sc, Optional: key.ModShift, Name: "Z"},
		key.Filter{Required: sc, Name: "Y"},
		key.Filter{Required: sc, Optional: key.ModShift, Name: "S"},
		key.Filter{Required: sc, Name: "O"},
		key.Filter{Required: sc, Optional: key.ModShift, Name: "N"},
		key.Filter{Required: sc, Name: "E"},
		key.Filter{Required: sc, Name: "D"},
		key.Filter{Required: sc, Name: "F"},
		key.Filter{Required: sc, Optional: key.ModShift, Name: "="},
		key.Filter{Required: sc, Optional: key.ModShift, Name: "+"},
		key.Filter{Required: sc, Name: "-"},
		key.Filter{Required: sc, Name: "0"},
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: key.NamePageDown},
		key.Filter{Name: key.NamePageUp},
		key.Filter{Name: key.NameSpace},
		key.Filter{Optional: key.ModShift, Name: "/"},
		key.Filter{Name: "["},
		key.Filter{Name: "]"},
	}
	for _, n := range []key.Name{"V", "P", "H", "E", "S", "B", "T", "L", "G", "F"} {
		filters = append(filters, key.Filter{Name: n})
	}
	// Key events go to whoever asks first, and this runs before the text
	// fields lay out: leave editing keys to a focused field.
	if !a.typing(gtx) {
		filters = append(filters,
			key.Filter{Required: sc, Name: "A"},
			key.Filter{Required: sc, Name: "C"},
			key.Filter{Required: sc, Name: "X"},
			key.Filter{Required: sc, Name: "V"},
			key.Filter{Name: key.NameReturn}, // Return submits a field
			key.Filter{Name: key.NameEnter},
			key.Filter{Name: key.NameDeleteBackward},
			key.Filter{Name: key.NameDeleteForward},
		)
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if debugKeys {
			fmt.Fprintf(os.Stderr, "key %q mods=%v state=%v tool=%d\n", e.Name, e.Modifiers, e.State, a.cv.Tool)
		}
		if e.Name == key.NameSpace {
			a.cv.SpacePan = e.State == key.Press && !a.typing(gtx)
			continue
		}
		if e.State != key.Press {
			continue
		}
		cmd := e.Modifiers.Contain(sc)
		shift := e.Modifiers.Contain(key.ModShift)
		// The tour: Return for next, Esc to skip; other keys still work.
		if a.tourUI.on && !a.home && !cmd && !a.typing(gtx) {
			switch e.Name {
			case key.NameReturn, key.NameEnter:
				a.tourNext()
				continue
			case key.NameEscape:
				a.endTour()
				continue
			}
		}
		if cmd && e.Name == "F" {
			if !a.home {
				a.goHome()
			}
			a.homeUI.searchFocus = true
			continue
		}
		if a.home { // Home: only app-level shortcuts
			switch {
			case cmd && e.Name == "N" && shift:
				a.newWindow("")
			case cmd && e.Name == "N":
				a.homeUI.newBtn.Click()
			case cmd && e.Name == "O":
				a.doOpen()
			case e.Name == key.NameEscape && a.homeUI.confirmDelete != nil:
				a.homeUI.confirmDelete = nil
			case e.Name == key.NameEscape && a.homeUI.renaming != nil:
				a.homeUI.renaming = nil
			case e.Name == key.NameEscape && !(a.path == "" && isEmpty(a.doc)):
				a.leaveHome()
			}
			continue
		}
		if a.cv.Cropping() && (e.Name == key.NameEscape || e.Name == key.NameReturn || e.Name == key.NameEnter) {
			if e.Name == key.NameEscape {
				a.cv.CancelCrop()
			} else {
				a.cv.ApplyCrop()
			}
			continue
		}
		if e.Name == key.NameEscape && a.cv.Editing() {
			a.cv.FinishText()
			continue
		}
		if !cmd && a.typing(gtx) {
			continue
		}
		switch {
		case cmd && e.Name == "Z" && shift, cmd && e.Name == "Y":
			a.redo()
		case cmd && e.Name == "Z":
			a.undo()
		case cmd && e.Name == "S":
			a.doSave(shift, false)
		case cmd && e.Name == "O":
			a.doOpen()
		case cmd && e.Name == "N" && shift:
			a.newWindow("")
		case cmd && e.Name == "N":
			a.newNotebook()
		case cmd && e.Name == "E":
			a.exportPDF()
		case cmd && e.Name == "A":
			a.cv.SelectAll()
		case cmd && e.Name == "C":
			a.cv.Copy()
			a.copyMark = clipboard.ChangeCount()
		case cmd && e.Name == "X":
			a.cv.Cut()
			a.copyMark = clipboard.ChangeCount()
		case cmd && e.Name == "V":
			a.paste()
		case cmd && e.Name == "D":
			a.cv.DuplicateSelection()
		case cmd && (e.Name == "=" || e.Name == "+"):
			a.cv.ZoomBy(1.2)
		case cmd && e.Name == "-":
			a.cv.ZoomBy(1 / 1.2)
		case cmd && e.Name == "0":
			a.cv.FitWidth()
		case e.Name == key.NameEscape && a.focus:
			a.focus = false
		case e.Name == "F" && !cmd:
			a.focus = !a.focus
			if a.focus {
				a.notify("Focus mode · press F or Esc to exit")
			}
		case e.Name == key.NameEscape:
			if a.showMenu || a.showHelp || a.showStyle {
				a.showMenu, a.showHelp, a.showStyle = false, false, false
			} else {
				a.cv.ClearSelection()
			}
		case e.Name == key.NameDeleteBackward || e.Name == key.NameDeleteForward:
			a.cv.DeleteSelection()
		case e.Name == key.NamePageDown:
			a.cv.ScrollToPage(a.cv.Current() + 1)
		case e.Name == key.NamePageUp:
			a.cv.ScrollToPage(a.cv.Current() - 1)
		case e.Name == "/" && shift:
			a.showHelp = !a.showHelp
		case e.Name == "[":
			a.scaleWidth(1 / 1.25)
		case e.Name == "]":
			a.scaleWidth(1.25)
		case !cmd:
			if t, ok := map[key.Name]canvas.Tool{"V": canvas.Select, "P": canvas.Pen, "H": canvas.Highlighter,
				"E": canvas.Eraser, "S": canvas.Shape, "B": canvas.Fill, "T": canvas.Text, "L": canvas.Laser, "G": canvas.Hand}[e.Name]; ok {
				a.cv.SetTool(t)
				a.showStyle = t == canvas.Shape || t == canvas.Fill
			}
		}
	}
}

func (a *App) scaleWidth(f float32) {
	cv := a.cv
	switch cv.Tool {
	case canvas.Highlighter:
		cv.HighlighterWidth = clamp(cv.HighlighterWidth*f, 2, 40)
	case canvas.Eraser:
		cv.EraserRadius = clamp(cv.EraserRadius*f, 2, 50)
	default:
		cv.PenWidth = clamp(cv.PenWidth*f, 0.2, 12)
	}
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }

// logMap maps slider 0..1 to lo..hi on a log scale (fine control at the
// thin end, where it matters for handwriting).
func logMap(v, lo, hi float32) float32 {
	return lo * float32(math.Pow(float64(hi/lo), float64(v)))
}

func logUnmap(w, lo, hi float32) float32 {
	return float32(math.Log(float64(w/lo)) / math.Log(float64(hi/lo)))
}
