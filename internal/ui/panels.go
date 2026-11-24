package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strconv"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/ink"
)

// at places w inside the current area, aligned by dir, inset by in.
func at(gtx C, dir layout.Direction, in layout.Inset, w layout.Widget) {
	gtx.Constraints.Min = gtx.Constraints.Max
	in.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return dir.Layout(gtx, w)
	})
}

func fill(gtx C, col color.NRGBA) D {
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return D{Size: gtx.Constraints.Max}
}

// selPalette is the selection bar's quick colours: recent ones first.
func (a *App) selPalette() []color.NRGBA {
	p := penPalette
	out := []color.NRGBA{}
	seen := map[color.NRGBA]bool{}
	for _, c := range append(append([]color.NRGBA{}, a.recentColors...), p[0], p[1], p[3], p[4], p[6], p[8], p[9], p[12]) {
		if !seen[c] && len(out) < len(a.selColors) {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// colorSection: palette, recent colours, and a custom colour wheel.
func (a *App) colorSection(gtx C, pal []color.NRGBA, cur color.NRGBA) D {
	cur.A = 0xff
	tool := a.cv.Tool
	if tool != canvas.Select {
		a.penPicker.Sync(cur)
	}
	var sw []layout.Widget
	for i, c := range pal {
		i, c := i, c
		cc := c
		cc.A = 0xff
		sw = append(sw, func(gtx C) D { return a.swatch(gtx, &a.colors[i], c, cc == cur) })
	}
	sw = append(sw, func(gtx C) D { return a.wheelButton(gtx, &a.customPen, a.showPenPicker) })
	ch := []layout.FlexChild{layout.Rigid(func(gtx C) D { return flow(gtx, sw...) })}
	if len(a.recentColors) > 0 {
		ch = append(ch,
			layout.Rigid(func(gtx C) D { return a.title(gtx, "RECENT") }),
			layout.Rigid(func(gtx C) D {
				var rs []layout.Widget
				for i, c := range a.recentColors {
					i, c := i, c
					rs = append(rs, func(gtx C) D { return a.swatch(gtx, &a.recentClicks[i], c, c == cur) })
				}
				return flow(gtx, rs...)
			}))
	}
	if a.showPenPicker {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				d := a.penPicker.Layout(gtx, a.th, false)
				if a.penPicker.Changed || a.penPicker.Committed {
					a.applyToolColor(a.penPicker.Color(), a.penPicker.Committed)
				}
				return d
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

var (
	miniWheel     paint.ImageOp
	miniWheelSize int
)

// wheelButton is a small colour wheel that opens the custom picker.
func (a *App) wheelButton(gtx C, c *widget.Clickable, open bool) D {
	return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			d := gtx.Dp(26)
			r := image.Rectangle{Max: image.Pt(d, d)}
			if open {
				paint.FillShape(gtx.Ops, cAccent, clip.Ellipse(r).Op(gtx.Ops))
				paint.FillShape(gtx.Ops, cSurface, clip.Ellipse(r.Inset(gtx.Dp(2))).Op(gtx.Ops))
			}
			in := gtx.Dp(4)
			sz := d - 2*in
			if miniWheelSize != sz {
				miniWheel, miniWheelSize = paint.NewImageOp(wheelImage(sz, 1)), sz
			}
			t := op.Offset(image.Pt(in, in)).Push(gtx.Ops)
			miniWheel.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			t.Pop()
			if c.Hovered() {
				a.tooltip(gtx, "Custom colour", image.Pt(d/2, d+gtx.Dp(4)))
			}
			return D{Size: r.Max}
		})
	})
}

// ---- top controls (floating) ----

func (a *App) titlePill(gtx C) D {
	return pill(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.menuBtn, gMenu, a.showMenu, "Menu") }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8, Right: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							l := material.Body1(a.th, "dot")
							l.Font.Weight, l.Color = font.Bold, cText
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							l := material.Body1(a.th, "!")
							l.Font.Weight, l.Color = font.Bold, cAccent
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D {
								gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(220))
								l := material.Body2(a.th, a.baseName())
								l.Color, l.MaxLines = cText, 1
								return l.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx C) D {
							status := "Saved"
							if a.doc.Dirty {
								status = "Edited"
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return a.text(gtx, status, cMuted, 12) })
						}),
					)
				})
			}),
		)
	})
}

func (a *App) actionsPill(gtx C) D {
	return pill(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.undoBtn, gUndo, false, "Undo  ⌘Z") }),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.redoBtn, gRedo, false, "Redo  Shift ⌘Z") }),
			layout.Rigid(vsep),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.pagesBtn, gPages, a.showPages, "Pages") }),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.setupBtn, gSetup, a.showSetup, "Page setup") }),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.helpBtn, gHelp, a.showHelp, "Shortcuts  ?") }),
		)
	})
}

// ---- canvas area with floating controls ----

func (a *App) canvasArea(gtx C) D {
	size := gtx.Constraints.Max
	a.cv.Layout(gtx)

	if a.showStyle {
		a.backdrop.Layout(gtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}
	const edge, top = 10, 64 // panels start below the floating top row
	// Corners show their controls only while the pointer is near them (or
	// a panel they control is open), keeping the page uncluttered.
	topRight := a.hoverNear(gtx, "tr", image.Rect(size.X-gtx.Dp(400), 0, size.X, gtx.Dp(110))) ||
		a.showPages || a.showSetup || a.showHelp
	bottomLeft := a.hoverNear(gtx, "bl", image.Rect(0, size.Y-gtx.Dp(110), gtx.Dp(380), size.Y)) || a.showMenu
	if !a.focus {
		if topRight {
			at(gtx, layout.NE, layout.UniformInset(edge), a.actionsPill)
		}
		if a.showPages {
			at(gtx, layout.W, layout.Inset{Left: edge, Top: top, Bottom: 70}, a.pagesPanel)
		}
		if a.showSetup {
			at(gtx, layout.E, layout.Inset{Right: edge, Top: top, Bottom: 70}, a.setupPanel)
		}
	}
	at(gtx, layout.N, layout.Inset{Top: edge}, a.dock)
	if a.showStyle {
		at(gtx, layout.N, layout.Inset{Top: top}, a.stylePanel)
	}
	a.selectionBar(gtx)
	if !a.focus {
		at(gtx, layout.SW, layout.UniformInset(edge), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(a.menuPill),
				layout.Rigid(func(gtx C) D {
					if !bottomLeft {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, a.pagePill)
				}),
			)
		})
		at(gtx, layout.SE, layout.UniformInset(edge), a.zoomPill)
	}
	if gtx.Now.Before(a.toastUntil) {
		at(gtx, layout.S, layout.Inset{Bottom: 18}, a.toastView)
		gtx.Execute(op.InvalidateCmd{At: a.toastUntil})
	}
	return D{Size: size}
}

