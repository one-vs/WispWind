package hotkey

import (
	"strings"
	"time"

	hook "github.com/robotn/gohook"
)

type Config struct {
	Mode  string
	Start string
	Stop  string
}

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

	recording := false
	var lastAction time.Time
	var blockedUntil time.Time

	start := func() {
		if time.Now().Before(blockedUntil) {
			return
		}
		if !recording && time.Since(lastAction) > 300*time.Millisecond {
			recording = true
			lastAction = time.Now()
			go onStart()
		}
	}

	stop := func() {
		if recording && time.Since(lastAction) > 300*time.Millisecond {
			recording = false
			lastAction = time.Now()
			blockedUntil = time.Now().Add(700 * time.Millisecond)
			go onStop()
		}
	}

	cancel := func() {
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

	hook.Register(hook.KeyDown, startCombo, func(e hook.Event) {
		if mode == "toggle" && recording {
			stop()
			return
		}
		start()
	})

	if mode == "toggle" {
		hook.Register(hook.KeyDown, stopCombo, func(e hook.Event) {
			stop()
		})
	} else {
		hook.Register(hook.KeyUp, startCombo, func(e hook.Event) {
			stop()
		})
		for _, key := range startCombo {
			k := key
			hook.Register(hook.KeyUp, []string{k}, func(e hook.Event) {
				stop()
			})
		}
	}

	hook.Register(hook.KeyDown, []string{"esc"}, func(e hook.Event) {
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
