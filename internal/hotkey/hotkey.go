package hotkey

import (
	"fmt"
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

// confirmationDelay is how long the start combo must be held before
// recording actually begins. This prevents accidental quick taps.
const confirmationDelay = 200 * time.Millisecond

type listener struct {
	mu   sync.Mutex
	mode string

	startCombo   []string
	stopCombo    []string
	historyCombo []string

	// pressed tracks physically pressed keys (normalized names). Injected
	// (synthetic) key events never reach this map, so layout switchers like
	// Punto Switcher cannot fake a hotkey press.
	pressed map[string]bool

	recording    bool
	startPending bool
	startTimer   *time.Timer
	lastAction   time.Time
	blockedUntil time.Time

	onStart   func()
	onStop    func()
	onCancel  func()
	onHistory func()
}

var (
	activeMu sync.Mutex
	active   *listener
)

// Listen installs a low-level keyboard hook and blocks forever dispatching
// hotkey events. It must be called at most once.
func Listen(cfg Config, onStart func(), onStop func(), onCancel func(), onHistory func()) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "hold"
	}
	startCombo := parseCombo(cfg.Start)
	if len(startCombo) == 0 {
		startCombo = []string{"ctrl", "space"}
	}
	stopCombo := parseCombo(cfg.Stop)
	if len(stopCombo) == 0 {
		stopCombo = []string{"ctrl", "shift", "space"}
	}
	historyCombo := parseCombo(cfg.History)

	l := &listener{
		mode:         mode,
		startCombo:   startCombo,
		stopCombo:    stopCombo,
		historyCombo: historyCombo,
		pressed:      make(map[string]bool),
		onStart:      onStart,
		onStop:       onStop,
		onCancel:     onCancel,
		onHistory:    onHistory,
	}

	activeMu.Lock()
	active = l
	activeMu.Unlock()

	runHook(l)
}

// ForceStop finishes the current recording as if the user released the
// hotkey. Used for safety limits (max recording duration).
func ForceStop() {
	activeMu.Lock()
	l := active
	activeMu.Unlock()
	if l != nil {
		l.stop()
	}
}

// Recording reports whether a recording is currently active.
func Recording() bool {
	activeMu.Lock()
	l := active
	activeMu.Unlock()
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.recording
}

func (l *listener) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopLocked()
}

func (l *listener) stopLocked() {
	if !l.recording {
		return
	}
	l.recording = false
	l.lastAction = time.Now()
	l.blockedUntil = time.Now().Add(700 * time.Millisecond)
	go l.onStop()
}

func (l *listener) cancelRecordingLocked() {
	if time.Now().Before(l.blockedUntil) {
		return
	}
	if l.recording && time.Since(l.lastAction) > 300*time.Millisecond {
		l.recording = false
		l.lastAction = time.Now()
		l.blockedUntil = time.Now().Add(700 * time.Millisecond)
		go l.onCancel()
	}
}

func (l *listener) cancelStartLocked() {
	if l.startPending {
		l.startPending = false
		if l.startTimer != nil {
			l.startTimer.Stop()
		}
	}
}

func (l *listener) tryStartLocked() {
	if l.recording || l.startPending {
		return
	}
	l.startPending = true
	l.startTimer = time.AfterFunc(confirmationDelay, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if !l.startPending {
			return
		}
		l.startPending = false
		// In hold mode the combo must still be exactly held after the
		// confirmation delay; a quick accidental tap therefore never starts.
		// In toggle mode the keys are naturally released right away.
		if l.mode == "hold" && !l.exactMatchLocked(l.startCombo) {
			return
		}
		if time.Now().Before(l.blockedUntil) {
			return
		}
		if !l.recording && time.Since(l.lastAction) > 300*time.Millisecond {
			l.recording = true
			l.lastAction = time.Now()
			go l.onStart()
		}
	})
}

// exactMatchLocked reports whether exactly the combo keys (and nothing else)
// are currently pressed.
func (l *listener) exactMatchLocked(combo []string) bool {
	if len(l.pressed) != len(combo) {
		return false
	}
	for _, k := range combo {
		if !l.pressed[k] {
			return false
		}
	}
	return true
}

func comboContains(combo []string, key string) bool {
	for _, k := range combo {
		if k == key {
			return true
		}
	}
	return false
}

