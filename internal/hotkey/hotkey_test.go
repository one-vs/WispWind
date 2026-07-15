package hotkey

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func newTestListener(mode string) (*listener, *events) {
	ev := &events{}
	return &listener{
		mode:       mode,
		startCombo: []string{"ctrl", "space"},
		stopCombo:  []string{"ctrl", "shift", "space"},
		pressed:    make(map[string]bool),
		onStart:    func() { ev.add("start") },
		onStop:     func() { ev.add("stop") },
		onCancel:   func() { ev.add("cancel") },
	}, ev
}

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	e.list = append(e.list, s)
	e.mu.Unlock()
}

func (e *events) get() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.list...)
}

func waitConfirm() { time.Sleep(confirmationDelay + 80*time.Millisecond) }

func TestParseCombo(t *testing.T) {
	cases := map[string][]string{
		"ctrl+space":       {"ctrl", "space"},
		"Ctrl + Space":     {"ctrl", "space"},
		"ctrl,shift,space": {"ctrl", "shift", "space"},
		"win+escape":       {"cmd", "esc"},
		"ctrl+ctrl+space":  {"ctrl", "space"},
	}
	for input, want := range cases {
		if got := parseCombo(input); !reflect.DeepEqual(got, want) {
			t.Errorf("parseCombo(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestVkName(t *testing.T) {
	cases := map[uint32]string{
		0xA2: "ctrl", 0xA3: "ctrl", 0xA0: "shift", 0x5B: "cmd",
		0x20: "space", 0x1B: "esc", 0x41: "a", 0x5A: "z",
		0x30: "0", 0x70: "f1", 0x7B: "f12",
	}
	for vk, want := range cases {
		if got := vkName(vk); got != want {
			t.Errorf("vkName(%#x) = %q, want %q", vk, got, want)
		}
	}
}

func TestHoldStartStop(t *testing.T) {
	l, ev := newTestListener("hold")
	l.keyDown("ctrl")
	if swallow := l.keyDown("space"); !swallow {
		t.Error("trigger key should be swallowed while start pending")
	}
	waitConfirm()
	if got := ev.get(); !reflect.DeepEqual(got, []string{"start"}) {
		t.Fatalf("after hold: %v", got)
	}
	time.Sleep(350 * time.Millisecond) // pass the min-recording guard
	l.keyUp("space")
	l.keyUp("ctrl")
	time.Sleep(50 * time.Millisecond)
	if got := ev.get(); !reflect.DeepEqual(got, []string{"start", "stop"}) {
		t.Fatalf("after release: %v", got)
	}
}

func TestQuickTapDoesNotStart(t *testing.T) {
	l, ev := newTestListener("hold")
	l.keyDown("ctrl")
	l.keyDown("space")
	l.keyUp("space") // released before confirmation delay
	l.keyUp("ctrl")
	waitConfirm()
	if got := ev.get(); len(got) != 0 {
		t.Fatalf("quick tap should not trigger, got %v", got)
	}
}

func TestExtraKeyBlocksStart(t *testing.T) {
	l, ev := newTestListener("hold")
	l.keyDown("ctrl")
	l.keyDown("shift") // ctrl+shift+space is NOT the start combo
	l.keyDown("space")
	waitConfirm()
	if got := ev.get(); len(got) != 0 {
		t.Fatalf("superset combo should not start, got %v", got)
	}
}

func TestExtraKeyAfterPendingCancels(t *testing.T) {
	l, ev := newTestListener("hold")
	l.keyDown("ctrl")
	l.keyDown("space")
	l.keyDown("x") // typing something else during the confirmation window
	waitConfirm()
	if got := ev.get(); len(got) != 0 {
		t.Fatalf("start should be cancelled by extra key, got %v", got)
	}
}

func TestEscCancelsAndSwallows(t *testing.T) {
	l, ev := newTestListener("toggle")
	l.keyDown("ctrl")
	l.keyDown("space")
	l.keyUp("space")
	l.keyUp("ctrl")
	waitConfirm()
	time.Sleep(350 * time.Millisecond)
	if swallow := l.keyDown("esc"); !swallow {
		t.Error("esc should be swallowed while recording")
	}
	l.keyUp("esc")
	time.Sleep(50 * time.Millisecond)
	if got := ev.get(); !reflect.DeepEqual(got, []string{"start", "cancel"}) {
		t.Fatalf("events: %v", got)
	}
	// Esc when idle passes through untouched.
	if swallow := l.keyDown("esc"); swallow {
		t.Error("esc must not be swallowed when idle")
	}
}

func TestToggleStop(t *testing.T) {
	l, ev := newTestListener("toggle")
	l.keyDown("ctrl")
	l.keyDown("space")
	l.keyUp("space")
	l.keyUp("ctrl")
	waitConfirm()
	time.Sleep(350 * time.Millisecond)
	l.keyDown("ctrl")
	l.keyDown("shift")
	l.keyDown("space")
	time.Sleep(50 * time.Millisecond)
	if got := ev.get(); !reflect.DeepEqual(got, []string{"start", "stop"}) {
		t.Fatalf("events: %v", got)
	}
}
