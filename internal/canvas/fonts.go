package canvas

import (
	"embed"
	"strings"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"github.com/go-fonts/liberation/liberationserifbold"
	"github.com/go-fonts/liberation/liberationserifbolditalic"
	"github.com/go-fonts/liberation/liberationserifitalic"
	"github.com/go-fonts/liberation/liberationserifregular"
)

// Handwriting fonts, all SIL Open Font License (see fonts/OFL-*.txt).
//
//go:embed fonts/*.ttf
var fontFiles embed.FS

// Family is a text font family offered in dot!.
type Family struct {
	Name     string        // shown in the menu and written to .xopp (Pango-style, as Xournal++ uses)
	Typeface font.Typeface // face name in the shaper
	Hand     bool          // a handwriting font
	// Embedded font files (handwriting fonts); empty for built-ins.
	Regular, Bold string
	// Hint shown under the name in the font menu.
	Hint string
}

// Families, in menu order. "Sans", "Serif" and "Mono" are built in; the
// rest are handwriting fonts like Excalidraw's and tldraw's.
var Families = []Family{
	{Name: "Sans", Typeface: "Go", Hint: "Clean"},
	{Name: "Serif", Typeface: serifFace, Hint: "Classic"},
	{Name: "Mono", Typeface: "Go Mono", Hint: "Code"},
	{Name: "Shantell Sans", Typeface: "Shantell Sans", Hand: true, Regular: "ShantellSans.ttf", Hint: "tldraw style"},
	{Name: "Caveat", Typeface: "Caveat", Hand: true, Regular: "Caveat.ttf", Hint: "Quick notes"},
	{Name: "Kalam", Typeface: "Kalam", Hand: true, Regular: "Kalam-Regular.ttf", Bold: "Kalam-Bold.ttf", Hint: "Neat handwriting"},
	{Name: "Patrick Hand", Typeface: "Patrick Hand", Hand: true, Regular: "PatrickHand.ttf", Hint: "Whiteboard"},
	{Name: "Indie Flower", Typeface: "Indie Flower", Hand: true, Regular: "IndieFlower.ttf", Hint: "Playful"},
}

// FontFamilies lists family names in menu order.
var FontFamilies = func() []string {
	var n []string
	for _, f := range Families {
		n = append(n, f.Name)
	}
	return n
}()

const serifFace = "dot Serif"

func familyByName(name string) (Family, bool) {
	for _, f := range Families {
		if f.Name == name {
			return f, true
		}
	}
	return Family{}, false
}

// FontFile returns the embedded TrueType data for a family's style, or nil
// for built-in families.
func FontFile(family string, bold bool) []byte {
	f, ok := familyByName(family)
	if !ok || f.Regular == "" {
		return nil
	}
	name := f.Regular
	if bold && f.Bold != "" {
		name = f.Bold
	}
	data, _ := fontFiles.ReadFile("fonts/" + name)
	return data
}

// collection is the Go fonts (Sans, Mono), Liberation Serif and the
// handwriting fonts.
func collection() []font.FontFace {
	faces := gofont.Collection()
	add := func(ttf []byte, fnt font.Font) {
		if face, err := opentype.Parse(ttf); err == nil {
			faces = append(faces, font.FontFace{Font: fnt, Face: face})
		}
	}
	for _, f := range []struct {
		ttf          []byte
		bold, italic bool
	}{
		{liberationserifregular.TTF, false, false}, {liberationserifbold.TTF, true, false},
		{liberationserifitalic.TTF, false, true}, {liberationserifbolditalic.TTF, true, true},
	} {
		add(f.ttf, GioFont("Serif", f.bold, f.italic))
	}
	for _, f := range Families {
		if f.Regular == "" {
			continue
		}
		add(FontFile(f.Name, false), font.Font{Typeface: f.Typeface})
		if f.Bold != "" {
			add(FontFile(f.Name, true), font.Font{Typeface: f.Typeface, Weight: font.Bold})
		}
	}
	return faces
}

// ParseFont reads a Xournal++ font attribute ("Caveat Bold", "Serif
// Italic", ...). The longest known family name at the start wins.
func ParseFont(attr string) (family string, bold, italic bool) {
	family = "Sans"
	rest := attr
	best := 0
	for _, f := range Families {
		if strings.HasPrefix(strings.ToLower(attr), strings.ToLower(f.Name)) && len(f.Name) > best {
			family, best = f.Name, len(f.Name)
		}
	}
	rest = attr[best:]
	for _, w := range strings.Fields(rest) {
		switch strings.ToLower(w) {
		case "serif", "times", "georgia":
			if best == 0 {
				family = "Serif"
			}
		case "mono", "monospace", "courier":
			if best == 0 {
				family = "Mono"
			}
		case "bold", "semibold", "heavy":
			bold = true
		case "italic", "oblique":
			italic = true
		}
	}
	return family, bold, italic
}

// FontAttr builds the Xournal++ font attribute.
func FontAttr(family string, bold, italic bool) string {
	s := family
	if bold {
		s += " Bold"
	}
	if italic {
		s += " Italic"
	}
	return s
}

// IsHandwriting reports whether a family is one of the handwriting fonts.
func IsHandwriting(family string) bool {
	f, _ := familyByName(family)
	return f.Hand
}

// GioFont maps a family and style to a face in the shaper's collection.
func GioFont(family string, bold, italic bool) font.Font {
	f := font.Font{Typeface: "Go"}
	if fam, ok := familyByName(family); ok {
		f.Typeface = fam.Typeface
	}
	if bold {
		f.Weight = font.Bold
	}
	if italic {
		f.Style = font.Italic
	}
	return f
}
