package canvas

import "testing"

func TestParseFont(t *testing.T) {
	for attr, want := range map[string][3]any{
		"Sans":                {"Sans", false, false},
		"Serif Bold Italic":   {"Serif", true, true},
		"Shantell Sans":       {"Shantell Sans", false, false},
		"Caveat Bold":         {"Caveat", true, false},
		"Patrick Hand Italic": {"Patrick Hand", false, true},
		"monospace":           {"Mono", false, false},
		"Comic Neue":          {"Sans", false, false},
	} {
		f, b, i := ParseFont(attr)
		if f != want[0] || b != want[1] || i != want[2] {
			t.Errorf("%q: got %q %v %v", attr, f, b, i)
		}
	}
	for _, f := range Families {
		if f.Regular != "" && FontFile(f.Name, false) == nil {
			t.Errorf("%s: font file missing", f.Name)
		}
	}
	if len(collection()) < 20 {
		t.Errorf("fonts failed to load: %d faces", len(collection()))
	}
}
