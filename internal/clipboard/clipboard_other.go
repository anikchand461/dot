//go:build !windows && !linux && !(darwin && cgo)

package clipboard

func changeCount() int64 { return 0 }
func readImage() []byte  { return nil }
func readText() string   { return "" }
