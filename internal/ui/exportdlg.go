package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/document"
	"dot/internal/export"
)

// exportState is the Export as PDF dialog: which pages, a few options,
// and the file size. The PDF is built in the background as the choices
// change (that's how the size is known) and reused when you export.
type exportState struct {
	on            bool
	modes         [3]widget.Clickable // all, current, range
	mode          int
	from, to      widget.Editor
	ruling, blank widget.Bool // keep lines & grid; skip blank pages
	cancel, save  widget.Clickable

	key  string // the choices data (or the build in flight) is for
	busy bool
	data []byte
	err  error
}

const (
	exportAll = iota
	exportCurrent
	exportRange
)

func (a *App) exportPDF() {
	s := &a.exportUI
	n := len(a.doc.Pages)
	if !s.on {
		s.mode = exportAll
		for _, e := range []*widget.Editor{&s.from, &s.to} {
			e.SingleLine, e.Submit, e.Filter = true, true, "0123456789"
		}
		s.ruling.Value, s.blank.Value = true, false
	}
	s.from.SetText("1")
	s.to.SetText(strconv.Itoa(n))
	s.on, s.key, s.data = true, "", nil
	a.showMenu, a.showHelp, a.showStyle = false, false, false
}

// exportPages is the 0-based pages the dialog has chosen, or why none.
func (a *App) exportPages() ([]int, string) {
	s := &a.exportUI
	n := len(a.doc.Pages)
	lo, hi := 1, n
	switch s.mode {
	case exportCurrent:
		lo = a.cv.Current() + 1
		hi = lo
	case exportRange:
		f, err1 := strconv.Atoi(strings.TrimSpace(s.from.Text()))
		t, err2 := strconv.Atoi(strings.TrimSpace(s.to.Text()))
		if err1 != nil || err2 != nil {
			return nil, "Enter the first and last page"
		}
		if f > t {
			f, t = t, f
		}
		if f < 1 || t > n {
			return nil, fmt.Sprintf("Pages go from 1 to %d", n)
		}
		lo, hi = f, t
	}
	var out []int
	for i := lo - 1; i < hi; i++ {
		if s.blank.Value && blankPage(a.doc.Pages[i]) {
			continue
		}
		out = append(out, i)
	}
	if len(out) == 0 {
		return nil, "Every chosen page is blank"
	}
	return out, ""
}

func blankPage(p *document.Page) bool {
	if p.Background.Type == "pdf" || p.Background.Type == "pixmap" {
		return false
	}
	for _, l := range p.Layers {
		if !l.Hidden && len(l.Items) > 0 {
			return false
		}
	}
	return true
}

// exportDoc is a copy of the chosen pages, safe to write in the background.
func (a *App) exportDoc(pages []int) *document.Document {
	snap := a.doc.Snapshot()
	d := &document.Document{}
	for _, i := range pages {
		p := snap.Pages[i]
		if !a.exportUI.ruling.Value && p.Background.Type == "solid" {
			p.Background.Style = document.StylePlain
		}
		d.Pages = append(d.Pages, p)
	}
	return d
}

// updateExport starts a background build when the choices changed and
// none is running (typing a range never queues up builds).
func (a *App) updateExport(pages []int) {
	s := &a.exportUI
	key := fmt.Sprint(pages, s.ruling.Value, a.doc.Edits)
	if pages == nil || s.busy || key == s.key {
		return
	}
	s.key, s.busy, s.data, s.err = key, true, nil, nil
	d := a.exportDoc(pages)
	a.async(func() func() {
		var buf bytes.Buffer
		err := export.FinalPDF(&buf, d)
		return func() {
			s.busy = false
			if s.key == key {
				s.data, s.err = buf.Bytes(), err
			}
		}
	})
}

func (a *App) saveExport() {
	s := &a.exportUI
	pages, msg := a.exportPages()
	if pages == nil {
		a.notify(msg)
		return
	}
	data := s.data
	if s.busy || s.key != fmt.Sprint(pages, s.ruling.Value, a.doc.Edits) || data == nil {
		var buf bytes.Buffer
		if err := export.FinalPDF(&buf, a.exportDoc(pages)); err != nil {
			a.notify("Export failed: " + err.Error())
			return
		}
		data = buf.Bytes()
	} else if s.err != nil {
		a.notify("Export failed: " + s.err.Error())
		return
	}
	name := a.baseName() + " (annotated).pdf"
	if len(pages) < len(a.doc.Pages) {
		if len(pages) == 1 {
			name = fmt.Sprintf("%s page %d.pdf", a.baseName(), pages[0]+1)
		} else {
			name = fmt.Sprintf("%s pages %d-%d.pdf", a.baseName(), pages[0]+1, pages[len(pages)-1]+1)
		}
	}
	s.on, s.data = false, nil
	a.exportBytes(name, data, fmt.Sprintf("Exported %d %s · %s", len(pages), plural(len(pages), "page"), humanSize(len(data))))
}

