//go:build windows

package paste

import (
	"strings"
	"syscall"
	"time"
	"unicode"
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
	inputKeyboard  = 1
	keyEventfKeyUp = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
	vkC            = 0x43
	vkLeft         = 0x25
	vkRight        = 0x27
	vkShift        = 0x10
	vkBack         = 0x08
)

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
	if HasSelection() {
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
	if text == "" {
		return
	}
	sendKey(vkControl, true)
	if smartSpacing && needsLeadingSpace(readPreviousCharacter()) {
		text = " " + text
	}

	clipboard.WriteAll(text)
	time.Sleep(200 * time.Millisecond)

	sendCombo(vkControl, vkV)
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
	PasteTextWithOptions(text, false)
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