func (a *App) dock(gtx C) D {
	tips := []string{"Select (lasso)  V", "Pen  P", "Highlighter  H", "Eraser  E", "Shapes (square, circle, star…)  S", "Fill  B", "Text  T", "Laser pointer  L", "Pan  G · hold Space"}
	glyphs := []glyph{gSelect, gPen, gHighlight, gEraser, gShape, gFill, gText, gLaser, gHand}
	return pill(gtx, func(gtx C) D {
		var children []layout.FlexChild
		for i := range toolOrder {
			i := i
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 2, Right: 2}.Layout(gtx, func(gtx C) D {
					return a.iconBtn(gtx, &a.tools[i], glyphs[i], a.cv.Tool == toolOrder[i], tips[i])
				})
			}))
		}
		children = append(children, layout.Rigid(vsep), layout.Rigid(a.styleButton))
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// styleButton previews the current colour and thickness.
func (a *App) styleButton(gtx C) D {
	cv := a.cv
	col, w := cv.PenColor, cv.PenWidth
	switch cv.Tool {
	case canvas.Highlighter:
		col, w = cv.HighlighterColor, cv.HighlighterWidth/4
	case canvas.Eraser:
		col, w = cMuted, cv.EraserRadius/3
	}
	col.A = 0xff
	return a.styleBtn.Layout(gtx, func(gtx C) D {
		sz := gtx.Dp(36)
		r := image.Rectangle{Max: image.Pt(sz, sz)}
		bg := cSurface2
		if a.showStyle {
			bg = cBorder
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, gtx.Dp(9)).Op(gtx.Ops))
		d := int(clamp(float32(gtx.Dp(6))+w*float32(gtx.Dp(2.2)), float32(gtx.Dp(6)), float32(gtx.Dp(22))))
		c := image.Rect((sz-d)/2, (sz-d)/2, (sz+d)/2, (sz+d)/2)
		paint.FillShape(gtx.Ops, cBorder, clip.Ellipse(c.Inset(-1)).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, col, clip.Ellipse(c).Op(gtx.Ops))
		if a.styleBtn.Hovered() {
			a.tooltip(gtx, "Colour & thickness", image.Pt(sz/2, sz+gtx.Dp(6)))
		}
		return D{Size: r.Max}
	})
}

func (a *App) stylePanel(gtx C) D {
	gtx.Constraints.Max.X = gtx.Dp(316)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Max.Y -= gtx.Dp(60)
	cv := a.cv
	var ws []layout.Widget
	add := func(w ...layout.Widget) { ws = append(ws, w...) }
	colors := func(pal []color.NRGBA, cur color.NRGBA) layout.Widget {
		return func(gtx C) D { return a.colorSection(gtx, pal, cur) }
	}
	title := func(s string) layout.Widget { return func(gtx C) D { return a.title(gtx, s) } }

	switch cv.Tool {
	case canvas.Pen:
		add(title("PEN"), a.penPreview, title("COLOUR"), colors(penPalette, cv.PenColor),
			func(gtx C) D { return a.slider(gtx, &a.penW, "Thickness", fmt.Sprintf("%.1f pt", cv.PenWidth)) },
			title("LINE"), a.lineStyleRow,
			title("PEN TYPE"),
			a.penCards,
			title("FEEL"),
			func(gtx C) D {
				return a.slider(gtx, &a.sensF, "Pressure sensitivity", sensLabel(cv.Ink.Sensitivity))
			},
			func(gtx C) D {
				return a.slider(gtx, &a.smoothF, "Stabiliser", fmt.Sprintf("%.0f%%", cv.Ink.Smoothing*100))
			},
			func(gtx C) D { return a.toggle(gtx, &a.taperB, "Tapered ends") },
			func(gtx C) D { return a.toggle(gtx, &a.snapB, "Draw & hold to make shapes") },
			func(gtx C) D { return a.text(gtx, "Input: "+a.in.Mode(), cMuted, 12) },
		)
	case canvas.Highlighter:
		add(title("HIGHLIGHTER"), colors(hiPalette, cv.HighlighterColor),
			func(gtx C) D {
				return a.slider(gtx, &a.hiW, "Thickness", fmt.Sprintf("%.0f pt", cv.HighlighterWidth))
			},
			func(gtx C) D { return a.toggle(gtx, &a.snapB, "Hold to straighten") },
		)
	case canvas.Eraser:
		add(title("ERASER"),
			func(gtx C) D {
				return flow(gtx,
					func(gtx C) D {
						return a.chip(gtx, &a.eraseModes[0], "Whole strokes", cv.EraserMode == canvas.EraseStroke)
					},
					func(gtx C) D {
						return a.chip(gtx, &a.eraseModes[1], "Precise", cv.EraserMode == canvas.ErasePartial)
					},
				)
			},
			func(gtx C) D { return a.slider(gtx, &a.erW, "Size", fmt.Sprintf("%.0f pt", cv.EraserRadius*2)) },
		)
	case canvas.Shape:
		add(title("SHAPE"),
			func(gtx C) D {
				var cs []layout.Widget
				for i, k := range ink.Gallery {
					i, k := i, k
					cs = append(cs, func(gtx C) D { return a.shapeTile(gtx, &a.shapeKinds[i], k) })
				}
				return flow(gtx, cs...)
			},
			title("OUTLINE"), colors(penPalette, cv.PenColor),
			title("FILL"), a.fillSection,
			title("LINE"), a.lineStyleRow,
			func(gtx C) D { return a.slider(gtx, &a.penW, "Thickness", fmt.Sprintf("%.1f pt", cv.PenWidth)) },
			func(gtx C) D {
				return a.text(gtx, "Shift = perfect square/circle. Double-click a shape to type inside it. Lines and arrows: select, then drag the middle dot to bend.", cMuted, 12)
			},
		)
	case canvas.Fill:
		add(title("FILL COLOUR"), a.fillSection,
			func(gtx C) D {
				return a.text(gtx, "Tap inside any shape or closed loop — even hand-drawn — to fill it. Choose None to remove a fill.", cMuted, 12)
			},
		)
	case canvas.Text:
		add(title("FONT"), a.fontRow(&a.panelFont, &a.boldBtn, &a.italicBtn),
			title("COLOUR"), colors(penPalette, cv.PenColor), title("SIZE"),
			func(gtx C) D {
				var cs []layout.Widget
				for i, s := range textSizes {
					i, s := i, s
					cs = append(cs, func(gtx C) D {
						_, _, _, cur, _ := cv.TextStyle()
						return a.chip(gtx, &a.textSizeBtns[i], strconv.Itoa(int(s)), cur == s)
					})
				}
				return flow(gtx, cs...)
			},
			func(gtx C) D {
				return a.text(gtx, "Click the page to type. Click existing text to edit it. Esc to finish.", cMuted, 12)
			},
		)
	case canvas.Select:
		add(title("RECOLOUR SELECTION"), colors(penPalette, color.NRGBA{}),
			title("FILL SELECTED SHAPES"), a.fillSection, title("SELECT"),
			func(gtx C) D {
				return a.text(gtx, "Draw around ink to select it, or tap one stroke. Drag to move, corners to resize.", cMuted, 12)
			},
			func(gtx C) D {
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return a.chip(gtx, &a.selectAll, "Select all on page  ⌘A", false) })
			},
		)
	case canvas.Laser:
		add(title("LASER POINTER"), func(gtx C) D {
			return a.text(gtx, "Point things out while presenting or screen sharing. The trail fades away and is never saved.", cMuted, 12)
		})
	default:
		add(title("PAN"), func(gtx C) D {
			return a.text(gtx, "Drag to move around. Hold Space with any tool to pan; ⌘-scroll to zoom.", cMuted, 12)
		})
	}
	return card(gtx, func(gtx C) D {
		return material.List(a.th, &a.styleList).Layout(gtx, len(ws), func(gtx C, i int) D {
			return layout.Inset{Bottom: 4}.Layout(gtx, ws[i])
		})
	})
}