func (a *App) exportClicks(gtx C) {
	s := &a.exportUI
	for i := range s.modes {
		if s.modes[i].Clicked(gtx) {
			s.mode = i
			if i == exportRange {
				gtx.Execute(key.FocusCmd{Tag: &s.from})
			}
		}
	}
	for _, e := range []*widget.Editor{&s.from, &s.to} {
		for {
			ev, ok := e.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				a.saveExport() // Return in a page field exports
			}
		}
	}
	if s.cancel.Clicked(gtx) {
		s.on, s.data = false, nil
	}
	if s.save.Clicked(gtx) {
		a.saveExport()
	}
}

func (a *App) exportDialog(gtx C) {
	s := &a.exportUI
	a.exportClicks(gtx)
	if !s.on {
		return
	}
	n := len(a.doc.Pages)
	pages, problem := a.exportPages()
	a.updateExport(pages)

	paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 0x70}, clip.Rect{Max: gtx.Constraints.Max}.Op())
	block(gtx, image.Rectangle{Max: gtx.Constraints.Max}, 0)
	at(gtx, layout.Center, layout.Inset{}, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(32), gtx.Dp(420))
		gtx.Constraints.Min = image.Point{}
		return card(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					l := material.H6(a.th, "Export as PDF")
					l.Color = cText
					return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx C) D { return a.title(gtx, "PAGES") }),
				layout.Rigid(func(gtx C) D {
					return flow(gtx,
						func(gtx C) D {
							return a.chip(gtx, &s.modes[exportAll], fmt.Sprintf("All (%d)", n), s.mode == exportAll)
						},
						func(gtx C) D {
							return a.chip(gtx, &s.modes[exportCurrent], fmt.Sprintf("Current (page %d)", a.cv.Current()+1), s.mode == exportCurrent)
						},
						func(gtx C) D { return a.chip(gtx, &s.modes[exportRange], "Range", s.mode == exportRange) },
					)
				}),
				layout.Rigid(func(gtx C) D {
					if s.mode != exportRange {
						return D{}
					}
					return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return a.text(gtx, "From page", cText, 13) }),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(func(gtx C) D { return a.field(gtx, &s.from, "1") }),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(func(gtx C) D { return a.text(gtx, "to", cText, 13) }),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(func(gtx C) D { return a.field(gtx, &s.to, strconv.Itoa(n)) }),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Rigid(func(gtx C) D { return a.text(gtx, fmt.Sprintf("of %d", n), cMuted, 13) }),
						)
					})
				}),
				layout.Rigid(func(gtx C) D { return a.title(gtx, "OPTIONS") }),
				layout.Rigid(func(gtx C) D { return a.toggle(gtx, &s.ruling, "Page lines, grid & dots") }),
				layout.Rigid(func(gtx C) D { return a.toggle(gtx, &s.blank, "Skip blank pages") }),
				layout.Rigid(func(gtx C) D { return a.title(gtx, "DETAILS") }),
				layout.Rigid(func(gtx C) D {
					if pages == nil {
						return a.text(gtx, problem, cDanger, 13)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return a.detailRow(gtx, "Pages", a.pagesSummary(pages)) }),
						layout.Rigid(func(gtx C) D { return a.detailRow(gtx, "Paper", a.paperSummary(pages)) }),
						layout.Rigid(func(gtx C) D {
							size := "Calculating…"
							switch {
							case s.err != nil:
								size = "Can't export: " + s.err.Error()
							case s.data != nil:
								size = humanSize(len(s.data))
							}
							return a.detailRow(gtx, "File size", size)
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return a.chip(gtx, &s.cancel, "Cancel", false) }),
							layout.Rigid(func(gtx C) D { return a.chip(gtx, &s.save, "Export…", pages != nil) }),
						)
					})
				}),
			)
		})
	})
}

func (a *App) detailRow(gtx C, label, value string) D {
	return layout.Inset{Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(80)
				return a.text(gtx, label, cMuted, 13)
			}),
			layout.Flexed(1, func(gtx C) D { return a.text(gtx, value, cText, 13) }),
		)
	})
}

func (a *App) pagesSummary(pages []int) string {
	first, last := pages[0]+1, pages[len(pages)-1]+1
	s := fmt.Sprintf("%d %s", len(pages), plural(len(pages), "page"))
	switch {
	case first == last:
		s += fmt.Sprintf(" (page %d)", first)
	case len(pages) == last-first+1:
		s += fmt.Sprintf(" (%d–%d)", first, last)
	default:
		s += fmt.Sprintf(" (%d–%d, blank ones skipped)", first, last)
	}
	return s
}

// paperSummary names the page size ("A4 portrait"), or says they differ.
func (a *App) paperSummary(pages []int) string {
	p := a.doc.Pages[pages[0]]
	for _, i := range pages[1:] {
		if q := a.doc.Pages[i]; q.Width != p.Width || q.Height != p.Height {
			return "Mixed sizes"
		}
	}
	w, h := min(p.Width, p.Height), max(p.Width, p.Height)
	orient := "portrait"
	if p.Width > p.Height {
		orient = "landscape"
	}
	for _, ps := range document.PaperSizes {
		if math.Abs(float64(ps.W-w)) < 1 && math.Abs(float64(ps.H-h)) < 1 {
			return ps.Name + " " + orient
		}
	}
	const mm = 25.4 / 72
	return fmt.Sprintf("%.0f × %.0f mm", p.Width*mm, p.Height*mm)
}

func humanSize(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.0f KB", float64(n)/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
