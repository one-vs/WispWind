//go:build darwin

package focus

/*
#cgo LDFLAGS: -framework AppKit

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
        [app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
    }
}
*/
import "C"

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
