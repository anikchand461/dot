//go:build darwin && cgo

package input

// This file only exports Go callbacks to the AppKit event monitor in
// native_darwin.go (cgo forbids C definitions in a file with //export).

// #include <stdint.h>
import "C"

//export dotMagnify
func dotMagnify(view C.uintptr_t, m C.double) { pushGesture(uintptr(view), float32(m), false) }

//export dotSmartMagnify
func dotSmartMagnify(view C.uintptr_t) { pushGesture(uintptr(view), 0, true) }
