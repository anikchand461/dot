package export

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"

	"dot/internal/document"
)

func sample() *document.Document {
	d := document.New()
	p := d.Pages[0]
	l := p.TopLayer()
	l.Items = append(l.Items,
		&document.Stroke{Tool: document.ToolPen, Color: document.Black, Width: 2,
			Points: []f32.Point{f32.Pt(80, 120), f32.Pt(200, 160), f32.Pt(320, 120)}, Widths: []float32{0.5, 3, 0.5}},
		&document.Stroke{Tool: document.ToolHighlighter, Color: document.White, Width: 10,
			Points: []f32.Point{f32.Pt(80, 200), f32.Pt(320, 200)}},
		document.NewText(f32.Pt(80, 240), 18, document.Black, "Hello (dot!) é"),
	)
	return d
}

// EXPORT_DIR=/tmp/x go test ./internal/export writes files to inspect.
func TestPDF(t *testing.T) {
	var buf bytes.Buffer
	d := sample()
	if path := os.Getenv("XOPP_FILE"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if d, err = document.Load(f); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := PDF(&buf, d); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-1.4")) || !bytes.Contains(buf.Bytes(), []byte("%%EOF")) {
		t.Fatal("not a PDF")
	}
	if dir := os.Getenv("EXPORT_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "out.pdf"), buf.Bytes(), 0o644)
	}
}

func TestPNG(t *testing.T) {
	var buf bytes.Buffer
	if err := PNG(&buf, sample().Pages[0], 1); err != nil {
		t.Skip("no headless GPU:", err)
	}
	if dir := os.Getenv("EXPORT_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "out.png"), buf.Bytes(), 0o644)
	}
}
