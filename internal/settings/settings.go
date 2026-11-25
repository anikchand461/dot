// Package settings persists user preferences as JSON in the OS config
// directory (~/Library/Application Support/dot on macOS,
// %AppData%\dot on Windows). Folders are named "dot" (no "!": it is
// awkward in shells).
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

type Settings struct {
	PenColor         [4]uint8
	PenWidth         float32
	PenStyle         int
	Sensitivity      float32
	Smoothing        float32
	Taper            bool
	SnapShapes       bool
	HighlighterColor [4]uint8
	HighlighterWidth float32
	EraserRadius     float32
	EraserMode       int
	ShapeKind        int
	TextSize         float32
	TextFont         string
	TextBold         bool
	TextItalic       bool
	LineStyle        string
	FillColor        [4]uint8
	ShowPages        bool
	Recent           []string
	RecentColors     [][4]uint8
	DefaultPage      PageTemplate
	// SaveDir is where untitled notebooks and PDF notes are saved;
	// empty means ~/Documents/dot.
	SaveDir string
	// NewFormat is the file type for new notebooks: "dott" (default) or "xopp".
	NewFormat string
	// TourDone is set once the first-run tour is finished or skipped.
	TourDone bool
}

// PageTemplate is the page new notebooks start with. Zero means A4 lined white.
type PageTemplate struct {
	W, H  float32
	Style string
	Color [4]uint8
	// Image backgrounds: the picture and how much it is softened.
	ImagePath string
	Soften    float32
}

func Default() Settings {
	return Settings{
		PenColor: [4]uint8{0x10, 0x10, 0x10, 0xff}, PenWidth: 1.41,
		Sensitivity: 0.6, Smoothing: 0.45, Taper: true, SnapShapes: true,
		HighlighterColor: [4]uint8{0xff, 0xe0, 0x00, 0x7f}, HighlighterWidth: 8.5,
		EraserRadius: 6, ShapeKind: 3, TextSize: 14, ShowPages: false,
	}
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dot", "settings.json"), nil
}

// Load returns saved settings, or defaults if there are none. Settings
// and notebooks from the app's previous name (ink++) carry over.
func Load() Settings {
	s := Default()
	p, err := path()
	if err != nil {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil { // first run as dot!: pick up ink++ settings
		b, err = os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(p)), "ink++", "settings.json"))
	}
	if err == nil {
		json.Unmarshal(b, &s)
	}
	if s.SaveDir == "" { // keep using the old notebook folder if it exists
		if home, err := os.UserHomeDir(); err == nil {
			old := filepath.Join(home, "Documents", "ink++")
			if fi, err := os.Stat(old); err == nil && fi.IsDir() {
				s.SaveDir = old
			}
		}
	}
	return s
}

func Save(s Settings) error {
	p, err := path()
	if err != nil {
		return err
	}
	if !s.TourDone { // once any window finished the tour, it stays done
		s.TourDone = Load().TourDone
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// AddRecent moves path to the front of the recent-files list.
func (s *Settings) AddRecent(p string) {
	if p == "" {
		return
	}
	s.Recent = slices.DeleteFunc(s.Recent, func(q string) bool { return q == p })
	s.Recent = append([]string{p}, s.Recent...)
	if len(s.Recent) > 8 {
		s.Recent = s.Recent[:8]
	}
}

// Dir is the save folder: the one the user chose, else DefaultDir.
func (s Settings) Dir() string {
	if s.SaveDir != "" {
		return s.SaveDir
	}
	return DefaultDir()
}

// DefaultDir is the built-in save folder, ~/Documents/dot.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Documents", "dot")
}
