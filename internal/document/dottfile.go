package document

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The .dott format is dot!'s own file: one self-contained zip. (Files
// from before the rename use .ink; they are the same format and still open.)
//
//	content.xml   the notebook, in the same XML as .xopp (Xournal++)
//	assets/…      background images and annotated PDFs, so nothing breaks
//	              when the file is moved, mailed or synced
//	preview.png   the first page, so Home can show it instantly
//	meta.json     {"app":"dot!","format":1,"pages":N}
//
// .xopp stays fully supported for Xournal++; dot! reads and writes both.

const inkFormat = 1

type inkMeta struct {
	App    string `json:"app"`
	Format int    `json:"format"`
	Pages  int    `json:"pages"`
}

// IsDott reports whether path is a dot! notebook (.dott, or the older .ink).
func IsDott(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, ".dott") || strings.EqualFold(ext, ".ink")
}

// SaveDott writes d as a .dott zip. preview (PNG bytes) is optional.
func SaveDott(w io.Writer, d *Document, preview []byte) error {
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestSpeed)
	})
	// Embed every background file once and point the pages at the copy.
	assets := map[string]string{} // absolute path -> zip name
	pages := make([]*Page, len(d.Pages))
	for i, p := range d.Pages {
		cp := *p
		b := &cp.Background
		var src *string
		switch {
		case b.Type == "pdf" && b.PDFPath != "":
			src = &b.PDFPath
		case b.Type == "pixmap" && b.ImagePath != "":
			src = &b.ImagePath
		}
		if src != nil {
			name, ok := assets[*src]
			if !ok {
				name = fmt.Sprintf("assets/%d-%s", len(assets)+1, filepath.Base(*src))
				if err := copyInto(zw, name, *src); err != nil {
					// Missing file: keep the original path rather than fail the save.
					name = *src
				}
				assets[*src] = name
			}
			*src = name
		}
		pages[i] = &cp
	}
	cw, err := zw.Create("content.xml")
	if err != nil {
		return err
	}
	if err := Encode(cw, &Document{Pages: pages}); err != nil {
		return err
	}
	if len(preview) > 0 {
		pw, err := zw.CreateHeader(&zip.FileHeader{Name: "preview.png", Method: zip.Store})
		if err != nil {
			return err
		}
		pw.Write(preview)
	}
	mw, err := zw.Create("meta.json")
	if err != nil {
		return err
	}
	json.NewEncoder(mw).Encode(inkMeta{App: "dot!", Format: inkFormat, Pages: len(d.Pages)})
	return zw.Close()
}

func copyInto(zw *zip.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// LoadDott reads a .dott file. Embedded assets are unpacked into a cache
// folder (keyed by the file's path) and the pages point there.
func LoadDott(data []byte, filePath string) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not an dot! file: %w", err)
	}
	var doc *Document
	for _, f := range zr.File {
		if f.Name != "content.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		doc, err = Load(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	if doc == nil {
		return nil, fmt.Errorf("not an dot! file: no content")
	}
	dir := assetDir(filePath)
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "assets/") || strings.Contains(f.Name, "..") {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := extract(f, dst); err != nil {
			return nil, err
		}
	}
	for _, p := range doc.Pages {
		b := &p.Background
		for _, s := range []*string{&b.PDFPath, &b.ImagePath} {
			if strings.HasPrefix(*s, "assets/") {
				*s = filepath.Join(dir, filepath.FromSlash(path.Clean(*s)))
			}
		}
	}
	return doc, nil
}

func extract(f *zip.File, dst string) error {
	if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(f.UncompressedSize64) {
		return nil // already unpacked
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func assetDir(filePath string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	h := sha1.Sum([]byte(filePath))
	return filepath.Join(base, "dot", "files", hex.EncodeToString(h[:8]))
}

// DottPreview returns a .dott file's preview image and page count without
// reading the whole notebook (for Home).
func DottPreview(filePath string) (png []byte, pages int, err error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		switch f.Name {
		case "preview.png":
			rc, err := f.Open()
			if err != nil {
				return nil, 0, err
			}
			png, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, 0, err
			}
		case "meta.json":
			rc, err := f.Open()
			if err != nil {
				continue
			}
			var m inkMeta
			json.NewDecoder(rc).Decode(&m)
			rc.Close()
			pages = m.Pages
		}
	}
	return png, pages, nil
}

// OpenFile loads a notebook by path: .dott (or .ink), .xopp or .xoj.
func OpenFile(filePath string) (*Document, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if IsDott(filePath) || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return LoadDott(data, filePath)
	}
	d, err := Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	d.ResolvePDFs(filePath)
	return d, nil
}
