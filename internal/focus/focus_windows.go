package focus

import (
	"syscall"

	"github.com/lxn/win"
)

type Handle = win.HWND

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procAttachThreadInput     = user32.NewProc("AttachThreadInput")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procGetCurrentThreadId    = kernel32.NewProc("GetCurrentThreadId")
	procBringWindowToTop      = user32.NewProc("BringWindowToTop")
)

func Current() Handle {
	return win.GetForegroundWindow()
}

// Restore sets hwnd as the foreground window reliably.
// Plain SetForegroundWindow is silently ignored by Windows when the calling
// process is not the current foreground process. The AttachThreadInput trick
// temporarily merges input queues so the call is accepted.
func Restore(hwnd Handle) {
	if hwnd == 0 {
		return
	}

	targetTID, _, _ := procGetWindowThreadProcId.Call(uintptr(hwnd), 0)
	currentTID, _, _ := procGetCurrentThreadId.Call()

	if targetTID != 0 && targetTID != currentTID {
		procAttachThreadInput.Call(currentTID, targetTID, 1)
		win.SetForegroundWindow(hwnd)
		procBringWindowToTop.Call(uintptr(hwnd))
		procAttachThreadInput.Call(currentTID, targetTID, 0)
	} else {
		win.SetForegroundWindow(hwnd)
		procBringWindowToTop.Call(uintptr(hwnd))
	}
}
