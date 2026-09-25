//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework Carbon -framework CoreFoundation -framework ApplicationServices
#include <Carbon/Carbon.h>
#include <CoreFoundation/CoreFoundation.h>
#include <ApplicationServices/ApplicationServices.h>

extern void wispwindHotkeyCallback(int id);

// Modifier-only combos (e.g. Option+Shift) can't be registered as Carbon
// hotkeys, so they are detected with a listen-only event tap: the combo fires
// when exactly those modifiers were held together and then released without
// any other key being pressed in between (so ⌥⇧- for an em dash is ignored).
#define WW_MOD_MASK (kCGEventFlagMaskShift | kCGEventFlagMaskAlternate | kCGEventFlagMaskControl | kCGEventFlagMaskCommand)

static CGEventFlags wwComboFlags;
static int wwArmed;
static CFAbsoluteTime wwArmedAt;
static CFMachPortRef wwTap;

static CGEventRef wwTapCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *info) {
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (wwTap) CGEventTapEnable(wwTap, true);
		return event;
	}
	if (type == kCGEventKeyDown || type == kCGEventLeftMouseDown || type == kCGEventRightMouseDown) {
		wwArmed = 0;
		return event;
	}
	if (type != kCGEventFlagsChanged) return event;

	CGEventFlags mods = CGEventGetFlags(event) & WW_MOD_MASK;
	if (mods == wwComboFlags) {
		wwArmed = 1;
		wwArmedAt = CFAbsoluteTimeGetCurrent();
	} else if (mods == 0) {
		if (wwArmed && CFAbsoluteTimeGetCurrent() - wwArmedAt < 1.0) {
			wispwindHotkeyCallback(4);
		}
		wwArmed = 0;
	} else if ((mods & ~wwComboFlags) != 0) {
		wwArmed = 0; // an extra modifier joined in
	}
	return event;
}

// Installs the tap on the current thread's run loop. Returns 0 on success.
static int wispwindInstallModifierCombo(UInt64 flags) {
	wwComboFlags = (CGEventFlags)flags;
	if (!CGPreflightListenEventAccess()) {
		CGRequestListenEventAccess();
	}
	CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged) | CGEventMaskBit(kCGEventKeyDown) |
		CGEventMaskBit(kCGEventLeftMouseDown) | CGEventMaskBit(kCGEventRightMouseDown);
	wwTap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionListenOnly,
		mask, wwTapCallback, NULL);
	if (!wwTap) return -1;
	CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, wwTap, 0);
	CFRunLoopAddSource(CFRunLoopGetCurrent(), src, kCFRunLoopCommonModes);
	CFRelease(src);
	CGEventTapEnable(wwTap, true);
	return 0;
}

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
	Mode    string
	Start   string
	Stop    string
	History string
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
	onHistory    func()
}

// ForceStop finishes the current recording as if the user released the
// hotkey. Used for safety limits (max recording duration).
func ForceStop() {
	darwinStop()
}

// Recording reports whether a recording is currently active.
func Recording() bool {
	darwinHotkeyState.mu.Lock()
	defer darwinHotkeyState.mu.Unlock()
	return darwinHotkeyState.recording
}

func Listen(cfg Config, onStart func(), onStop func(), onCancel func(), onHistory func()) {
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
	darwinHotkeyState.onHistory = onHistory
	darwinHotkeyState.mu.Unlock()

	startStatus := C.wispwindRegisterHotkey(1, C.UInt32(startCode), C.UInt32(startMods))
	log.Printf("macOS hotkey start registration status: %d", int(startStatus))
	if mode == "toggle" && (stopCode != startCode || stopMods != startMods) {
		stopStatus := C.wispwindRegisterHotkey(2, C.UInt32(stopCode), C.UInt32(stopMods))
		log.Printf("macOS hotkey stop registration status: %d", int(stopStatus))
	}
	cancelStatus := C.wispwindRegisterHotkey(3, C.UInt32(53), 0)
	log.Printf("macOS hotkey cancel registration status: %d", int(cancelStatus))

	if onHistory != nil {
		registerHistoryHotkey(cfg.History)
	}
	C.wispwindRunHotkeyLoop()
}

// registerHistoryHotkey binds the history panel: a modifier-only combo
// (default Option+Shift) goes through the event tap, a regular combo through
// Carbon. Windows-style chords like "ctrl+space+z" aren't supported here and
// fall back to the default.
func registerHistoryHotkey(combo string) {
	code, mods := parseCarbonCombo(combo)
	if code != 0 && !strings.Contains(strings.ToLower(combo), "space+") {
		status := C.wispwindRegisterHotkey(4, C.UInt32(code), C.UInt32(mods))
		log.Printf("macOS hotkey history (%s) registration status: %d", combo, int(status))
		return
	}
	if code != 0 || mods == 0 {
		if combo != "" {
			log.Printf("History hotkey %q is not supported on macOS, using alt+shift", combo)
		}
		mods = uint32(C.optionKey | C.shiftKey)
	}
	var flags C.UInt64
	if mods&uint32(C.shiftKey) != 0 {
		flags |= C.kCGEventFlagMaskShift
	}
	if mods&uint32(C.optionKey) != 0 {
		flags |= C.kCGEventFlagMaskAlternate
	}
	if mods&uint32(C.controlKey) != 0 {
		flags |= C.kCGEventFlagMaskControl
	}
	if mods&uint32(C.cmdKey) != 0 {
		flags |= C.kCGEventFlagMaskCommand
	}
	if C.wispwindInstallModifierCombo(flags) != 0 {
		log.Printf("History hotkey: event tap unavailable; enable WispWind in Privacy & Security > Input Monitoring")
		return
	}
	log.Printf("macOS hotkey history registered: modifier-only combo")
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
	case 4:
		darwinHistory()
	}
}

func darwinHistory() {
	darwinHotkeyState.mu.Lock()
	recording := darwinHotkeyState.recording
	onHistory := darwinHotkeyState.onHistory
	darwinHotkeyState.mu.Unlock()
	if recording || onHistory == nil {
		return
	}
	go onHistory()
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
