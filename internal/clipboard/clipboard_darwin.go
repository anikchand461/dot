//go:build darwin && cgo

package clipboard

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

static long cb_change(void) { return (long)[[NSPasteboard generalPasteboard] changeCount]; }

// cb_image returns malloc'd PNG bytes (caller frees) or NULL.
static void *cb_image(int *len) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		NSData *d = [pb dataForType:NSPasteboardTypePNG];
		if (d == nil) {
			NSData *tiff = [pb dataForType:NSPasteboardTypeTIFF];
			if (tiff == nil) {
				// An image file copied in Finder.
				NSArray *urls = [pb readObjectsForClasses:@[[NSURL class]]
					options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
				for (NSURL *u in urls) {
					NSImage *img = [[NSImage alloc] initWithContentsOfURL:u];
					if (img != nil) { tiff = [img TIFFRepresentation]; break; }
				}
			}
			if (tiff != nil) {
				NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:tiff];
				d = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
			}
		}
		if (d == nil || d.length == 0) return NULL;
		*len = (int)d.length;
		void *buf = malloc(d.length);
		memcpy(buf, d.bytes, d.length);
		return buf;
	}
}

static char *cb_text(void) {
	@autoreleasepool {
		NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
		if (s == nil) return NULL;
		return strdup(s.UTF8String);
	}
}
*/
import "C"

import "unsafe"

func changeCount() int64 { return int64(C.cb_change()) }

func readImage() []byte {
	var n C.int
	p := C.cb_image(&n)
	if p == nil {
		return nil
	}
	defer C.free(p)
	return C.GoBytes(p, n)
}

func readText() string {
	p := C.cb_text()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}
