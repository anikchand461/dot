package ui

import (
	"bytes"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/op/paint"
	"gioui.org/widget"

	"dot/internal/document"
	"dot/internal/export"
	"dot/internal/settings"
	"dot/internal/trash"
)

// noteInfo is one notebook card on the Home page.
type noteInfo struct {
	path, name string
	mod        time.Time
	pages      int
	pdf        bool
	thumb      paint.ImageOp
	thumbSize  image.Point
	loaded     bool
	failed     bool
	click      widget.Clickable

	renameBtn, deleteBtn widget.Clickable
}

type homeState struct {
	notes  []*noteInfo
	cache  map[string]*noteInfo // by path; keeps thumbnails across visits
	gen    atomic.Int64         // bumps per scan, stops stale loaders
	search widget.Editor
	list   widget.List

	newBtn, pdfBtn, openBtn, folderBtn, backBtn widget.Clickable
	githubBtn                                   widget.Clickable
	fmtInk, fmtXopp                             widget.Clickable
	renaming                                    *noteInfo
	renameEd                                    widget.Editor
	renameFocus                                 bool
	confirmDelete                               *noteInfo
	confirmYes, confirmNo, dimmer               widget.Clickable
	sortRecent, sortName                        widget.Clickable
	byName                                      bool
}

// goHome shows the Home page (saving the open note first).
func (a *App) goHome() {
	if a.doc.Dirty && !isEmpty(a.doc) {
		a.saveNow(a.savePath())
	}
	a.home = true
	a.showMenu, a.showStyle, a.showSetup = false, false, false
	a.scanNotes()
}

// leaveHome returns to the open note (if there is one worth returning to).
func (a *App) leaveHome() { a.home = false }

func isEmpty(d *document.Document) bool {
	for _, p := range d.Pages {
		for _, l := range p.Layers {
			if len(l.Items) > 0 {
				return false
			}
		}
	}
	return len(d.Pages) <= 1
}

// scanNotes lists the notebooks in the save folder (and one level of
// sub-folders), newest first, then fills in page counts and thumbnails
// in the background.
func (a *App) scanNotes() {
	h := &a.homeUI
	if h.cache == nil {
		h.cache = map[string]*noteInfo{}
	}
	gen := h.gen.Add(1)
	dir := a.cfg.Dir()
	a.async(func() func() {
		type found struct {
			path string
			mod  time.Time
		}
		var files []found
		root := filepath.Clean(dir)
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root && (strings.HasPrefix(d.Name(), ".") || strings.Count(strings.TrimPrefix(p, root), string(filepath.Separator)) > 2) {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := strings.ToLower(filepath.Ext(p)); ext == ".dott" || ext == ".ink" || ext == ".xopp" || ext == ".xoj" {
				if info, err := d.Info(); err == nil {
					files = append(files, found{p, info.ModTime()})
				}
			}
			return nil
		})
		sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
		return func() {
			if gen != h.gen.Load() {
				return
			}
			var notes, todo []*noteInfo
			for _, f := range files {
				n := h.cache[f.path]
				if n == nil || !n.mod.Equal(f.mod) {
					n = &noteInfo{path: f.path, mod: f.mod,
						name: strings.TrimSuffix(filepath.Base(f.path), filepath.Ext(f.path))}
					h.cache[f.path] = n
				}
				if !n.loaded {
					todo = append(todo, n)
				}
				notes = append(notes, n)
			}
			h.notes = notes
			a.loadThumbs(gen, todo)
		}
	})
}

// loadThumbs reads each notebook and renders its first page, one at a
// time, updating the cards as they become ready.
func (a *App) loadThumbs(gen int64, todo []*noteInfo) {
	if len(todo) == 0 {
		return
	}
	go func() {
		for _, n := range todo {
			if gen != a.homeUI.gen.Load() {
				return
			}
			n := n
			if document.IsDott(n.path) {
				if png, pages, err := document.DottPreview(n.path); err == nil && len(png) > 0 {
					if img, _, err := image.Decode(bytes.NewReader(png)); err == nil {
						a.results <- func() {
							n.loaded, n.pages = true, pages
							n.thumb, n.thumbSize = paint.NewImageOp(img), img.Bounds().Size()
						}
						a.w.Invalidate()
						continue
					}
				}
			}
			d, err := document.OpenFile(n.path)
			if err != nil {
				a.results <- func() { n.loaded, n.failed = true, true }
				a.w.Invalidate()
				continue
			}
			p := d.Pages[0]
			img, err := export.Thumbnail(p, 300, 420)
			a.results <- func() {
				n.loaded, n.pages = true, len(d.Pages)
				n.pdf = p.Background.Type == "pdf"
				if err == nil {
					n.thumb, n.thumbSize = paint.NewImageOp(img), img.Bounds().Size()
				} else {
					n.failed = true
				}
			}
			a.w.Invalidate()
		}
	}()
}