func sensLabel(v float32) string {
	switch {
	case v < 0.3:
		return "Firm"
	case v < 0.7:
		return "Medium"
	}
	return "Light touch"
}

// penPreview draws a sample stroke with the current pen, pressure curve
// and style, using a pressure swell from light to firm and back.
func (a *App) penPreview(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(64)
	paper := a.doc.Pages[a.cv.Current()].Background.Color
	paint.FillShape(gtx.Ops, paper, clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(10)).Op(gtx.Ops))
	e := ink.Engine{Cfg: a.cv.Ink, Style: a.cv.PenStyle, Base: a.cv.PenWidth}
	s := gtx.Metric.PxPerDp * 1.6 // pt -> px for the preview
	span := float32(w)/s - 24
	n := 60
	pt := func(i int) f32.Point {
		t := float64(i) / float64(n)
		return f32.Pt(12+span*float32(t), float32(h)/s/2+float32(9*math.Sin(t*2*math.Pi*1.5)))
	}
	press := func(i int) float32 {
		return float32(0.05 + 0.95*math.Pow(math.Sin(math.Pi*float64(i)/float64(n)), 1.2))
	}
	e.Begin(pt(0), press(0), true)
	for i := 1; i <= n; i++ {
		e.Add(pt(i), press(i), 0.1)
	}
	pts, ws := e.Finish(pt(n))
	col := a.cv.PenColor
	if document.IsDark(paper) == document.IsDark(col) {
		col = penPalette[map[bool]int{true: 1, false: 0}[document.IsDark(paper)]]
	}
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s))).Push(gtx.Ops)
	canvas.DrawStroke(gtx.Ops, &document.Stroke{Color: col, Width: a.cv.PenWidth, Points: pts, Widths: ws})
	t.Pop()
	return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return D{Size: image.Pt(w, h)} })
}

func (a *App) selectionBar(gtx C) {
	if r, ok := a.cv.EditingRect(); ok {
		a.floatBar(gtx, r, a.formatBar)
		return
	}
	if r, ok := a.cv.CropRect(); ok {
		a.floatBar(gtx, r, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D { return a.text(gtx, "Drag the handles to crop", cMuted, 13) })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D { return a.chip(gtx, &a.cropCancel, "Cancel  Esc", false) })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D { return a.chip(gtx, &a.cropDone, "Done  ↵", true) })
				}),
			)
		})
		return
	}
	r, ok := a.cv.SelectionRect()
	if !ok {
		return
	}
	a.floatBar(gtx, r, func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.selDelete, gDelete, false, "Delete  Del") }),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.selDup, gCopy, false, "Duplicate  ⌘D") }),
		}
		if a.cv.CanCrop() {
			ch = append(ch, layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.selCrop, gCrop, false, "Crop image") }))
		}
		ch = append(ch, layout.Rigid(vsep))
		for i, c := range a.selPalette() {
			i, c := i, c
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 6, Left: 2}.Layout(gtx, func(gtx C) D { return a.swatch(gtx, &a.selColors[i], c, false) })
			}))
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
	})
}

// floatBar shows a pill just above r (or below it near the top edge).
func (a *App) floatBar(gtx C, r image.Rectangle, w layout.Widget) {
	m := op.Record(gtx.Ops)
	cgtx := gtx
	cgtx.Constraints.Min = image.Point{}
	dims := pill(cgtx, w)
	call := m.Stop()
	x := (r.Min.X+r.Max.X)/2 - dims.Size.X/2
	y := r.Min.Y - dims.Size.Y - gtx.Dp(10)
	if y < gtx.Dp(64) {
		y = r.Max.Y + gtx.Dp(10)
	}
	x = max(gtx.Dp(8), min(x, gtx.Constraints.Max.X-dims.Size.X-gtx.Dp(8)))
	y = max(gtx.Dp(8), min(y, gtx.Constraints.Max.Y-dims.Size.Y-gtx.Dp(70)))
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

func (a *App) pagePill(gtx C) D {
	return pill(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.pagePrev, gUp, false, "Previous page  PgUp") }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
					return a.text(gtx, fmt.Sprintf("%d / %d", a.cv.Current()+1, len(a.doc.Pages)), cText, 13)
				})
			}),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.pageNext, gDown, false, "Next page  PgDn") }),
			layout.Rigid(vsep),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.addPage, gAdd, false, "Add page") }),
		)
	})
}

