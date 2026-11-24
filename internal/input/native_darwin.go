//go:build darwin && cgo

package input

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include <os/lock.h>

typedef struct {
	double pressure, tiltX, tiltY, timestamp;
} np_reading;

#define NP_RING 256
static os_unfair_lock np_lock = OS_UNFAIR_LOCK_INIT;
static np_reading np_ring[NP_RING];
static int np_count;
static id np_monitor;

// Implemented in Go (gesture_darwin.go).
extern void dotMagnify(uintptr_t view, double m);
extern void dotSmartMagnify(uintptr_t view);

// A local monitor sees every tablet event before AppKit dispatches it to
// Gio's view, so the reading is current when Gio's pointer event arrives.
static void np_install(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (np_monitor != nil) return;
		NSEventMask mask = NSEventMaskLeftMouseDown | NSEventMaskLeftMouseDragged |
			NSEventMaskLeftMouseUp | NSEventMaskTabletPoint;
		static int dbg = -1;
		if (dbg < 0) dbg = getenv("DOT_DEBUG") != NULL;
		np_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *e) {
			if (dbg && e.type != NSEventTypeLeftMouseDragged) NSLog(@"dot monitor: type=%ld", (long)e.type);
			if (e.type == NSEventTypeTabletPoint || e.subtype == NSEventSubtypeTabletPoint) {
				np_reading r = {e.pressure, e.tilt.x, e.tilt.y, e.timestamp};
				os_unfair_lock_lock(&np_lock);
				np_ring[np_count % NP_RING] = r;
				np_count++;
				os_unfair_lock_unlock(&np_lock);
			}
			return e;
		}];
	});
}

// Pinch: AppKit only streams a magnify gesture to a view that implements
// -magnifyWithEvent:, and Gio's view doesn't. Add the methods to its class
// at runtime; they forward to Go. (A monitor alone sees only the first
// event of each pinch.)
// self is the view the gesture happened in: each window gets its own.
static void np_magnify(id self, SEL _cmd, NSEvent *e) { dotMagnify((uintptr_t)(__bridge void *)self, e.magnification); }
static void np_smart(id self, SEL _cmd, NSEvent *e) { dotSmartMagnify((uintptr_t)(__bridge void *)self); }

static void np_install_gestures(CFTypeRef view) {
	Class cls = object_getClass((__bridge id)view);
	class_addMethod(cls, @selector(magnifyWithEvent:), (IMP)np_magnify, "v@:@");
	class_addMethod(cls, @selector(smartMagnifyWithEvent:), (IMP)np_smart, "v@:@");
}

// np_snapshot copies the ring into out and returns how many entries are valid.
static int np_snapshot(np_reading *out) {
	os_unfair_lock_lock(&np_lock);
	int n = np_count < NP_RING ? np_count : NP_RING;
	memcpy(out, np_ring, sizeof(np_ring));
	os_unfair_lock_unlock(&np_lock);
	return n;
}
*/
import "C"

import (
	"time"

	"gioui.org/app"
)

type macNative struct {
	installed bool
	view      uintptr
}

func newNative() Native { return &macNative{} }

func (*macNative) Name() string { return "macOS tablet" }

func (n *macNative) View() uintptr { return n.view }

func (n *macNative) Attach(e app.ViewEvent) {
	v, ok := e.(app.AppKitViewEvent)
	if n.installed || !ok || !v.Valid() {
		return
	}
	n.installed, n.view = true, v.View
	C.np_install()
	C.np_install_gestures(C.CFTypeRef(v.View))
}

// At matches on NSEvent.timestamp, which Gio also uses for pointer.Event.Time.
func (*macNative) At(t time.Duration) (Reading, bool) {
	var buf [C.NP_RING]C.np_reading
	var r ring
	for _, c := range buf[:C.np_snapshot(&buf[0])] {
		r.add(Reading{
			Pressure: float32(c.pressure),
			TiltX:    float32(c.tiltX),
			TiltY:    float32(c.tiltY),
			T:        time.Duration(float64(c.timestamp)*float64(time.Second) + .5),
		})
	}
	return r.at(t)
}
