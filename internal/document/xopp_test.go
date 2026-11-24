package document

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
)

func TestRoundTrip(t *testing.T) {
	d := New()
	p := d.Pages[0]
	p.Background.Style = StyleGraph
	l := p.TopLayer()
	l.Items = append(l.Items,
		&Stroke{Tool: ToolPen, Color: Black, Width: 2.26,
			Points: []f32.Point{{X: 10, Y: 20}, {X: 11.5, Y: 21}, {X: 13, Y: 22.25}},
			Widths: []float32{0.7, 1.1, 1.3}},
		&Stroke{Tool: ToolHighlighter, Color: White, Width: 8.5,
			Points: []f32.Point{{X: 30, Y: 40}, {X: 35, Y: 45}}},
		&RawElement{Name: "text", Attrs: nil, Inner: "a &amp; b"},
	)

	var buf bytes.Buffer
	if err := Save(&buf, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	gp := got.Pages[0]
	if gp.Width != p.Width || gp.Background.Style != StyleGraph {
		t.Fatalf("page mismatch: %+v", gp)
	}
	items := gp.Layers[0].Items
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	s := items[0].(*Stroke)
	if len(s.Points) != 3 || s.Width != 2.26 || s.Widths[0] != 0.7 || s.Widths[1] != 1.1 {
		t.Fatalf("pressure stroke mismatch: %+v", s)
	}
	if h := items[1].(*Stroke); h.Tool != ToolHighlighter || h.Widths != nil {
		t.Fatalf("highlighter mismatch: %+v", h)
	}
	if r := items[2].(*RawElement); r.Inner != "a &amp; b" {
		t.Fatalf("raw element mismatch: %q", r.Inner)
	}
}

// XOPP_FILE=/path/to/real.xopp go test ./internal/document -run Real
// loads a real Xournal++ file and checks that re-saving keeps every element.
func TestRealFile(t *testing.T) {
	path := os.Getenv("XOPP_FILE")
	if path == "" {
		t.Skip("XOPP_FILE not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	count := func(d *Document) (strokes, raw int) {
		for _, p := range d.Pages {
			for _, l := range p.Layers {
				for _, it := range l.Items {
					if _, ok := it.(*Stroke); ok {
						strokes++
					} else {
						raw++
					}
				}
			}
		}
		return
	}
	s1, r1 := count(d)
	var buf bytes.Buffer
	if err := Save(&buf, d); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("XOPP_OUT"); out != "" {
		os.WriteFile(out, buf.Bytes(), 0o644)
	}
	d2, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	s2, r2 := count(d2)
	t.Logf("%d pages, %d strokes, %d other elements", len(d.Pages), s1, r1)
	if len(d2.Pages) != len(d.Pages) || s1 != s2 || r1 != r2 {
		t.Fatalf("round trip lost data: pages %d->%d strokes %d->%d raw %d->%d", len(d.Pages), len(d2.Pages), s1, s2, r1, r2)
	}
}

func TestPDFBackgroundRoundTrip(t *testing.T) {
	d := NewPDFDocument("/tmp/paper.pdf", [][2]float32{{612, 792}, {612, 792}, {792, 612}})
	var buf bytes.Buffer
	if err := Save(&buf, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range got.Pages {
		if p.Background.Type != "pdf" || p.Background.PDFPath != "/tmp/paper.pdf" || p.Background.PDFPage != i {
			t.Fatalf("page %d: %+v", i, p.Background)
		}
	}
}

func TestFillAndStyleRoundTrip(t *testing.T) {
	d := New()
	s := &Stroke{Tool: ToolPen, Color: Black, Width: 2, Style: "dash",
		Fill:   color.NRGBA{0x20, 0x90, 0xff, 0xc0},
		Points: []f32.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 50}, {X: 0, Y: 0}}}
	d.Pages[0].TopLayer().Items = append(d.Pages[0].TopLayer().Items, s)
	var buf bytes.Buffer
	Save(&buf, d)
	got, _ := Load(&buf)
	g := got.Pages[0].Layers[0].Items[0].(*Stroke)
	if g.Fill != s.Fill || g.Style != "dash" {
		t.Fatalf("fill/style lost: %+v %q", g.Fill, g.Style)
	}
	if n := len(g.Pieces()); n < 10 {
		t.Fatalf("dashed stroke should split into many pieces, got %d", n)
	}
	g.Style = "dot"
	dots := 0
	for _, p := range g.Pieces() {
		if p.Dot {
			dots++
		}
	}
	if dots < 10 {
		t.Fatalf("dotted stroke should produce dots, got %d", dots)
	}
}

func TestDottRoundTripEmbedsAssets(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "photo.png")
	os.WriteFile(img, []byte("\x89PNG fake"), 0o644)
	d := New()
	d.Pages[0].Background = Background{Type: "pixmap", ImagePath: img, Soften: 0.3, Color: White}
	d.Pages[0].TopLayer().Items = append(d.Pages[0].TopLayer().Items,
		&Stroke{Tool: ToolPen, Color: Black, Width: 1, Points: []f32.Point{{X: 1, Y: 1}, {X: 9, Y: 9}}})
	var buf bytes.Buffer
	if err := SaveDott(&buf, d, []byte("preview")); err != nil {
		t.Fatal(err)
	}
	inkPath := filepath.Join(dir, "note.dott")
	os.WriteFile(inkPath, buf.Bytes(), 0o644)
	os.Remove(img) // the .ink must not need the original any more
	got, err := OpenFile(inkPath)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Pages[0].Background
	if b.Type != "pixmap" || b.Soften != 0.3 {
		t.Fatalf("background lost: %+v", b)
	}
	if data, err := os.ReadFile(b.ImagePath); err != nil || string(data) != "\x89PNG fake" {
		t.Fatalf("embedded image not restored at %s: %v", b.ImagePath, err)
	}
	if len(got.Pages[0].Layers[0].Items) != 1 {
		t.Fatal("ink lost")
	}
	png, pages, err := DottPreview(inkPath)
	if err != nil || string(png) != "preview" || pages != 1 {
		t.Fatalf("preview: %q %d %v", png, pages, err)
	}
}
