//go:build !darwin

package hotkey

import (
	"strings"
	"sync"
	"time"

	hook "github.com/robotn/gohook"
)

type Config struct {
	Mode  string
	Start string
	Stop  string
}

// confirmationDelay is how long the start combo must be held before
// recording actually begins. This prevents accidental quick taps.
const confirmationDelay = 120 * time.Millisecond

func Listen(cfg Config, onStart func(), onStop func(), onCancel func()) {
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

	var mu sync.Mutex
	recording := false
	var lastAction time.Time
	var blockedUntil time.Time

	var startPending bool
	var startTimer *time.Timer

	stop := func() {
		mu.Lock()
		defer mu.Unlock()
		if recording {
			recording = false
			lastAction = time.Now()
			blockedUntil = time.Now().Add(700 * time.Millisecond)
			go onStop()
		}
	}

	cancel := func() {
		mu.Lock()
		defer mu.Unlock()
		if time.Now().Before(blockedUntil) {
			return
		}
		if recording && time.Since(lastAction) > 300*time.Millisecond {
			recording = false
			lastAction = time.Now()
			blockedUntil = time.Now().Add(700 * time.Millisecond)
			go onCancel()
		}
	}

	cancelStart := func() {
		mu.Lock()
		defer mu.Unlock()
		if startPending {
			startPending = false
			if startTimer != nil {
				startTimer.Stop()
			}
		}
	}

	tryStart := func() {
		mu.Lock()
		defer mu.Unlock()
		if recording || startPending {
			return
		}
		startPending = true
		startTimer = time.AfterFunc(confirmationDelay, func() {
			mu.Lock()
			defer mu.Unlock()
			if !startPending {
				return
			}
			startPending = false
			if time.Now().Before(blockedUntil) {
				return
			}
			if !recording && time.Since(lastAction) > 300*time.Millisecond {
				recording = true
				lastAction = time.Now()
				go onStart()
			}
		})
	}

	hook.Register(hook.KeyDown, startCombo, func(e hook.Event) {
		mu.Lock()
		isRecording := recording
		mu.Unlock()
		if mode == "toggle" && isRecording {
			cancelStart()
			stop()
			return
		}
		tryStart()
	})

	if mode == "toggle" {
		hook.Register(hook.KeyDown, stopCombo, func(e hook.Event) {
			stop()
		})
	} else {
		hook.Register(hook.KeyUp, startCombo, func(e hook.Event) {
			cancelStart()
			stop()
		})
		for _, key := range startCombo {
			k := key
			hook.Register(hook.KeyUp, []string{k}, func(e hook.Event) {
				cancelStart()
				stop()
			})
		}
	}

	hook.Register(hook.KeyDown, []string{"esc"}, func(e hook.Event) {
		if e.Mask != 0 {
			return
		}
		cancel()
	})

	s := hook.Start()
	<-hook.Process(s)
}

func parseCombo(combo string) []string {
	parts := strings.FieldsFunc(strings.ToLower(combo), func(r rune) bool {
		return r == '+' || r == ',' || r == ' '
	})
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := normalizeKey(part)
		if key != "" {
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
	default:
		return key
	}
}
