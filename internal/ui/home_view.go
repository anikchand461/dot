package ui

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/document"
)

// A calm, flat palette for Home.
var (
	homeBG      = color.NRGBA{0x14, 0x15, 0x19, 0xff}
	cardBG      = color.NRGBA{0x1c, 0x1d, 0x23, 0xff}
	cardHover   = color.NRGBA{0x21, 0x22, 0x29, 0xff}
	cardLine    = color.NRGBA{0x2a, 0x2b, 0x33, 0xff}
	cardLineHi  = color.NRGBA{0x4a, 0x4b, 0x58, 0xff}
	previewWell = color.NRGBA{0x17, 0x18, 0x1d, 0xff}
	danger      = color.NRGBA{0xe5, 0x48, 0x4d, 0xff}
)

func (a *App) homeView(gtx C) D {
	h := &a.homeUI
	h.list.Axis = layout.Vertical
	h.search.SingleLine = true
	h.renameEd.SingleLine, h.renameEd.Submit = true, true
	size := gtx.Constraints.Max
	paint.Fill(gtx.Ops, homeBG)

	query := strings.ToLower(strings.TrimSpace(h.search.Text()))
	var shown []*noteInfo
	for _, n := range h.notes {
		if query == "" || strings.Contains(strings.ToLower(n.name), query) {
			shown = append(shown, n)
		}
	}
	if h.byName {
		sort.SliceStable(shown, func(i, j int) bool { return strings.ToLower(shown[i].name) < strings.ToLower(shown[j].name) })
	}

	padDp := unit.Dp(40)
	pad := gtx.Dp(padDp)
	minW, gap := gtx.Dp(190), gtx.Dp(20)
	avail := size.X - 2*pad - gtx.Dp(16)
	cols := max(1, (avail+gap)/(minW+gap))
	cardW := min((avail-(cols-1)*gap)/cols, gtx.Dp(260))
	rows := (len(shown) + cols - 1) / cols

	head := []layout.Widget{a.homeHeader, a.homeToolbar}
	total := len(head) + max(rows, 1) + 1
	d := material.List(a.th, &h.list).Layout(gtx, total, func(gtx C, i int) D {
		in := layout.Inset{Left: padDp, Right: padDp}
		if i < len(head) {
			return in.Layout(gtx, head[i])
		}
		r := i - len(head)
		if r == max(rows, 1) {
			return D{Size: image.Pt(0, gtx.Dp(48))}
		}
		if len(shown) == 0 {
			return in.Layout(gtx, func(gtx C) D { return a.homeEmpty(gtx, query) })
		}
		return in.Layout(gtx, func(gtx C) D {
			return layout.Inset{Bottom: 22}.Layout(gtx, func(gtx C) D {
				var ch []layout.FlexChild
				for c := 0; c < cols; c++ {
					k := r*cols + c
					if k >= len(shown) {
						break
					}
					if c > 0 {
						ch = append(ch, layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gap, 0)} }))
					}
					n := shown[k]
					ch = append(ch, layout.Rigid(func(gtx C) D { return a.noteCard(gtx, n, cardW) }))
				}
				return layout.Flex{}.Layout(gtx, ch...)
			})
		})
	})
	if h.confirmDelete != nil {
		a.deleteDialog(gtx)
	}
	return d
}

func (a *App) homeHeader(gtx C) D {
	h := &a.homeUI
	return layout.Inset{Top: 30, Bottom: 22}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return logoMark(gtx, gtx.Dp(38)) })
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						l := material.H5(a.th, "dot")
						l.Font.Weight, l.Color = font.Bold, cText
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						l := material.H5(a.th, "!")
						l.Font.Weight, l.Color = font.Bold, cAccent
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(func(gtx C) D {
				if a.path == "" && isEmpty(a.doc) {
					return D{}
				}
				return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D {
					return a.flatButton(gtx, &h.backBtn, gUndo, "Back to "+a.baseName(), false)
				})
			}),
			layout.Rigid(a.searchBox),
		)
	})
}

