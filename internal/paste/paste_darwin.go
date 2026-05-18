//go:build darwin

package paste

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/go-vgo/robotgo"
)

func Init() error {
	return nil
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

	robotgo.KeyTap("c", "command")
	time.Sleep(100 * time.Millisecond)

	selected, _ := clipboard.ReadAll()
	return selected != ""
}

func PasteTextWithOptions(text string, smartSpacing bool) {
	if text == "" {
		return
	}
	if smartSpacing && needsLeadingSpace(readPreviousCharacter()) {
		text = " " + text
	}

	clipboard.WriteAll(text)
	time.Sleep(100 * time.Millisecond)

	robotgo.KeyTap("v", "command")
}

func readPreviousCharacter() string {
	previousClipboard, _ := clipboard.ReadAll()
	defer clipboard.WriteAll(previousClipboard)

	clipboard.WriteAll("")
	time.Sleep(40 * time.Millisecond)

	robotgo.KeyTap("left", "shift")
	time.Sleep(40 * time.Millisecond)

	robotgo.KeyTap("c", "command")
	time.Sleep(100 * time.Millisecond)

	selected, _ := clipboard.ReadAll()
	robotgo.KeyTap("right")
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
	for i := 0; i < w.insertedRunes; i++ {
		robotgo.KeyTap("backspace")
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}
