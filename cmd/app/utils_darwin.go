//go:build darwin

package main

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>

static int wispwind_request_accessibility_permission() {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *values[] = { kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys,
		values,
		1,
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks
	);
	Boolean trusted = AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
	return trusted ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"wispwind/internal/trayicon"
)

func ensureSingleInstance() func() {
	lockFile := filepath.Join(os.TempDir(), "wispwind.lock")
	file, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Printf("Failed to open lock file: %v\n", err)
		os.Exit(1)
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		fmt.Println("Another instance is already running.")
		file.Close()
		os.Exit(0)
	}

	return func() {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		os.Remove(lockFile)
	}
}

func openPath(path string) error {
	return exec.Command("open", path).Start()
}

func getTrayIcon(recording bool) []byte {
	return trayicon.StatusIconPNG(recording)
}

func requestRuntimePermissions() bool {
	return C.wispwind_request_accessibility_permission() == 1
}