func (a *App) zoomPill(gtx C) D {
	return pill(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				col := cMuted
				if a.in.PressureSeen {
					col = cAccent
				}
				return layout.Inset{Left: 8, Right: 6}.Layout(gtx, func(gtx C) D { return a.text(gtx, a.in.Mode(), col, 12) })
			}),
			layout.Rigid(vsep),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.zoomOut, gZoomOut, false, "Zoom out  ⌘−") }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 2, Right: 2}.Layout(gtx, func(gtx C) D {
					return a.text(gtx, fmt.Sprintf("%d%%", int(a.cv.Zoom*100+0.5)), cText, 13)
				})
			}),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.zoomIn, gZoomIn, false, "Zoom in  ⌘+") }),
			layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.fit, gFit, false, "Fit width  ⌘0") }),
		)
	})
}

func (a *App) toastView(gtx C) D {
	return pill(gtx, func(gtx C) D {
		return layout.Inset{Top: 6, Bottom: 6, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
			return a.text(gtx, a.toast, cText, 13)
		})
	})
}

// ---- pages panel ----

// floating sizes a side panel to width w (dp) and the full available
// height, on a rounded surface.
func floating(gtx C, w unit.Dp) C {
	sz := image.Pt(gtx.Dp(w), gtx.Constraints.Max.Y)
	gtx.Constraints = layout.Exact(sz)
	surface(gtx, image.Rectangle{Max: sz}, gtx.Dp(14))
	return gtx
}

func (a *App) pagesPanel(gtx C) D {
	gtx = floating(gtx, 184)
	if len(a.thumbClicks) != len(a.doc.Pages) {
		a.thumbClicks = make([]widget.Clickable, len(a.doc.Pages))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Left: 14, Right: 6, Bottom: 2}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return a.title(gtx, fmt.Sprintf("PAGES · %d", len(a.doc.Pages))) }),
					layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.addPage, gAdd, false, "Add page") }),
					layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.closePages, gClose, false, "Close") }),
				)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return material.List(a.th, &a.thumbs).Layout(gtx, len(a.doc.Pages), a.thumb)
		}),
	)
}

func (a *App) thumb(gtx C, i int) D {
	p := a.doc.Pages[i]
	current := i == a.cv.Current()
	return layout.Inset{Top: 6, Bottom: 6, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return a.thumbClicks[i].Layout(gtx, func(gtx C) D {
					b := gtx.Dp(3)
					tw := float32(gtx.Constraints.Max.X - 2*b)
					s := tw / p.Width
					size := image.Pt(int(tw)+2*b, int(p.Height*s)+2*b)
					ring := cBorder
					if current {
						ring = cAccent
					} else if a.thumbClicks[i].Hovered() {
						ring = cMuted
					}
					paint.FillShape(gtx.Ops, ring, clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(6)).Op(gtx.Ops))
					t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(float32(b), float32(b)))).Push(gtx.Ops)
					cl := clip.Rect{Max: image.Pt(int(p.Width), int(p.Height))}.Push(gtx.Ops)
					a.cv.PageOps(p).Add(gtx.Ops)
					cl.Pop()
					t.Pop()
					return D{Size: size}
				})
			}),
			layout.Rigid(func(gtx C) D {
				if !current {
					return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return a.text(gtx, strconv.Itoa(i+1), cMuted, 12) })
				}
				return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return a.smallBtn(gtx, &a.pageUp, gUp, "Move up") }),
						layout.Rigid(func(gtx C) D { return a.smallBtn(gtx, &a.pageDown, gDown, "Move down") }),
						layout.Rigid(func(gtx C) D { return a.smallBtn(gtx, &a.pageDup, gCopy, "Duplicate") }),
						layout.Rigid(func(gtx C) D { return a.smallBtn(gtx, &a.pageDel, gDelete, "Delete page") }),
					)
				})
			}),
		)
	})
}

func (a *App) smallBtn(gtx C, c *widget.Clickable, g glyph, tip string) D {
	return c.Layout(gtx, func(gtx C) D {
		sz := gtx.Dp(28)
		if c.Hovered() {
			paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(image.Rectangle{Max: image.Pt(sz, sz)}, gtx.Dp(7)).Op(gtx.Ops))
			a.tooltip(gtx, tip, image.Pt(sz/2, sz+gtx.Dp(4)))
		}
		isz := gtx.Dp(16)
		t := op.Offset(image.Pt((sz-isz)/2, (sz-isz)/2)).Push(gtx.Ops)
		g(gtx, isz, cMuted)
		t.Pop()
		return D{Size: image.Pt(sz, sz)}
	})
}

// ---- page setup ----

