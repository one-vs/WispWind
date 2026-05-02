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

	robotgo.KeyToggle("control", "down")
	robotgo.KeyTap("c")
	robotgo.KeyToggle("control", "up")
	time.Sleep(70 * time.Millisecond)

	selected, _ := clipboard.ReadAll()
	return selected != ""
}

func PasteTextWithOptions(text string, smartSpacing bool) {
	if text == "" {
		return
	}
	robotgo.KeyToggle("control", "up")
	if smartSpacing && needsLeadingSpace(readPreviousCharacter()) {
		text = " " + text
	}

	// Пишем текст в буфер обмена
	clipboard.WriteAll(text)

	// Небольшая задержка, чтобы ОС успела обновить буфер
	time.Sleep(200 * time.Millisecond)

	// Явно зажимаем Control
	robotgo.KeyToggle("control", "down")
	time.Sleep(50 * time.Millisecond)

	// Нажимаем V
	robotgo.KeyTap("v")
	time.Sleep(50 * time.Millisecond)

	// Отпускаем Control
	robotgo.KeyToggle("control", "up")
}

func readPreviousCharacter() string {
	previousClipboard, _ := clipboard.ReadAll()
	defer clipboard.WriteAll(previousClipboard)

	clipboard.WriteAll("")
	time.Sleep(40 * time.Millisecond)

	robotgo.KeyToggle("shift", "down")
	robotgo.KeyTap("left")
	robotgo.KeyToggle("shift", "up")
	time.Sleep(30 * time.Millisecond)

	robotgo.KeyToggle("control", "down")
	robotgo.KeyTap("c")
	robotgo.KeyToggle("control", "up")
	time.Sleep(70 * time.Millisecond)

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
	robotgo.KeyToggle("control", "up")
	for i := 0; i < w.insertedRunes; i++ {
		robotgo.KeyTap("backspace")
	}
	time.Sleep(50 * time.Millisecond)
}
