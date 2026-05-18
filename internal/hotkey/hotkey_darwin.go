//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework Carbon -framework CoreFoundation
#include <Carbon/Carbon.h>
#include <CoreFoundation/CoreFoundation.h>

extern void wispwindHotkeyCallback(int id);

static OSStatus wispwindHotkeyHandler(EventHandlerCallRef nextHandler, EventRef event, void *userData) {
	EventHotKeyID hotKeyID;
	GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(hotKeyID), NULL, &hotKeyID);
	wispwindHotkeyCallback((int)hotKeyID.id);
	return noErr;
}

static int wispwindRegisterHotkey(int id, UInt32 keyCode, UInt32 modifiers) {
	EventHotKeyID hotKeyID;
	hotKeyID.signature = 'wspw';
	hotKeyID.id = (UInt32)id;
	EventTypeSpec eventType;
	eventType.eventClass = kEventClassKeyboard;
	eventType.eventKind = kEventHotKeyPressed;
	InstallApplicationEventHandler(&wispwindHotkeyHandler, 1, &eventType, NULL, NULL);
	EventHotKeyRef ref = NULL;
	return RegisterEventHotKey(keyCode, modifiers, hotKeyID, GetEventDispatcherTarget(), 0, &ref);
}

static void wispwindRunHotkeyLoop() {
	CFRunLoopRun();
}
*/
import "C"

import (
	"log"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Mode  string
	Start string
	Stop  string
}

const confirmationDelay = 120 * time.Millisecond

var darwinHotkeyState struct {
	mu           sync.Mutex
	recording    bool
	lastAction   time.Time
	blockedUntil time.Time
	onStart      func()
	onStop       func()
	onCancel     func()
}

func Listen(cfg Config, onStart func(), onStop func(), onCancel func()) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "hold"
	}

	startCode, startMods := parseCarbonCombo(cfg.Start)
	if startCode == 0 {
		startCode, startMods = 49, C.controlKey
	}
	stopCode, stopMods := parseCarbonCombo(cfg.Stop)
	if stopCode == 0 {
		stopCode, stopMods = 49, C.controlKey|C.shiftKey
	}

	darwinHotkeyState.mu.Lock()
	darwinHotkeyState.onStart = onStart
	darwinHotkeyState.onStop = onStop
	darwinHotkeyState.onCancel = onCancel
	darwinHotkeyState.mu.Unlock()

	startStatus := C.wispwindRegisterHotkey(1, C.UInt32(startCode), C.UInt32(startMods))
	log.Printf("macOS hotkey start registration status: %d", int(startStatus))
	if mode == "toggle" && (stopCode != startCode || stopMods != startMods) {
		stopStatus := C.wispwindRegisterHotkey(2, C.UInt32(stopCode), C.UInt32(stopMods))
		log.Printf("macOS hotkey stop registration status: %d", int(stopStatus))
	}
	cancelStatus := C.wispwindRegisterHotkey(3, C.UInt32(53), 0)
	log.Printf("macOS hotkey cancel registration status: %d", int(cancelStatus))
	C.wispwindRunHotkeyLoop()
}

//export wispwindHotkeyCallback
func wispwindHotkeyCallback(id C.int) {
	switch int(id) {
	case 1:
		darwinStartOrStop()
	case 2:
		darwinStop()
	case 3:
		darwinCancel()
	}
}

func darwinStartOrStop() {
	darwinHotkeyState.mu.Lock()
	if time.Now().Before(darwinHotkeyState.blockedUntil) {
		darwinHotkeyState.mu.Unlock()
		return
	}
	if darwinHotkeyState.recording {
		darwinHotkeyState.recording = false
		darwinHotkeyState.lastAction = time.Now()
		darwinHotkeyState.blockedUntil = time.Now().Add(700 * time.Millisecond)
		onStop := darwinHotkeyState.onStop
		darwinHotkeyState.mu.Unlock()
		go onStop()
		return
	}
	if time.Since(darwinHotkeyState.lastAction) <= 300*time.Millisecond {
		darwinHotkeyState.mu.Unlock()
		return
	}
	darwinHotkeyState.recording = true
	darwinHotkeyState.lastAction = time.Now()
	onStart := darwinHotkeyState.onStart
	darwinHotkeyState.mu.Unlock()
	time.AfterFunc(confirmationDelay, func() {
		go onStart()
	})
}

func darwinStop() {
	darwinHotkeyState.mu.Lock()
	if !darwinHotkeyState.recording {
		darwinHotkeyState.mu.Unlock()
		return
	}
	darwinHotkeyState.recording = false
	darwinHotkeyState.lastAction = time.Now()
	darwinHotkeyState.blockedUntil = time.Now().Add(700 * time.Millisecond)
	onStop := darwinHotkeyState.onStop
	darwinHotkeyState.mu.Unlock()
	go onStop()
}

func darwinCancel() {
	darwinHotkeyState.mu.Lock()
	if time.Now().Before(darwinHotkeyState.blockedUntil) || !darwinHotkeyState.recording || time.Since(darwinHotkeyState.lastAction) <= 300*time.Millisecond {
		darwinHotkeyState.mu.Unlock()
		return
	}
	darwinHotkeyState.recording = false
	darwinHotkeyState.lastAction = time.Now()
	darwinHotkeyState.blockedUntil = time.Now().Add(700 * time.Millisecond)
	onCancel := darwinHotkeyState.onCancel
	darwinHotkeyState.mu.Unlock()
	go onCancel()
}

func parseCarbonCombo(combo string) (uint32, uint32) {
	parts := strings.FieldsFunc(strings.ToLower(combo), func(r rune) bool {
		return r == '+' || r == ',' || r == ' '
	})
	var keyCode uint32
	var modifiers uint32
	for _, part := range parts {
		switch strings.TrimSpace(part) {
		case "ctrl", "control", "ctrlleft", "ctrlright":
			modifiers |= uint32(C.controlKey)
		case "shift", "shiftleft", "shiftright":
			modifiers |= uint32(C.shiftKey)
		case "alt", "option", "opt":
			modifiers |= uint32(C.optionKey)
		case "cmd", "command", "super":
			modifiers |= uint32(C.cmdKey)
		case "space":
			keyCode = 49
		case "esc", "escape":
			keyCode = 53
		}
	}
	return keyCode, modifiers
}