func (a *App) setupPanel(gtx C) D {
	gtx = floating(gtx, 300)
	cur := a.doc.Pages[a.cv.Current()]
	title := func(s string) layout.Widget { return func(gtx C) D { return a.title(gtx, s) } }
	ws := []layout.Widget{
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					l := material.H6(a.th, "Page setup")
					l.Color = cText
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.closeSetup, gClose, false, "Close") }),
			)
		},
		title("APPLY TO"),
		func(gtx C) D {
			return flow(gtx,
				func(gtx C) D { return a.chip(gtx, &a.scope[0], "This page", !a.allPages) },
				func(gtx C) D { return a.chip(gtx, &a.scope[1], "All pages", a.allPages) },
			)
		},
		title("PAPER"),
		func(gtx C) D {
			var tiles []layout.Widget
			for i, s := range document.Styles {
				i, s := i, s
				tiles = append(tiles, func(gtx C) D { return a.paperTile(gtx, &a.paperStyles[i], s, cur) })
			}
			return flow(gtx, tiles...)
		},
		title("BACKGROUND IMAGE"),
		func(gtx C) D {
			ws := []layout.Widget{func(gtx C) D { return a.chip(gtx, &a.bgImageBtn, "Choose image…", false) }}
			if cur.Background.Type == "pixmap" {
				ws = append(ws, func(gtx C) D { return a.chip(gtx, &a.bgRemoveBtn, "Remove", false) })
			}
			return flow(gtx, ws...)
		},
		func(gtx C) D {
			if cur.Background.Type != "pixmap" {
				return a.text(gtx, "Put a photo or design behind your writing. Set it as default below to use it for every new notebook.", cMuted, 12)
			}
			return a.slider(gtx, &a.softenF, "Soften", fmt.Sprintf("%.0f%%", cur.Background.Soften*100))
		},
		title("PAPER COLOUR"),
		func(gtx C) D {
			var cs []layout.Widget
			for i, pc := range paperPresets {
				i, pc := i, pc
				cs = append(cs, func(gtx C) D {
					d := a.swatch(gtx, &a.paperPresetBtns[i], pc.c, cur.Background.Color == pc.c)
					if a.paperPresetBtns[i].Hovered() {
						a.tooltip(gtx, pc.name, image.Pt(d.Size.X/2-gtx.Dp(3), d.Size.Y))
					}
					return d
				})
			}
			cs = append(cs, func(gtx C) D { return a.wheelButton(gtx, &a.customPaper, a.showPaperPicker) })
			return flow(gtx, cs...)
		},
		func(gtx C) D {
			if !a.showPaperPicker {
				return D{}
			}
			if a.paperOld == nil {
				a.paperPicker.Sync(cur.Background.Color)
			}
			return layout.Inset{Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
				d := a.paperPicker.Layout(gtx, a.th, false)
				if a.paperPicker.Changed || a.paperPicker.Committed {
					a.setPaperColor(a.paperPicker.Color(), a.paperPicker.Committed)
				}
				return d
			})
		},
		title("SIZE"),
		func(gtx C) D {
			var cs []layout.Widget
			for i, s := range document.PaperSizes {
				i, s := i, s
				active := (cur.Width == s.W && cur.Height == s.H) || (cur.Width == s.H && cur.Height == s.W)
				cs = append(cs, func(gtx C) D { return a.chip(gtx, &a.paperSizes[i], s.Name, active) })
			}
			return flow(gtx, cs...)
		},
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return a.field(gtx, &a.customW, "W") }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return a.text(gtx, "×", cMuted, 13) })
				}),
				layout.Rigid(func(gtx C) D { return a.field(gtx, &a.customH, "H") }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 6, Right: 8}.Layout(gtx, func(gtx C) D { return a.text(gtx, "mm", cMuted, 13) })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D { return a.chip(gtx, &a.customApply, "Apply", false) })
				}),
			)
		},
		title("ORIENTATION"),
		func(gtx C) D {
			land := cur.Width > cur.Height
			return flow(gtx,
				func(gtx C) D { return a.chip(gtx, &a.orient[0], "Portrait", !land) },
				func(gtx C) D { return a.chip(gtx, &a.orient[1], "Landscape", land) },
			)
		},
		func(gtx C) D {
			return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
				return a.text(gtx, fmt.Sprintf("Current: %.0f × %.0f mm. New pages copy the current page.", cur.Width*25.4/72, cur.Height*25.4/72), cMuted, 12)
			})
		},
		title("NEW NOTEBOOKS"),
		func(gtx C) D { return a.text(gtx, "Default page: "+a.defaultSummary(), cText, 13) },
		func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				return flow(gtx,
					func(gtx C) D { return a.chip(gtx, &a.setDefault, "Use this page as default", false) },
					func(gtx C) D { return a.chip(gtx, &a.resetDefault, "Reset", false) },
				)
			})
		},
		func(gtx C) D {
			return a.text(gtx, "dot! opens with this page whenever you start without a document (and for ⌘N).", cMuted, 12)
		},
	}
	return layout.Inset{Top: 8, Left: 16, Right: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
		return material.List(a.th, &a.setupList).Layout(gtx, len(ws), func(gtx C, i int) D { return ws[i](gtx) })
	})
}

func (a *App) field(gtx C, e *widget.Editor, hint string) D {
	w := gtx.Dp(56)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 6, Bottom: 6, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		ed := material.Editor(a.th, e, hint)
		ed.Color, ed.HintColor, ed.TextSize = cText, cMuted, unit.Sp(13)
		return ed.Layout(gtx)
	})
	call := m.Stop()
	paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(7)).Op(gtx.Ops))
	call.Add(gtx.Ops)
	return dims
}

// paperTile is a mini preview of a ruling style.
func (a *App) paperTile(gtx C, c *widget.Clickable, s document.Style, cur *document.Page) D {
	return layout.Inset{Right: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					tw, th := gtx.Dp(44), gtx.Dp(58)
					r := image.Rectangle{Max: image.Pt(tw, th)}
					ring := cBorder
					if cur.Background.Style == s {
						ring = cAccent
					} else if c.Hovered() {
						ring = cMuted
					}
					paint.FillShape(gtx.Ops, ring, clip.UniformRRect(r, gtx.Dp(6)).Op(gtx.Ops))
					in := r.Inset(gtx.Dp(2))
					paper := cur.Background.Color
					paint.FillShape(gtx.Ops, paper, clip.UniformRRect(in, gtx.Dp(4)).Op(gtx.Ops))
					line := color.NRGBA{0x40, 0xa0, 0xff, 0xb0}
					if document.IsDark(paper) {
						line = color.NRGBA{0x80, 0x80, 0x90, 0xb0}
					}
					step := gtx.Dp(6)
					px := max(1, gtx.Dp(0.75))
					switch s {
					case document.StyleRuled, document.StyleLined:
						for y := in.Min.Y + step*2; y < in.Max.Y-2; y += step {
							paint.FillShape(gtx.Ops, line, clip.Rect{Min: image.Pt(in.Min.X+2, y), Max: image.Pt(in.Max.X-2, y+px)}.Op())
						}
						if s == document.StyleLined {
							x := in.Min.X + gtx.Dp(8)
							paint.FillShape(gtx.Ops, document.MarginColor, clip.Rect{Min: image.Pt(x, in.Min.Y+2), Max: image.Pt(x+px, in.Max.Y-2)}.Op())
						}
					case document.StyleGraph:
						for y := in.Min.Y + step; y < in.Max.Y-1; y += step {
							paint.FillShape(gtx.Ops, line, clip.Rect{Min: image.Pt(in.Min.X+1, y), Max: image.Pt(in.Max.X-1, y+px)}.Op())
						}
						for x := in.Min.X + step; x < in.Max.X-1; x += step {
							paint.FillShape(gtx.Ops, line, clip.Rect{Min: image.Pt(x, in.Min.Y+1), Max: image.Pt(x+px, in.Max.Y-1)}.Op())
						}
					case document.StyleDotted:
						d := max(2, gtx.Dp(1.6))
						for y := in.Min.Y + step; y < in.Max.Y-1; y += step {
							for x := in.Min.X + step; x < in.Max.X-1; x += step {
								paint.FillShape(gtx.Ops, line, clip.Ellipse(image.Rect(x-d/2, y-d/2, x+d/2+1, y+d/2+1)).Op(gtx.Ops))
							}
						}
					}
					return D{Size: r.Max}
				}),
				layout.Rigid(func(gtx C) D {
					names := map[document.Style]string{document.StylePlain: "Plain", document.StyleRuled: "Ruled",
						document.StyleLined: "Lined", document.StyleGraph: "Grid", document.StyleDotted: "Dots"}
					return layout.Inset{Top: 3}.Layout(gtx, func(gtx C) D { return a.text(gtx, names[s], cMuted, 11) })
				}),
			)
		})
	})
}

