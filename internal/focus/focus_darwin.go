//go:build darwin

package focus

/*
#cgo LDFLAGS: -framework AppKit
#cgo CFLAGS: -x objective-c

#import <AppKit/AppKit.h>

int getActivePid() {
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (app) {
        return [app processIdentifier];
    }
    return 0;
}

void activatePid(int pid) {
    NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if (app) {
        [app activateWithOptions:NSApplicationActivateAllWindows];
    }
}
*/
import "C"

import "time"

type Handle int32

func Current() Handle {
	return Handle(C.getActivePid())
}

func Restore(h Handle) {
	if h == 0 {
		return
	}
	C.activatePid(C.int(h))
}

// RestoreAndWait activates the app and waits until it is frontmost, so a
// paste lands in the right place. Returns false on timeout.
func RestoreAndWait(h Handle, timeout time.Duration) bool {
	if h == 0 {
		return true
	}
	Restore(h)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if Current() == h {
			return true
		}
		time.Sleep(15 * time.Millisecond)
	}
	return Current() == h
}
