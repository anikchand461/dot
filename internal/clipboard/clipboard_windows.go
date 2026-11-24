//go:build windows

package clipboard

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

const (
	cfUnicodeText = 13
	cfDIB         = 8
)

func changeCount() int64 {
	n, _, _ := procGetClipboardSequenceNumber.Call()
	return int64(n)
}

// read copies the clipboard data of format f.
func read(f uintptr) []byte {
	if r, _, _ := procIsClipboardFormatAvailable.Call(f); r == 0 {
		return nil
	}
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return nil
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(f)
	if h == 0 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	// p is a locked global-memory address owned by the clipboard, not a Go
	// pointer; reinterpret it without a uintptr→Pointer conversion.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	return append([]byte(nil), unsafe.Slice((*byte)(ptr), n)...)
}

func readImage() []byte {
	name, _ := windows.UTF16PtrFromString("PNG")
	if f, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name))); f != 0 {
		if b := read(f); len(b) > 0 {
			return b
		}
	}
	if img := decodeDIB(read(cfDIB)); img != nil {
		var buf bytes.Buffer
		if png.Encode(&buf, img) == nil {
			return buf.Bytes()
		}
	}
	return nil
}

// decodeDIB handles uncompressed 24/32-bit DIBs (what screenshots use).
func decodeDIB(b []byte) image.Image {
	if len(b) < 40 {
		return nil
	}
	le := binary.LittleEndian
	hdr := int(le.Uint32(b[0:]))
	w, h := int(int32(le.Uint32(b[4:]))), int(int32(le.Uint32(b[8:])))
	bpp, comp := int(le.Uint16(b[14:])), le.Uint32(b[16:])
	if (bpp != 24 && bpp != 32) || (comp != 0 && comp != 3) || w <= 0 || h == 0 {
		return nil
	}
	off := hdr
	if comp == 3 && hdr == 40 {
		off += 12 // colour masks follow a v1 header
	}
	topDown := h < 0
	if topDown {
		h = -h
	}
	stride := (w*bpp/8 + 3) &^ 3
	if len(b) < off+stride*h {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := b[off+y*stride:]
		dy := h - 1 - y
		if topDown {
			dy = y
		}
		for x := 0; x < w; x++ {
			px := row[x*bpp/8:]
			a := uint8(255)
			if bpp == 32 && hdr > 40 {
				a = px[3]
			}
			img.SetNRGBA(x, dy, color.NRGBA{px[2], px[1], px[0], a})
		}
	}
	return img
}

func readText() string {
	b := read(cfUnicodeText)
	if len(b) < 2 {
		return ""
	}
	u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
	return windows.UTF16ToString(u)
}
