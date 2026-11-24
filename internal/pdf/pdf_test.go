package pdf

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PDF_FILE=/path/x.pdf PDF_OUT=/tmp/dir go test ./internal/pdf -v
func TestRenderAndMerge(t *testing.T) {
	path := os.Getenv("PDF_FILE")
	if path == "" {
		t.Skip("PDF_FILE not set")
	}
	start := time.Now()
	sizes, err := PageSizes(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pages, first %v pt (engine start + open %v)", len(sizes), sizes[0], time.Since(start))
	start = time.Now()
	img, err := Render(path, 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rendered %v in %v", img.Bounds().Size(), time.Since(start))
	if out := os.Getenv("PDF_OUT"); out != "" {
		f, _ := os.Create(filepath.Join(out, "render.png"))
		png.Encode(f, img)
		f.Close()
	}
	// Merge: overlay the same PDF (as "annotations") on itself, page 0.
	annot, _ := os.ReadFile(path)
	var buf bytes.Buffer
	ov := Overlay{Lines: []Line{{Points: [][2]float32{{50, 50}, {200, 120}}, Width: 3}}}
	if err := Merge(&buf, annot, []Source{{Path: path, Page: 0}}, []Overlay{ov}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Fatal("merge did not produce a PDF")
	}
}
