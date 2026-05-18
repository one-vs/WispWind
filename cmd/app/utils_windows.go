//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
	"wispwind/internal/trayicon"
)

func ensureSingleInstance() func() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex := kernel32.NewProc("CreateMutexW")
	mutexName, _ := syscall.UTF16PtrFromString("WispWind_SingleInstance_Mutex")
	handle, _, err := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if err != nil && err.(syscall.Errno) == 183 {
		os.Exit(0)
	}
	return func() {
		syscall.CloseHandle(syscall.Handle(handle))
	}
}

func openPath(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

func getTrayIcon(recording bool) []byte {
	return trayicon.StatusIconICO(recording)
}
