package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/widget/material"

	"dot/internal/canvas"
	"dot/internal/document"
	"dot/internal/export"
	"dot/internal/input"
	"dot/internal/settings"
)

// Annotate PDF from Home must leave Home and create the notebook in the
// save folder at once, before anything is drawn.
func TestAnnotatePDFFromHome(t *testing.T) {
	src, notesDir := t.TempDir(), t.TempDir()
	pdfPath := filepath.Join(src, "Lecture.pdf")
	f, err := os.Create(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := export.FinalPDF(f, document.New()); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// An unrelated notebook with the same name must not be overwritten.
	clash := filepath.Join(notesDir, "Lecture.dott")
	os.WriteFile(clash, []byte("keep me"), 0o644)

	th := material.NewTheme()
	th.Shaper = canvas.Shaper()
	doc := document.New()
	a := &App{th: th, doc: doc, cv: canvas.New(doc, &document.History{}, input.NewProvider(), th),
		w: new(app.Window), results: make(chan func(), 8), home: true,
		cfg: settings.Settings{SaveDir: notesDir}}
	a.openPDF(pdfPath)

	want := filepath.Join(notesDir, "Lecture 2.dott")
	for deadline := time.Now().Add(5 * time.Second); ; {
		a.drainResults()
		if _, err := os.Stat(want); err == nil && !a.home && !a.saving {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("home=%v saving=%v path=%q; notebook not written", a.home, a.saving, a.path)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if b, _ := os.ReadFile(clash); string(b) != "keep me" {
		t.Error("overwrote an unrelated notebook")
	}
	d, err := document.OpenFile(want)
	if err != nil || d.Pages[0].Background.Type != "pdf" {
		t.Fatalf("saved notebook doesn't annotate the PDF: %v", err)
	}
}