// ---- menu & help ----

func (a *App) overlayBackdrop(gtx C, dim bool) {
	a.backdrop.Layout(gtx, func(gtx C) D {
		if dim {
			paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 0x70}, clip.Rect{Max: gtx.Constraints.Max}.Op())
		}
		return D{Size: gtx.Constraints.Max}
	})
}

func (a *App) menu(gtx C) {
	a.overlayBackdrop(gtx, false)
	full := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = gtx.Dp(300)
	gtx.Constraints.Max.Y = full.Y - gtx.Dp(80)
	// Measure, then place the menu just above the ☰ button (bottom-left).
	m := op.Record(gtx.Ops)
	dims := a.menuCard(gtx)
	call := m.Stop()
	y := full.Y - dims.Size.Y - gtx.Dp(62)
	if a.home {
		y = gtx.Dp(70)
	}
	defer op.Offset(image.Pt(gtx.Dp(10), max(gtx.Dp(10), y))).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

func (a *App) menuCard(gtx C) D {
	items := []struct {
		g          glyph
		label, key string
	}{
		{gHome, "Home · all notebooks", ""}, {gAdd, "New notebook", "⌘N"}, {gWindow, "New window", "Shift ⌘N"}, {iconGlyph(iconsFolder), "Open…", "⌘O"}, {gPDF, "Annotate a PDF…", ""}, {iconGlyph(iconsSave), "Save", "⌘S"},
		{iconGlyph(iconsSave), "Save as…", "Shift ⌘S"}, {gImage, "Insert image…", "or ⌘V"}, {gExport, "Export as PDF…", "⌘E"},
		{gExport, "Export page as PNG…", ""}, {gHelp, "Keyboard shortcuts", "?"},
	}
	return card(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ch := []layout.FlexChild{}
		for i, it := range items {
			i, it := i, it
			ch = append(ch, layout.Rigid(func(gtx C) D { return a.menuRow(gtx, &a.menuItems[i], it.g, it.label, it.key) }))
		}
		ch = append(ch,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8, Top: 6}.Layout(gtx, func(gtx C) D { return a.title(gtx, "SAVE FOLDER") })
			}),
			layout.Rigid(func(gtx C) D {
				return a.menuRow(gtx, &a.saveFolderBtn, iconGlyph(iconsFolder), shortPath(a.cfg.Dir()), "Change…")
			}),
			layout.Rigid(func(gtx C) D {
				return a.menuRow(gtx, &a.revealFolderBtn, gWindow, "Show save folder", "")
			}),
		)
		if len(a.cfg.Recent) > 0 {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8, Top: 6}.Layout(gtx, func(gtx C) D { return a.title(gtx, "RECENT") })
			}))
			for i, p := range a.cfg.Recent {
				if i >= 4 {
					break
				}
				i, p := i, p
				ch = append(ch, layout.Rigid(func(gtx C) D {
					return a.menuRow(gtx, &a.recent[i], gPages, filepath.Base(p), "")
				}))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
	})
}

func (a *App) menuRow(gtx C, c *widget.Clickable, g glyph, label, key string) D {
	return c.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Top: 8, Bottom: 8, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					sz := gtx.Dp(18)
					g(gtx, sz, cMuted)
					return D{Size: image.Pt(sz, sz)}
				}),
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return a.text(gtx, label, cText, 14) })
				}),
				layout.Rigid(func(gtx C) D { return a.text(gtx, key, cMuted, 12) }),
			)
		})
		call := m.Stop()
		if c.Hovered() {
			paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(8)).Op(gtx.Ops))
		}
		call.Add(gtx.Ops)
		return dims
	})
}

func (a *App) help(gtx C) {
	a.overlayBackdrop(gtx, true)
	rows := [][2]string{
		{"V P H E S B T L G", "Select · Pen · Highlighter · Eraser · Shapes · Fill · Text · Laser · Pan"},
		{"Shift + drag (Shapes)", "Perfect square, circle or 45° line"},
		{"Double-click a shape", "Type a label inside it (moves with the shape)"},
		{"Select a line/arrow", "Drag the middle dot to bend it, end dots to move ends"},
		{"Pinch · two-finger scroll", "Zoom · move around (trackpad)"},
		{"Two-finger double-tap", "Smart zoom: fit page ↔ 2×"},
		{"Hold Space", "Pan with any tool"},
		{"F", "Focus mode: just you and the page"},
		{"Draw, then hold still", "Snap to line, rectangle, circle, triangle"},
		{"[   ]", "Thinner / thicker"},
		{"⌘Z   Shift ⌘Z", "Undo / redo"},
		{"⌘C  ⌘X  ⌘D", "Copy · cut · duplicate selection"},
		{"⌘V", "Paste ink, a screenshot/image, or text"},
		{"⌘A   Del", "Select all on page · delete selection"},
		{"⌘S   Shift ⌘S   ⌘O   ⌘N", "Save · save as · open · new"},
		{"Shift ⌘N", "New window (each window has its own file)"},
		{"⌘E", "Export PDF"},
		{"⌘+  ⌘−  ⌘0  ⌘scroll", "Zoom in · out · fit · at cursor"},
		{"PgUp  PgDn", "Previous / next page"},
		{"Right-drag · two fingers", "Pan"},
		{"Esc", "Close panels / clear selection"},
	}
	at(gtx, layout.Center, layout.Inset{}, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(560))
		gtx.Constraints.Min = image.Point{}
		return card(gtx, func(gtx C) D {
			ch := []layout.FlexChild{layout.Rigid(func(gtx C) D {
				l := material.H6(a.th, "Keyboard shortcuts")
				l.Color = cText
				return layout.Inset{Bottom: 10}.Layout(gtx, l.Layout)
			})}
			for _, r := range rows {
				r := r
				ch = append(ch, layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								gtx.Constraints.Min.X = gtx.Dp(190)
								return a.text(gtx, r[0], cAccent, 13)
							}),
							layout.Flexed(1, func(gtx C) D { return a.text(gtx, r[1], cText, 13) }),
						)
					})
				}))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					return a.text(gtx, "On Windows use Ctrl instead of ⌘. Click anywhere to close.", cMuted, 12)
				})
			}))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
		})
	})
}