// isModifier reports whether the key is a modifier (we only swallow
// non-modifier trigger keys so apps never see stuck modifiers).
func isModifier(key string) bool {
	switch key {
	case "ctrl", "shift", "alt", "cmd":
		return true
	}
	return false
}

// keyDown processes a physical key press. Returns true when the event should
// be swallowed (not delivered to the foreground application).
func (l *listener) keyDown(key string) (swallow bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	repeat := l.pressed[key]
	l.pressed[key] = true

	// Escape cancels an active recording and is swallowed so the foreground
	// app (e.g. Telegram) does not also react to it.
	if key == "esc" && len(l.pressed) == 1 {
		if l.recording {
			l.cancelRecordingLocked()
			return true
		}
		return false
	}

	if l.exactMatchLocked(l.startCombo) {
		wasRecording := l.recording
		if l.mode == "toggle" && l.recording {
			if !repeat {
				l.cancelStartLocked()
				l.stopLocked()
			}
		} else if !repeat {
			l.tryStartLocked()
		}
		// Swallow the non-modifier trigger key (and its auto-repeats) so the
		// app underneath doesn't react to e.g. Ctrl+Space.
		if !isModifier(key) && (l.startPending || l.recording || wasRecording) {
			return true
		}
		return false
	}

	if l.mode == "toggle" && l.exactMatchLocked(l.stopCombo) {
		wasRecording := l.recording
		if !repeat {
			l.stopLocked()
		}
		if !isModifier(key) && wasRecording {
			return true
		}
		return false
	}

	// History panel combo (e.g. Ctrl+Space+Z). Only when not recording, so
	// it can't clash with an active dictation.
	if len(l.historyCombo) > 0 && l.exactMatchLocked(l.historyCombo) && !l.recording {
		if !repeat {
			l.cancelStartLocked()
			if l.onHistory != nil {
				go l.onHistory()
			}
		}
		if !isModifier(key) {
			return true
		}
		return false
	}

	// Any unrelated key breaks a pending start (e.g. Ctrl+Space+X).
	if !repeat {
		l.cancelStartLocked()
	}
	return false
}

// keyUp processes a physical key release. Returns true when the event should
// be swallowed.
func (l *listener) keyUp(key string) (swallow bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.pressed, key)

	if l.mode == "hold" && comboContains(l.startCombo, key) {
		l.cancelStartLocked()
		wasRecording := l.recording
		l.stopLocked()
		if !isModifier(key) && wasRecording {
			return true
		}
	}
	return false
}

func parseCombo(combo string) []string {
	parts := strings.FieldsFunc(strings.ToLower(combo), func(r rune) bool {
		return r == '+' || r == ',' || r == ' '
	})
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := normalizeKey(part)
		if key != "" && !comboContains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	switch key {
	case "", "none":
		return ""
	case "control", "ctrlleft", "ctrlright":
		return "ctrl"
	case "cmd", "command", "win", "windows", "super":
		return "cmd"
	case "escape":
		return "esc"
	case "spacebar":
		return "space"
	default:
		return key
	}
}

// vkName maps a Windows virtual-key code to a normalized key name.
func vkName(vk uint32) string {
	switch vk {
	case 0xA0, 0xA1, 0x10: // L/R/generic shift
		return "shift"
	case 0xA2, 0xA3, 0x11: // L/R/generic ctrl
		return "ctrl"
	case 0xA4, 0xA5, 0x12: // L/R/generic alt
		return "alt"
	case 0x5B, 0x5C: // L/R win
		return "cmd"
	case 0x20:
		return "space"
	case 0x1B:
		return "esc"
	case 0x0D:
		return "enter"
	case 0x09:
		return "tab"
	case 0x08:
		return "backspace"
	case 0x14: // caps lock
		return "capslock"
	}
	if vk >= 0x30 && vk <= 0x39 { // 0-9
		return string(rune('0' + vk - 0x30))
	}
	if vk >= 0x41 && vk <= 0x5A { // a-z
		return string(rune('a' + vk - 0x41))
	}
	if vk >= 0x70 && vk <= 0x87 { // f1-f24
		return fmt.Sprintf("f%d", vk-0x70+1)
	}
	return fmt.Sprintf("vk%02x", vk)
}
