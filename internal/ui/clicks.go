package ui

import (
	"image/color"
	"strconv"

	"dot/internal/settings"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/ink"
)

var toolOrder = []canvas.Tool{canvas.Select, canvas.Pen, canvas.Highlighter, canvas.Eraser, canvas.Shape, canvas.Fill, canvas.Text, canvas.Laser, canvas.Hand}

// handleClicks applies every button press for this frame before layout.
func (a *App) handleClicks(gtx C) {
	cv := a.cv
	for i := range a.tools {
		if a.tools[i].Clicked(gtx) {
			t := toolOrder[i]
			if cv.Tool == t {
				a.showStyle = !a.showStyle // tap the active tool for its options
			} else {
				cv.SetTool(t)
				// Shapes and Fill need a choice first: show their panel.
				a.showStyle = t == canvas.Shape || t == canvas.Fill
			}
		}
	}
	if a.styleBtn.Clicked(gtx) {
		a.showStyle = !a.showStyle
	}
	if a.backdrop.Clicked(gtx) {
		a.showMenu, a.showStyle, a.showHelp = false, false, false
	}
	if a.homeLogo.Clicked(gtx) {
		a.goHome()
	}
	if a.menuBtn.Clicked(gtx) {
		a.showMenu = !a.showMenu
	}
	if a.undoBtn.Clicked(gtx) {
		a.undo()
	}
	if a.redoBtn.Clicked(gtx) {
		a.redo()
	}
	if a.pagesBtn.Clicked(gtx) {
		a.showPages = !a.showPages
	}
	if a.setupBtn.Clicked(gtx) {
		a.showSetup = !a.showSetup
	}
	if a.helpBtn.Clicked(gtx) {
		a.showHelp = !a.showHelp
	}

	// Menu.
	actions := []func(){a.goHome, a.newNotebook, func() { a.newWindow("") }, a.doOpen, a.choosePDF, func() { a.doSave(false, false) }, func() { a.doSave(true, false) },
		a.insertImageFile, a.exportPDF, a.exportPNG, func() { a.showHelp = true }}
	for i := range a.menuItems {
		if a.menuItems[i].Clicked(gtx) {
			a.showMenu = false
			actions[i]()
		}
	}
	if a.saveFolderBtn.Clicked(gtx) {
		a.showMenu = false
		a.chooseSaveFolder()
	}
	if a.revealFolderBtn.Clicked(gtx) {
		a.showMenu = false
		a.revealSaveFolder()
	}
	for i := range a.recent {
		if a.recent[i].Clicked(gtx) && i < len(a.cfg.Recent) {
			a.showMenu = false
			a.openAsync(a.cfg.Recent[i], nil)
		}
	}

	// Style panel.
	for i := range a.colors {
		if !a.colors[i].Clicked(gtx) {
			continue
		}
		if cv.Tool == canvas.Highlighter {
			if i < len(hiPalette) {
				a.applyToolColor(hiPalette[i], true)
			}
		} else {
			a.applyToolColor(penPalette[i], true)
		}
	}
	for i := range a.recentClicks {
		if a.recentClicks[i].Clicked(gtx) && i < len(a.recentColors) {
			a.applyToolColor(a.recentColors[i], true)
		}
	}
	if a.customPen.Clicked(gtx) {
		a.showPenPicker = !a.showPenPicker
	}
	if a.customPaper.Clicked(gtx) {
		a.showPaperPicker = !a.showPaperPicker
	}
	for i := range a.paperPresetBtns {
		if a.paperPresetBtns[i].Clicked(gtx) {
			a.setPaperColor(paperPresets[i].c, true)
		}
	}
	for i := range a.penStyles {
		if a.penStyles[i].Clicked(gtx) {
			cv.PenStyle = ink.Style(i)
		}
	}
	for i := range a.shapeKinds {
		if a.shapeKinds[i].Clicked(gtx) {
			cv.ShapeKind = ink.Gallery[i]
		}
	}
	for i := range a.lineStyles {
		if a.lineStyles[i].Clicked(gtx) {
			cv.LineStyle = lineStyles[i].style
		}
	}
	if a.fillNone.Clicked(gtx) {
		a.applyFill(color.NRGBA{}, true)
	}
	for i := range a.fillColors {
		if a.fillColors[i].Clicked(gtx) {
			a.applyFill(penPalette[i], true)
		}
	}
	if a.customFill.Clicked(gtx) {
		a.showFillPicker = !a.showFillPicker
	}
	if a.fillOpacity.Update(gtx) {
		if cv.FillColor.A > 0 {
			cv.FillColor.A = uint8(max(0.1, a.fillOpacity.Value) * 255)
		}
	} else if cv.FillColor.A > 0 {
		a.fillOpacity.Value = float32(cv.FillColor.A) / 255
	}
	for i := range a.eraseModes {
		if a.eraseModes[i].Clicked(gtx) {
			cv.EraserMode = canvas.EraserMode(i)
		}
	}
	// Text style: the same controls in the Text panel and the floating
	// format bar; they apply to new text and to the text being edited.
	fam, bold, italic, size, col := cv.TextStyle()
	restyle := func() { cv.SetTextStyle(fam, bold, italic, size, col) }
	for i := range a.textSizeBtns {
		if a.textSizeBtns[i].Clicked(gtx) {
			size = textSizes[i]
			restyle()
		}
	}
	for _, dd := range []*fontDrop{&a.panelFont, &a.fmtFont} {
		if dd.btn.Clicked(gtx) {
			dd.open = !dd.open
		}
		for i, f := range canvas.FontFamilies {
			if dd.items[i].Clicked(gtx) {
				fam, dd.open = f, false
				restyle()
			}
		}
	}
	if !cv.Editing() {
		a.fmtFont.open = false
	}
	if a.boldBtn.Clicked(gtx) || a.fmtBold.Clicked(gtx) {
		bold = !bold
		restyle()
	}
	if a.italicBtn.Clicked(gtx) || a.fmtItalic.Clicked(gtx) {
		italic = !italic
		restyle()
	}
	if a.fmtSmaller.Clicked(gtx) {
		size = max(6, size-2)
		restyle()
	}
	if a.fmtBigger.Clicked(gtx) {
		size = min(200, size+2)
		restyle()
	}
	for i := range a.fmtColors {
		if a.fmtColors[i].Clicked(gtx) {
			a.applyToolColor(a.selPalette()[i], true)
		}
	}
	if a.penW.Update(gtx) {
		cv.PenWidth = logMap(a.penW.Value, 0.2, 12)
	} else {
		a.penW.Value = logUnmap(cv.PenWidth, 0.2, 12)
	}
	if a.hiW.Update(gtx) {
		cv.HighlighterWidth = logMap(a.hiW.Value, 2, 40)
	} else {
		a.hiW.Value = logUnmap(cv.HighlighterWidth, 2, 40)
	}
	if a.erW.Update(gtx) {
		cv.EraserRadius = logMap(a.erW.Value, 2, 50)
	} else {
		a.erW.Value = logUnmap(cv.EraserRadius, 2, 50)
	}
	if a.sensF.Update(gtx) {
		cv.Ink.Sensitivity = a.sensF.Value
	} else {
		a.sensF.Value = cv.Ink.Sensitivity
	}
	if a.smoothF.Update(gtx) {
		cv.Ink.Smoothing = a.smoothF.Value
	} else {
		a.smoothF.Value = cv.Ink.Smoothing
	}
	if a.taperB.Update(gtx) {
		cv.Ink.Taper = a.taperB.Value
	} else {
		a.taperB.Value = cv.Ink.Taper
	}
	if a.snapB.Update(gtx) {
		cv.SnapShapes = a.snapB.Value
	} else {
		a.snapB.Value = cv.SnapShapes
	}

	// Selection bar.
	if a.selDelete.Clicked(gtx) {
		cv.DeleteSelection()
	}
	if a.selDup.Clicked(gtx) {
		cv.DuplicateSelection()
	}
	if a.selCopy.Clicked(gtx) {
		cv.Copy()
		a.notify("Copied — paste with ⌘V")
	}
	if a.selCrop.Clicked(gtx) {
		cv.StartCrop()
	}
	if a.cropDone.Clicked(gtx) {
		cv.ApplyCrop()
	}
	if a.cropCancel.Clicked(gtx) {
		cv.CancelCrop()
	}
	if a.selCut.Clicked(gtx) {
		cv.Cut()
	}
	for i := range a.selColors {
		if a.selColors[i].Clicked(gtx) {
			c := a.selPalette()[i]
			cv.RecolorSelection(c)
			a.addRecent(c)
		}
	}
	if a.selectAll.Clicked(gtx) {
		cv.SelectAll()
	}

	// Zoom & page navigation.
	if a.zoomIn.Clicked(gtx) {
		cv.ZoomBy(1.2)
	}
	if a.zoomOut.Clicked(gtx) {
		cv.ZoomBy(1 / 1.2)
	}
	if a.fit.Clicked(gtx) {
		cv.FitWidth()
	}
	if a.pagePrev.Clicked(gtx) {
		cv.ScrollToPage(cv.Current() - 1)
	}
	if a.pageNext.Clicked(gtx) {
		cv.ScrollToPage(cv.Current() + 1)
	}
	if a.addPage.Clicked(gtx) {
		a.insertPage(a.newPageLikeCurrent())
	}

	// Page management.
	cur := cv.Current()
	if a.pageUp.Clicked(gtx) && cur > 0 {
		a.hist.Do(a.doc, &document.MovePage{From: cur, To: cur - 1})
		cv.ScrollToPage(cur - 1)
	}
	if a.pageDown.Clicked(gtx) && cur+1 < len(a.doc.Pages) {
		a.hist.Do(a.doc, &document.MovePage{From: cur, To: cur + 1})
		cv.ScrollToPage(cur + 1)
	}
	if a.pageDup.Clicked(gtx) {
		a.insertPage(a.doc.Pages[cur].Clone())
	}
	if a.pageDel.Clicked(gtx) {
		if len(a.doc.Pages) > 1 {
			cv.ClearSelection()
			a.hist.Do(a.doc, &document.DeletePage{Index: cur})
			a.notify("Page deleted — ⌘Z to undo")
		} else {
			a.notify("A notebook needs at least one page")
		}
	}
	for i := range a.thumbClicks {
		if a.thumbClicks[i].Clicked(gtx) {
			cv.ScrollToPage(i)
		}
	}

	// Page setup.
	for i := range a.paperStyles {
		if a.paperStyles[i].Clicked(gtx) {
			s := document.Styles[i]
			a.applyFormat(func(f *document.PageFormat) {
				if f.Background.Type == "pixmap" { // a ruling replaces the picture
					f.Background.Type = "solid"
				}
				if f.Background.Type == "solid" {
					f.Background.Style = s
				}
			})
		}
	}
	for i := range a.paperSizes {
		if a.paperSizes[i].Clicked(gtx) {
			s := document.PaperSizes[i]
			a.setSize(s.W, s.H)
		}
	}
	for i := range a.orient {
		if a.orient[i].Clicked(gtx) {
			landscape := i == 1
			a.applyFormat(func(f *document.PageFormat) {
				if (f.W > f.H) != landscape {
					f.W, f.H = f.H, f.W
				}
			})
		}
	}
	for i := range a.scope {
		if a.scope[i].Clicked(gtx) {
			a.allPages = i == 1
		}
	}
	if a.closePages.Clicked(gtx) {
		a.showPages = false
	}
	if a.closeSetup.Clicked(gtx) {
		a.showSetup = false
	}
	if a.bgImageBtn.Clicked(gtx) {
		a.chooseBackgroundImage()
	}
	if a.bgRemoveBtn.Clicked(gtx) {
		a.applyFormat(func(f *document.PageFormat) {
			if f.Background.Type == "pixmap" {
				f.Background.Type = "solid"
			}
		})
	}
	// Soften: preview while dragging, one undo step when released.
	curPage := a.doc.Pages[cv.Current()]
	if a.softenF.Update(gtx) {
		a.softenDragging = true
	}
	if a.softenDragging && !a.softenF.Dragging() {
		a.softenDragging = false
		v := a.softenF.Value * 0.9
		a.applyFormat(func(f *document.PageFormat) {
			if f.Background.Type == "pixmap" {
				f.Background.Soften = v
			}
		})
	} else if !a.softenDragging {
		a.softenF.Value = curPage.Background.Soften / 0.9
	}
	if a.setDefault.Clicked(gtx) {
		p := a.doc.Pages[cv.Current()]
		c := p.Background.Color
		a.cfg.DefaultPage = settings.PageTemplate{W: p.Width, H: p.Height,
			Style: string(p.Background.StyleOr(document.StylePlain)), Color: [4]uint8{c.R, c.G, c.B, c.A}}
		if p.Background.Type == "pixmap" {
			a.cfg.DefaultPage.ImagePath, a.cfg.DefaultPage.Soften = p.Background.ImagePath, p.Background.Soften
		}
		settings.Save(a.collectSettings())
		a.notify("New notebooks will start with this page")
	}
	if a.resetDefault.Clicked(gtx) {
		a.cfg.DefaultPage = settings.PageTemplate{}
		settings.Save(a.collectSettings())
		a.notify("New notebooks: back to A4 lined white")
	}
	if a.customApply.Clicked(gtx) {
		w, err1 := strconv.ParseFloat(a.customW.Text(), 32)
		h, err2 := strconv.ParseFloat(a.customH.Text(), 32)
		if err1 != nil || err2 != nil || w < 10 || h < 10 || w > 5000 || h > 5000 {
			a.notify("Custom size must be 10–5000 mm")
		} else {
			w, h := document.MM(float32(w)), document.MM(float32(h))
			a.applyFormat(func(f *document.PageFormat) { f.W, f.H = w, h })
		}
	}
}