func (a *App) defaultSummary() string {
	dp := a.cfg.DefaultPage
	if dp.W == 0 {
		return "A4 · Lined · White"
	}
	if dp.ImagePath != "" {
		dp.Style = "image"
	}
	size := fmt.Sprintf("%.0f×%.0f mm", dp.W*25.4/72, dp.H*25.4/72)
	for _, ps := range document.PaperSizes {
		if (ps.W == dp.W && ps.H == dp.H) || (ps.W == dp.H && ps.H == dp.W) {
			size = ps.Name
		}
	}
	if dp.W > dp.H {
		size += " landscape"
	}
	col := hexString(color.NRGBA{dp.Color[0], dp.Color[1], dp.Color[2], 0xff})
	for _, pp := range paperPresets {
		if pp.c == (color.NRGBA{dp.Color[0], dp.Color[1], dp.Color[2], dp.Color[3]}) {
			col = pp.name
		}
	}
	style := dp.Style
	if style != "" {
		style = string(style[0]-32) + style[1:]
	}
	return size + " · " + style + " · " + col
}

// fontDrop is a font-family dropdown.
type fontDrop struct {
	btn   widget.Clickable
	open  bool
	items [16]widget.Clickable
	list  widget.List
}

// fontRow: a font dropdown (each font previewed in itself) and B / I.
func (a *App) fontRow(dd *fontDrop, bold, italic *widget.Clickable) layout.Widget {
	return func(gtx C) D {
		fam, b, i, _, _ := a.cv.TextStyle()
		ws := []layout.Widget{func(gtx C) D { return a.fontDropdown(gtx, dd, fam) }}
		ws = append(ws,
			func(gtx C) D { return a.fontChip(gtx, bold, "B", canvas.GioFont(fam, true, false), b) },
			func(gtx C) D { return a.fontChip(gtx, italic, "I", canvas.GioFont(fam, false, true), i) },
		)
		return flow(gtx, ws...)
	}
}

// fontChip is a chip whose label is drawn in the font it stands for.
func (a *App) fontChip(gtx C, c *widget.Clickable, label string, f font.Font, active bool) D {
	return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			l := material.Body2(a.th, label)
			l.Font, l.TextSize, l.Color = f, unit.Sp(15), cText
			bg := cSurface2
			if active {
				bg, l.Color = cAccent, cWhite
			} else if c.Hovered() {
				bg = cBorder
			}
			m := op.Record(gtx.Ops)
			gtx.Constraints.Min = image.Point{}
			dims := layout.Inset{Top: 4, Bottom: 4, Left: 11, Right: 11}.Layout(gtx, l.Layout)
			call := m.Stop()
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(8)).Op(gtx.Ops))
			call.Add(gtx.Ops)
			return dims
		})
	})
}

// formatBar floats above the text being typed: font, bold, italic, size,
// colour — everything one click away while writing.
func (a *App) formatBar(gtx C) D {
	_, _, _, size, col := a.cv.TextStyle()
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Left: 4}.Layout(gtx, a.fontRow(&a.fmtFont, &a.fmtBold, &a.fmtItalic))
		}),
		layout.Rigid(vsep),
		layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.fmtSmaller, gZoomOut, false, "Smaller") }),
		layout.Rigid(func(gtx C) D { return a.text(gtx, strconv.Itoa(int(size)), cText, 13) }),
		layout.Rigid(func(gtx C) D { return a.iconBtn(gtx, &a.fmtBigger, gZoomIn, false, "Bigger") }),
		layout.Rigid(vsep),
	}
	for i, c := range a.selPalette()[:len(a.fmtColors)] {
		i, c := i, c
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Left: 2}.Layout(gtx, func(gtx C) D { return a.swatch(gtx, &a.fmtColors[i], c, c == col) })
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
}

// shapeTile is a gallery button that previews the shape it makes.
func (a *App) shapeTile(gtx C, c *widget.Clickable, k ink.Shape) D {
	return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			sz := gtx.Dp(40)
			r := image.Rectangle{Max: image.Pt(sz, sz)}
			bg := cSurface2
			active := a.cv.ShapeKind == k
			if active {
				bg = cAccent
			} else if c.Hovered() {
				bg = cBorder
			}
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, gtx.Dp(8)).Op(gtx.Ops))
			m := float32(gtx.Dp(9))
			col := cText
			if active {
				col = cWhite
			}
			st := &document.Stroke{Color: col, Width: float32(gtx.Dp(1.6)),
				Points: ink.Build(k, f32.Pt(m, m), f32.Pt(float32(sz)-m, float32(sz)-m))}
			if k.Closed() && a.cv.FillColor.A > 0 {
				st.Fill = a.cv.FillColor
			}
			canvas.DrawStroke(gtx.Ops, st)
			if c.Hovered() {
				a.tooltip(gtx, ink.ShapeNames[k], image.Pt(sz/2, sz+gtx.Dp(4)))
			}
			return D{Size: r.Max}
		})
	})
}

var lineStyles = []struct{ name, style string }{{"Solid", ""}, {"Dashed", "dash"}, {"Dotted", "dot"}, {"Dash-dot", "dashdot"}}

