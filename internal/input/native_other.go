//go:build !windows && !(darwin && cgo)

package input

import (
	"time"

	"gioui.org/app"
)

// noNative is used where no stylus hook exists; the velocity model takes over.
type noNative struct{}

func newNative() Native { return noNative{} }

func (noNative) Name() string                     { return "none" }
func (noNative) Attach(app.ViewEvent)             {}
func (noNative) At(time.Duration) (Reading, bool) { return Reading{}, false }
