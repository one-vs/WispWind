package hotkey

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookEx = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx   = user32.NewProc("CallNextHookEx")
	procGetMessage       = user32.NewProc("GetMessageW")
)

const (
	whKeyboardLL = 13

	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	// Set on events synthesized via SendInput/keybd_event (Punto Switcher,
	// autoclickers, our own paste module). We ignore those entirely.
	llkhfInjected        = 0x10
	llkhfLowerIlInjected = 0x02
)

type kbdllHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

// runHook installs the WH_KEYBOARD_LL hook and pumps messages forever.
func runHook(l *listener) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	callback := syscall.NewCallback(func(nCode int32, wParam uintptr, lParam uintptr) uintptr {
		if nCode == 0 {
			kb := (*kbdllHookStruct)(unsafe.Pointer(lParam))
			if kb.Flags&(llkhfInjected|llkhfLowerIlInjected) == 0 {
				key := vkName(kb.VkCode)
				var swallow bool
				switch wParam {
				case wmKeyDown, wmSysKeyDown:
					swallow = l.keyDown(key)
				case wmKeyUp, wmSysKeyUp:
					swallow = l.keyUp(key)
				}
				if swallow {
					return 1
				}
			}
		}
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	})

	hook, _, _ := procSetWindowsHookEx.Call(whKeyboardLL, callback, 0, 0)
	if hook == 0 {
		panic("hotkey: SetWindowsHookExW failed")
	}

	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
	}
}