// lineStyleRow: tiles that show each pattern.
func (a *App) lineStyleRow(gtx C) D {
	var ws []layout.Widget
	for i, ls := range lineStyles {
		i, ls := i, ls
		ws = append(ws, func(gtx C) D {
			return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return a.lineStyles[i].Layout(gtx, func(gtx C) D {
					w, h := gtx.Dp(62), gtx.Dp(30)
					r := image.Rectangle{Max: image.Pt(w, h)}
					active := a.cv.LineStyle == ls.style
					bg, col := cSurface2, cText
					if active {
						bg, col = cAccent, cWhite
					} else if a.lineStyles[i].Hovered() {
						bg = cBorder
					}
					paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, gtx.Dp(8)).Op(gtx.Ops))
					y, m := float32(h)/2, float32(gtx.Dp(10))
					canvas.DrawStroke(gtx.Ops, &document.Stroke{Color: col, Width: float32(gtx.Dp(2)), Style: ls.style,
						Points: []f32.Point{f32.Pt(m, y), f32.Pt(float32(w)-m, y)}})
					if a.lineStyles[i].Hovered() {
						a.tooltip(gtx, ls.name, image.Pt(w/2, h+gtx.Dp(4)))
					}
					return D{Size: r.Max}
				})
			})
		})
	}
	return flow(gtx, ws...)
}

// fillSection: None, palette, colour wheel and opacity for fills. With
// the Select tool it fills the selected shapes; otherwise it sets the
// fill for new shapes and the Fill tool.
func (a *App) fillSection(gtx C) D {
	cur := a.cv.FillColor
	sel := a.cv.Tool == canvas.Select
	if sel {
		cur = color.NRGBA{}
	}
	solid := cur
	solid.A = 0xff
	if !sel && cur.A > 0 {
		a.fillPicker.Sync(solid)
	}
	ws := []layout.Widget{func(gtx C) D { return a.chip(gtx, &a.fillNone, "None", !sel && cur.A == 0) }}
	for i, c := range penPalette {
		i, c := i, c
		ws = append(ws, func(gtx C) D { return a.swatch(gtx, &a.fillColors[i], c, !sel && cur.A > 0 && c == solid) })
	}
	ws = append(ws, func(gtx C) D { return a.wheelButton(gtx, &a.customFill, a.showFillPicker) })
	ch := []layout.FlexChild{layout.Rigid(func(gtx C) D { return flow(gtx, ws...) })}
	if !sel && cur.A > 0 {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return a.slider(gtx, &a.fillOpacity, "Fill opacity", fmt.Sprintf("%.0f%%", float32(cur.A)/2.55))
		}))
	}
	if a.showFillPicker {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				d := a.fillPicker.Layout(gtx, a.th, false)
				if a.fillPicker.Changed || a.fillPicker.Committed {
					a.applyFill(a.fillPicker.Color(), a.fillPicker.Committed)
				}
				return d
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

// menuPill: the dot! logo, bottom-left, with no background; it opens
// the menu.
func (a *App) menuPill(gtx C) D {
	return a.menuBtn.Layout(gtx, func(gtx C) D {
		sz := gtx.Dp(40)
		if a.menuBtn.Hovered() || a.showMenu {
			sz = gtx.Dp(44) // a gentle grow instead of a backdrop
		}
		box := gtx.Dp(44)
		t := op.Offset(image.Pt((box-sz)/2, (box-sz)/2)).Push(gtx.Ops)
		logoMarkOutlined(gtx, sz)
		t.Pop()
		if a.menuBtn.Hovered() && !a.showMenu {
			a.tooltip(gtx, "Menu", image.Pt(box/2, -gtx.Dp(30)))
		}
		return D{Size: image.Pt(box, box)}
	})
}

// fontDropdown shows the current font; clicking opens a menu of every
// font, each written in its own typeface. The menu is drawn on top of
// everything (op.Defer) so it can overflow its bar.
func (a *App) fontDropdown(gtx C, dd *fontDrop, cur string) D {
	d := layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
		return dd.btn.Layout(gtx, func(gtx C) D {
			m := op.Record(gtx.Ops)
			dims := layout.Inset{Top: 4, Bottom: 4, Left: 12, Right: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Dp(104)
						gtx.Constraints.Max.X = gtx.Dp(130)
						l := material.Body2(a.th, cur)
						l.Font, l.TextSize, l.Color, l.MaxLines = canvas.GioFont(cur, false, false), unit.Sp(15), cText, 1
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						sz := gtx.Dp(18)
						gChevron(gtx, sz, cMuted)
						return D{Size: image.Pt(sz, sz)}
					}),
				)
			})
			call := m.Stop()
			bg := cSurface2
			if dd.open || dd.btn.Hovered() {
				bg = cBorder
			}
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(8)).Op(gtx.Ops))
			call.Add(gtx.Ops)
			return dims
		})
	})
	if dd.open {
		m := op.Record(gtx.Ops)
		t := op.Offset(image.Pt(0, d.Size.Y)).Push(gtx.Ops)
		a.fontMenu(gtx, dd, cur)
		t.Pop()
		op.Defer(gtx.Ops, m.Stop())
	}
	return d
}

func (a *App) fontMenu(gtx C, dd *fontDrop, cur string) D {
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = gtx.Dp(290)
	return card(gtx, func(gtx C) D {
		var rows []layout.FlexChild
		for k, f := range canvas.Families {
			k, f := k, f
			if k == 3 { // built-ins, then handwriting
				rows = append(rows, layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 4, Left: 8}.Layout(gtx, func(gtx C) D { return a.title(gtx, "HANDWRITING") })
				}))
			}
			rows = append(rows, layout.Rigid(func(gtx C) D {
				return dd.items[k].Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					m := op.Record(gtx.Ops)
					dims := layout.Inset{Top: 6, Bottom: 6, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								l := material.Body1(a.th, f.Name)
								l.Font, l.TextSize, l.Color = canvas.GioFont(f.Name, false, false), unit.Sp(19), cText
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx C) D { return a.text(gtx, f.Hint, cMuted, 11) }),
						)
					})
					call := m.Stop()
					r := image.Rectangle{Max: dims.Size}
					switch {
					case f.Name == cur:
						paint.FillShape(gtx.Ops, color.NRGBA{cAccent.R, cAccent.G, cAccent.B, 0x55}, clip.UniformRRect(r, gtx.Dp(8)).Op(gtx.Ops))
					case dd.items[k].Hovered():
						paint.FillShape(gtx.Ops, cSurface2, clip.UniformRRect(r, gtx.Dp(8)).Op(gtx.Ops))
					}
					call.Add(gtx.Ops)
					return dims
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
}