// homeToolbar: create/open actions on the left, folder and sorting on the right.
func (a *App) homeToolbar(gtx C) D {
	h := &a.homeUI
	space := func(dp unit.Dp) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(dp), 0)} })
	}
	return layout.Inset{Bottom: 24}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.flatButton(gtx, &h.newBtn, gAdd, "New notebook", true) }),
			space(8),
			layout.Rigid(a.formatSwitch),
			space(16),
			layout.Rigid(func(gtx C) D { return a.flatButton(gtx, &h.pdfBtn, gPDF, "Annotate PDF", false) }),
			space(8),
			layout.Rigid(func(gtx C) D {
				return a.flatButton(gtx, &h.openBtn, iconGlyph(iconsFolder), "Open…", false)
			}),
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(func(gtx C) D {
				return h.folderBtn.Layout(gtx, func(gtx C) D {
					col := cMuted
					if h.folderBtn.Hovered() {
						col = cText
					}
					l := material.Body2(a.th, fmt.Sprintf("%d notebooks · %s", len(h.notes), filepath.Base(a.cfg.Dir())))
					l.Color, l.MaxLines, l.TextSize = col, 1, unit.Sp(13)
					if h.folderBtn.Hovered() {
						a.tooltip(gtx, shortPath(a.cfg.Dir())+" · click to change", image.Pt(gtx.Dp(60), gtx.Dp(24)))
					}
					return l.Layout(gtx)
				})
			}),
			space(14),
			layout.Rigid(func(gtx C) D {
				return segmented(gtx, a, []segment{{&h.sortRecent, "Recent", !h.byName}, {&h.sortName, "A–Z", h.byName}})
			}),
		)
	})
}

type segment struct {
	c     *widget.Clickable
	label string
	on    bool
}

// segmented is a small two-or-more-way switch.
func segmented(gtx C, a *App, segs []segment) D {
	m := op.Record(gtx.Ops)
	dims := layout.UniformInset(3).Layout(gtx, func(gtx C) D {
		var ch []layout.FlexChild
		for _, s := range segs {
			s := s
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return s.c.Layout(gtx, func(gtx C) D {
					m := op.Record(gtx.Ops)
					dims := layout.Inset{Top: 5, Bottom: 5, Left: 11, Right: 11}.Layout(gtx, func(gtx C) D {
						col := cMuted
						if s.on {
							col = cText
						}
						return a.text(gtx, s.label, col, 13)
					})
					call := m.Stop()
					if s.on {
						paint.FillShape(gtx.Ops, cBorder, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(6)).Op(gtx.Ops))
					}
					call.Add(gtx.Ops)
					return dims
				})
			}))
		}
		return layout.Flex{}.Layout(gtx, ch...)
	})
	call := m.Stop()
	paint.FillShape(gtx.Ops, cardBG, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(8)).Op(gtx.Ops))
	call.Add(gtx.Ops)
	return dims
}

// formatSwitch picks the file type for new notebooks.
func (a *App) formatSwitch(gtx C) D {
	h := &a.homeUI
	dott := a.cfg.NewFormat != "xopp"
	return segmented(gtx, a, []segment{{&h.fmtInk, ".dott", dott}, {&h.fmtXopp, ".xopp", !dott}})
}

func (a *App) homeEmpty(gtx C, query string) D {
	msg, sub := "No notebooks yet", "Click New notebook to start writing."
	if query != "" {
		msg, sub = "Nothing matches “"+query+"”", "Try a different name."
	}
	return layout.Inset{Top: 60}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.text(gtx, msg, cText, 16) }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D { return a.text(gtx, sub, cMuted, 13) })
			}),
		)
	})
}

func greeting() string {
	switch h := time.Now().Hour(); {
	case h < 5:
		return "Working late"
	case h < 12:
		return "Good morning"
	case h < 17:
		return "Good afternoon"
	}
	return "Good evening"
}

func (a *App) searchBox(gtx C) D {
	w := gtx.Dp(260)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 9, Bottom: 9, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(16)
				gSearch(gtx, sz, cMuted)
				return D{Size: image.Pt(sz, sz)}
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D {
					ed := material.Editor(a.th, &a.homeUI.search, "Search")
					ed.Color, ed.HintColor, ed.TextSize = cText, cMuted, unit.Sp(14)
					return ed.Layout(gtx)
				})
			}),
		)
	})
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	paint.FillShape(gtx.Ops, cardBG, clip.UniformRRect(r, gtx.Dp(8)).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cardLine, clip.Stroke{Path: clip.UniformRRect(r, gtx.Dp(8)).Path(gtx.Ops), Width: 1}.Op())
	call.Add(gtx.Ops)
	return dims
}

