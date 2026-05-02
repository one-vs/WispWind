package focus

import "github.com/lxn/win"

type Handle = win.HWND

func Current() Handle {
	return win.GetForegroundWindow()
}

func Restore(hwnd Handle) {
	if hwnd != 0 {
		win.SetForegroundWindow(hwnd)
	}
}