func (a *App) homeClicks(gtx C) {
	h := &a.homeUI
	if h.newBtn.Clicked(gtx) {
		a.setDocument(newFromDefaults(a.cfg), "")
		a.home = false
		a.notify("New notebook")
	}
	if h.pdfBtn.Clicked(gtx) {
		a.choosePDF()
	}
	if h.openBtn.Clicked(gtx) {
		a.doOpen()
	}
	if h.folderBtn.Clicked(gtx) {
		a.chooseSaveFolder()
	}
	if h.backBtn.Clicked(gtx) {
		a.leaveHome()
	}
	if h.githubBtn.Clicked(gtx) {
		openExternal(RepoURL)
	}
	if h.fmtInk.Clicked(gtx) {
		a.cfg.NewFormat = "dott"
		settings.Save(a.collectSettings())
	}
	if h.fmtXopp.Clicked(gtx) {
		a.cfg.NewFormat = "xopp"
		settings.Save(a.collectSettings())
	}
	if h.sortRecent.Clicked(gtx) {
		h.byName = false
	}
	if h.sortName.Clicked(gtx) {
		h.byName = true
	}
	if h.confirmNo.Clicked(gtx) || h.dimmer.Clicked(gtx) {
		h.confirmDelete = nil
	}
	if h.confirmYes.Clicked(gtx) && h.confirmDelete != nil {
		a.trashNote(h.confirmDelete)
		h.confirmDelete = nil
	}
	if h.renaming != nil {
		for {
			ev, ok := h.renameEd.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				a.renameNote(h.renaming, h.renameEd.Text())
				h.renaming = nil
				break
			}
		}
	}
	for _, n := range h.notes {
		if n.renameBtn.Clicked(gtx) {
			h.renaming, h.renameFocus = n, true
			h.renameEd.SetText(n.name)
			h.renameEd.SetCaret(0, len([]rune(n.name))) // select all: just type
		}
		if n.deleteBtn.Clicked(gtx) {
			h.confirmDelete = n
		}
	}
	for _, n := range h.notes {
		if n.click.Clicked(gtx) {
			if h.renaming != nil {
				h.renaming = nil // clicking elsewhere cancels a rename
				continue
			}
			if n.path == a.path {
				a.leaveHome()
				continue
			}
			a.openAsync(n.path, nil)
		}
	}
}

// ago formats a time like "5 min ago", "Yesterday", "24 Nov".
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour && t.Day() == time.Now().Day():
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour && t.Day() == time.Now().AddDate(0, 0, -1).Day():
		return "yesterday"
	case t.Year() == time.Now().Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// renameNote renames the notebook file on disk (keeping its extension).
func (a *App) renameNote(n *noteInfo, name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == n.name {
		return
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		a.notify(`A name can't contain / \ : * ? " < > |`)
		return
	}
	ext := filepath.Ext(n.path)
	dst := filepath.Join(filepath.Dir(n.path), name+ext)
	if _, err := os.Stat(dst); err == nil && !strings.EqualFold(dst, n.path) {
		a.notify("A notebook called “" + name + "” already exists")
		return
	}
	if err := os.Rename(n.path, dst); err != nil {
		a.notify("Couldn't rename: " + err.Error())
		return
	}
	// Xournal++ keeps an attached PDF beside the file: <name>.xopp.bg.pdf
	if _, err := os.Stat(n.path + ".bg.pdf"); err == nil {
		os.Rename(n.path+".bg.pdf", dst+".bg.pdf")
	}
	old := n.path
	delete(a.homeUI.cache, old)
	n.path, n.name = dst, name
	a.homeUI.cache[dst] = n
	if a.path == old {
		a.path = dst
	}
	for i, r := range a.cfg.Recent {
		if r == old {
			a.cfg.Recent[i] = dst
		}
	}
	settings.Save(a.collectSettings())
	a.notify("Renamed to " + name)
}

// trashNote moves a notebook to the system Trash.
func (a *App) trashNote(n *noteInfo) {
	if err := trash.Move(n.path); err != nil {
		a.notify("Couldn't move to Trash: " + err.Error())
		return
	}
	if _, err := os.Stat(n.path + ".bg.pdf"); err == nil {
		trash.Move(n.path + ".bg.pdf")
	}
	h := &a.homeUI
	delete(h.cache, n.path)
	for i, m := range h.notes {
		if m == n {
			h.notes = append(h.notes[:i], h.notes[i+1:]...)
			break
		}
	}
	if a.path == n.path { // it was open: don't let autosave bring it back
		a.setDocument(newFromDefaults(a.cfg), "")
	}
	var recent []string
	for _, r := range a.cfg.Recent {
		if r != n.path {
			recent = append(recent, r)
		}
	}
	a.cfg.Recent = recent
	settings.Save(a.collectSettings())
	a.notify("Moved “" + n.name + "” to the Trash")
}