// flatButton: solid accent for the main action, quiet outline otherwise.
func (a *App) flatButton(gtx C, c *widget.Clickable, g glyph, label string, primary bool) D {
	return c.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 14}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					sz := gtx.Dp(16)
					col := cMuted
					if primary {
						col = cWhite
					}
					g(gtx, sz, col)
					return D{Size: image.Pt(sz, sz)}
				}),
				layout.Rigid(func(gtx C) D {
					col := cText
					if primary {
						col = cWhite
					}
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return a.text(gtx, label, col, 13.5) })
				}),
			)
		})
		call := m.Stop()
		r := image.Rectangle{Max: dims.Size}
		rad := gtx.Dp(8)
		if primary {
			bg := cAccent
			if c.Hovered() {
				bg = color.NRGBA{0x9b, 0x80, 0xff, 0xff}
			}
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, rad).Op(gtx.Ops))
		} else {
			bg := cardBG
			if c.Hovered() {
				bg = cardHover
			}
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, rad).Op(gtx.Ops))
			paint.FillShape(gtx.Ops, cardLine, clip.Stroke{Path: clip.UniformRRect(r, rad).Path(gtx.Ops), Width: 1}.Op())
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// homeButton is kept for other callers; same look as flatButton.
func (a *App) homeButton(gtx C, c *widget.Clickable, g glyph, label string, accent bool) D {
	return a.flatButton(gtx, c, g, label, accent)
}

// thumbHeight keeps cards roughly page-shaped.
func thumbHeight(w int) int { return w * 5 / 4 }

func (a *App) noteCard(gtx C, n *noteInfo, w int) D {
	h := &a.homeUI
	th := thumbHeight(w)
	total := th + gtx.Dp(58)
	renaming := h.renaming == n
	hover := n.click.Hovered() || n.renameBtn.Hovered() || n.deleteBtn.Hovered()

	body := func(gtx C) D {
		r := image.Rect(0, 0, w, total)
		rad := gtx.Dp(12)
		bg, line := cardBG, cardLine
		if hover || renaming {
			bg, line = cardHover, cardLineHi
		}
		if n.path == a.path {
			line = cAccent
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, rad).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, line, clip.Stroke{Path: clip.UniformRRect(r, rad).Path(gtx.Ops), Width: 1}.Op())

		well := image.Rect(gtx.Dp(8), gtx.Dp(8), w-gtx.Dp(8), th)
		paint.FillShape(gtx.Ops, previewWell, clip.UniformRRect(well, gtx.Dp(8)).Op(gtx.Ops))
		inner := well.Inset(gtx.Dp(12))
		if n.thumbSize.X > 0 {
			s := min(float32(inner.Dx())/float32(n.thumbSize.X), float32(inner.Dy())/float32(n.thumbSize.Y))
			pw, ph := int(float32(n.thumbSize.X)*s), int(float32(n.thumbSize.Y)*s)
			pos := image.Pt(inner.Min.X+(inner.Dx()-pw)/2, inner.Min.Y+(inner.Dy()-ph)/2)
			pr := image.Rectangle{Min: pos, Max: pos.Add(image.Pt(pw, ph))}
			paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 0x50}, clip.UniformRRect(pr.Add(image.Pt(0, gtx.Dp(2))), gtx.Dp(2)).Op(gtx.Ops))
			cl := clip.UniformRRect(pr, gtx.Dp(2)).Push(gtx.Ops)
			t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(float32(pos.X), float32(pos.Y)))).Push(gtx.Ops)
			n.thumb.Filter = paint.FilterLinear
			n.thumb.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			t.Pop()
			cl.Pop()
		} else {
			pw := inner.Dx() * 3 / 4
			ph := min(pw*141/100, inner.Dy())
			pos := image.Pt(inner.Min.X+(inner.Dx()-pw)/2, inner.Min.Y+(inner.Dy()-ph)/2)
			paint.FillShape(gtx.Ops, cardLine, clip.UniformRRect(image.Rectangle{Min: pos, Max: pos.Add(image.Pt(pw, ph))}, gtx.Dp(2)).Op(gtx.Ops))
		}
		if renaming {
			return D{Size: r.Max} // the name field is laid out on top
		}
		t := op.Offset(image.Pt(gtx.Dp(12), th+gtx.Dp(10))).Push(gtx.Ops)
		tg := gtx
		tg.Constraints.Min = image.Point{}
		tg.Constraints.Max.X = w - gtx.Dp(24)
		layout.Flex{Axis: layout.Vertical}.Layout(tg,
			layout.Rigid(func(gtx C) D {
				l := material.Body2(a.th, n.name)
				l.Font.Weight, l.Color, l.MaxLines, l.TextSize = font.SemiBold, cText, 1, unit.Sp(14)
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 3}.Layout(gtx, func(gtx C) D { return a.text(gtx, noteMeta(n), cMuted, 12) })
			}),
		)
		t.Pop()
		return D{Size: r.Max}
	}

	var dims D
	if renaming {
		dims = body(gtx)
		a.renameField(gtx, w, th)
	} else {
		dims = n.click.Layout(gtx, body)
	}
	// Rename / delete appear on hover, laid out as siblings above the card
	// so their clicks don't open the notebook.
	if hover && !renaming {
		x := w - gtx.Dp(8) - 2*gtx.Dp(30) - gtx.Dp(6)
		t := op.Offset(image.Pt(x, gtx.Dp(14))).Push(gtx.Ops)
		a.cardAction(gtx, &n.renameBtn, gRename, "Rename", false)
		t.Pop()
		t = op.Offset(image.Pt(x+gtx.Dp(36), gtx.Dp(14))).Push(gtx.Ops)
		a.cardAction(gtx, &n.deleteBtn, gDelete, "Move to Trash", true)
		t.Pop()
	}
	return dims
}

