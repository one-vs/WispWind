package paste

import (
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/atotto/clipboard"
)

var (
	user32dll         = syscall.NewLazyDLL("user32.dll")
	procGetForeground = user32dll.NewProc("GetForegroundWindow")
	procSendInput     = user32dll.NewProc("SendInput")
)

const (
	inputKeyboard    = 1
	keyEventfKeyUp   = 0x0002
	keyEventfUnicode = 0x0004
	vkControl        = 0x11
	vkV              = 0x56
	vkC              = 0x43
	vkLeft           = 0x25
	vkRight          = 0x27
	vkShift          = 0x10
	vkBack           = 0x08
	vkReturn         = 0x0D
)

// Options control paste behavior; set once at startup from config.
type Options struct {
	// SmartSpacing probes the surrounding text by sending Ctrl+C/Shift+Left
	// keystrokes into the target app. Disabled by default because those
	// synthetic keystrokes have side effects in some apps (Telegram,
	// terminals).
	SmartSpacing bool
	// RestoreClipboard puts the user's previous clipboard content back after
	// pasting.
	RestoreClipboard bool
	// Mode is "clipboard" (Ctrl+V) or "type" (character-by-character via
	// SendInput KEYEVENTF_UNICODE, never touches the clipboard).
	Mode string
}

var (
	optMu sync.RWMutex
	opts  = Options{RestoreClipboard: true, Mode: "clipboard"}
)

func Configure(o Options) {
	if o.Mode != "type" {
		o.Mode = "clipboard"
	}
	optMu.Lock()
	opts = o
	optMu.Unlock()
}

func currentOptions() Options {
	optMu.RLock()
	defer optMu.RUnlock()
	return opts
}

type keyboardInput struct {
	Type    uint32
	_       uint32 // padding for union alignment on 64-bit
	WVk     uint16
	WScan   uint16
	DwFlags uint32
	Time    uint32
	DwExtra uintptr
	_       [8]byte // padding so struct matches sizeof(INPUT) = 40 on x64
}

func sendKey(vk uint16, keyUp bool) {
	var flags uint32
	if keyUp {
		flags = keyEventfKeyUp
	}
	in := keyboardInput{
		Type:    inputKeyboard,
		WVk:     vk,
		DwFlags: flags,
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func sendUnicode(codeUnit uint16, keyUp bool) {
	flags := uint32(keyEventfUnicode)
	if keyUp {
		flags |= keyEventfKeyUp
	}
	in := keyboardInput{
		Type:    inputKeyboard,
		WScan:   codeUnit,
		DwFlags: flags,
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func sendCombo(modifier, key uint16) {
	sendKey(modifier, false)
	time.Sleep(20 * time.Millisecond)
	sendKey(key, false)
	time.Sleep(20 * time.Millisecond)
	sendKey(key, true)
	time.Sleep(20 * time.Millisecond)
	sendKey(modifier, true)
}

func Init() error {
	return nil // atotto/clipboard doesn't require initialization
}

func PasteText(text string) {
	if text == "" {
		return
	}
	PasteTextWithOptions(text, false)
}

func PasteTextSmart(text string) {
	if text == "" {
		return
	}
	if currentOptions().SmartSpacing && HasSelection() {
		PasteTextWithOptions(text, false)
		return
	}
	PasteTextWithOptions(text, true)
}

func HasSelection() bool {
	previousClipboard, _ := clipboard.ReadAll()
	defer clipboard.WriteAll(previousClipboard)

	clipboard.WriteAll("")
	time.Sleep(40 * time.Millisecond)

	sendCombo(vkControl, vkC)
	time.Sleep(70 * time.Millisecond)

	selected, _ := clipboard.ReadAll()
	return selected != ""
}

func PasteTextWithOptions(text string, smartSpacing bool) {
	pasteInternal(text, smartSpacing, true)
}

func pasteInternal(text string, smartSpacing bool, restore bool) {
	if text == "" {
		return
	}
	o := currentOptions()
	// Release Ctrl in case the user is still holding the hotkey modifier.
	sendKey(vkControl, true)
	if smartSpacing && o.SmartSpacing && needsLeadingSpace(readPreviousCharacter()) {
		text = " " + text
	}

	if o.Mode == "type" {
		TypeText(text)
		return
	}

	var previousClipboard string
	if restore && o.RestoreClipboard {
		previousClipboard, _ = clipboard.ReadAll()
	}

	clipboard.WriteAll(text)
	time.Sleep(200 * time.Millisecond)

	sendCombo(vkControl, vkV)

	if restore && o.RestoreClipboard {
		// Give the target app time to actually read the clipboard before we
		// put the user's previous content back.
		time.Sleep(400 * time.Millisecond)
		clipboard.WriteAll(previousClipboard)
	}
}

// TypeText types the text into the focused window character-by-character via
// SendInput with KEYEVENTF_UNICODE. The clipboard is never touched.
func TypeText(text string) {
	for _, r := range text {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			sendKey(vkReturn, false)
			sendKey(vkReturn, true)
			time.Sleep(2 * time.Millisecond)
			continue
		}
		for _, cu := range utf16.Encode([]rune{r}) {
			sendUnicode(cu, false)
			sendUnicode(cu, true)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func readPreviousCharacter() string {
	previousClipboard, _ := clipboard.ReadAll()
	defer clipboard.WriteAll(previousClipboard)

	clipboard.WriteAll("")
	time.Sleep(40 * time.Millisecond)

	sendCombo(vkShift, vkLeft)
	time.Sleep(30 * time.Millisecond)

	sendCombo(vkControl, vkC)
	time.Sleep(70 * time.Millisecond)

	selected, _ := clipboard.ReadAll()
	sendKey(vkRight, false)
	time.Sleep(20 * time.Millisecond)
	sendKey(vkRight, true)
	return selected
}

func needsLeadingSpace(prev string) bool {
	prev = strings.TrimRightFunc(prev, unicode.IsSpace)
	if prev == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(prev)
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '.', '!', '?', ':', ';', ',', ')', ']', '}', '"', '\'':
		return true
	default:
		return false
	}
}

type LiveWriter struct {
	insertedRunes int
}

func NewLiveWriter() *LiveWriter {
	return &LiveWriter{}
}

func (w *LiveWriter) Replace(text string) {
	w.erase()
	// Live updates skip clipboard restore: restoring after every interim
	// update would thrash the clipboard several times per second.
	pasteInternal(text, false, false)
	w.insertedRunes = utf8.RuneCountInString(text)
}

// ReplaceFinal writes the final text and restores the user's clipboard.
func (w *LiveWriter) ReplaceFinal(text string) {
	w.erase()
	pasteInternal(text, false, true)
	w.insertedRunes = utf8.RuneCountInString(text)
}

func (w *LiveWriter) Clear() {
	w.erase()
	w.insertedRunes = 0
}

func (w *LiveWriter) Forget() {
	w.insertedRunes = 0
}

func (w *LiveWriter) erase() {
	if w.insertedRunes <= 0 {
		return
	}
	sendKey(vkControl, true)
	for i := 0; i < w.insertedRunes; i++ {
		sendKey(vkBack, false)
		time.Sleep(5 * time.Millisecond)
		sendKey(vkBack, true)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}
