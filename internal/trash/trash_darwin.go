//go:build darwin && cgo

package trash

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>

// Returns NULL on success, else a malloc'd error message.
static char *trash_move(const char *path) {
	@autoreleasepool {
		NSURL *u = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		NSError *err = nil;
		if ([[NSFileManager defaultManager] trashItemAtURL:u resultingItemURL:nil error:&err]) return NULL;
		return strdup(err.localizedDescription.UTF8String);
	}
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func move(path string) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	if e := C.trash_move(cs); e != nil {
		defer C.free(unsafe.Pointer(e))
		return errors.New(C.GoString(e))
	}
	return nil
}