func noteMeta(n *noteInfo) string {
	kind := "Xournal++"
	switch {
	case n.pdf:
		kind = "PDF notes"
	case document.IsDott(n.path):
		kind = "dot!"
	}
	meta := kind + " · " + ago(n.mod)
	if n.pages > 1 {
		meta += fmt.Sprintf(" · %d pages", n.pages)
	}
	return meta
}

// cardAction is a small round icon button shown over a card.
func (a *App) cardAction(gtx C, c *widget.Clickable, g glyph, tip string, destructive bool) D {
	return c.Layout(gtx, func(gtx C) D {
		sz := gtx.Dp(30)
		r := image.Rectangle{Max: image.Pt(sz, sz)}
		bg := color.NRGBA{0x0d, 0x0e, 0x12, 0xd8}
		col := cText
		if c.Hovered() {
			bg = cBorder
			if destructive {
				bg, col = danger, cWhite
			}
		}
		paint.FillShape(gtx.Ops, bg, clip.Ellipse(r).Op(gtx.Ops))
		isz := gtx.Dp(16)
		t := op.Offset(image.Pt((sz-isz)/2, (sz-isz)/2)).Push(gtx.Ops)
		g(gtx, isz, col)
		t.Pop()
		if c.Hovered() {
			a.tooltip(gtx, tip, image.Pt(sz/2, sz+gtx.Dp(4)))
		}
		return D{Size: r.Max}
	})
}

// renameField edits a card's name in place. Enter saves, Esc cancels.
func (a *App) renameField(gtx C, w, th int) {
	h := &a.homeUI
	t := op.Offset(image.Pt(gtx.Dp(8), th+gtx.Dp(8))).Push(gtx.Ops)
	defer t.Pop()
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = w - gtx.Dp(16)
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 7, Bottom: 7, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ed := material.Editor(a.th, &h.renameEd, "Name")
		ed.Color, ed.HintColor, ed.TextSize = cText, cMuted, unit.Sp(14)
		ed.SelectionColor = color.NRGBA{cAccent.R, cAccent.G, cAccent.B, 0x70}
		return ed.Layout(gtx)
	})
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	paint.FillShape(gtx.Ops, previewWell, clip.UniformRRect(r, gtx.Dp(6)).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cAccent, clip.Stroke{Path: clip.UniformRRect(r, gtx.Dp(6)).Path(gtx.Ops), Width: 1.5}.Op())
	call.Add(gtx.Ops)
	if h.renameFocus {
		gtx.Execute(key.FocusCmd{Tag: &h.renameEd})
		h.renameFocus = false
	}
}

// deleteDialog asks before moving a notebook to the Trash.
func (a *App) deleteDialog(gtx C) {
	h := &a.homeUI
	n := h.confirmDelete
	h.dimmer.Layout(gtx, func(gtx C) D {
		paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 0x90}, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return D{Size: gtx.Constraints.Max}
	})
	at(gtx, layout.Center, layout.Inset{}, func(gtx C) D {
		gtx.Constraints.Max.X = gtx.Dp(400)
		return card(gtx, func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 2, Left: 6, Right: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						l := material.Body1(a.th, "Move “"+n.name+"” to the Trash?")
						l.Font.Weight, l.Color = font.SemiBold, cText
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 6, Bottom: 18}.Layout(gtx, func(gtx C) D {
							return a.text(gtx, "You can put it back from the Trash later.", cMuted, 13)
						})
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
							layout.Rigid(func(gtx C) D { return a.flatButton(gtx, &h.confirmNo, gClose, "Cancel", false) }),
							layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(8), 0)} }),
							layout.Rigid(func(gtx C) D { return a.dangerButton(gtx, &h.confirmYes, "Move to Trash") }),
						)
					}),
				)
			})
		})
	})
}

func (a *App) dangerButton(gtx C, c *widget.Clickable, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Top: 8, Bottom: 8, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
			return a.text(gtx, label, cWhite, 13.5)
		})
		call := m.Stop()
		bg := danger
		if c.Hovered() {
			bg = color.NRGBA{0xf0, 0x5a, 0x5f, 0xff}
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(8)).Op(gtx.Ops))
		call.Add(gtx.Ops)
		return dims
	})
}
