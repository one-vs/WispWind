//go:build darwin

package paste

import (
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework AppKit
#cgo CFLAGS: -x objective-c

#include <ApplicationServices/ApplicationServices.h>
#import <AppKit/AppKit.h>
#include <string.h>
#include <stdlib.h>

static void wispwind_set_clipboard_text(const char *utf8) {
    @autoreleasepool {
        NSString *text = [NSString stringWithUTF8String:utf8];
        if (!text) {
            text = @"";
        }
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];
        [pasteboard setString:text forType:NSPasteboardTypeString];
    }
}

static long wispwind_prev_char_from_element(AXUIElementRef focused, int *err_out) {
    CFBooleanRef yes = kCFBooleanTrue;
    AXUIElementSetAttributeValue(focused, CFSTR("AXManualAccessibility"), yes);
    AXUIElementSetAttributeValue(focused, CFSTR("AXEnhancedUserInterface"), yes);

    CFTypeRef rangeRef = NULL;
    AXError err = AXUIElementCopyAttributeValue(focused, kAXSelectedTextRangeAttribute, &rangeRef);
    if (err != kAXErrorSuccess || !rangeRef) {
        CFRelease(focused);
        if (err_out) *err_out = 3;
        return 0;
    }

    CFRange range = {0, 0};
    Boolean got = AXValueGetValue((AXValueRef)rangeRef, kAXValueCFRangeType, &range);
    CFRelease(rangeRef);
    if (!got) { CFRelease(focused); if (err_out) *err_out = 4; return 0; }
    if (range.location <= 0) { CFRelease(focused); if (err_out) *err_out = 5; return 0; }
    if (range.length != 0) { CFRelease(focused); if (err_out) *err_out = 6; return 0; }

    CFRange prevRange = CFRangeMake(range.location - 1, 1);
    AXValueRef prevRangeRef = AXValueCreate(kAXValueCFRangeType, &prevRange);
    if (prevRangeRef) {
        CFTypeRef strRef = NULL;
        err = AXUIElementCopyParameterizedAttributeValue(
            focused,
            CFSTR("AXStringForRange"),
            prevRangeRef,
            &strRef
        );
        CFRelease(prevRangeRef);
        if (err == kAXErrorSuccess && strRef) {
            if (CFGetTypeID(strRef) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)strRef) > 0) {
                UniChar c = CFStringGetCharacterAtIndex((CFStringRef)strRef, 0);
                CFRelease(strRef);
                CFRelease(focused);
                return (long)c;
            }
            CFRelease(strRef);
        }
    }

    CFTypeRef valueRef = NULL;
    err = AXUIElementCopyAttributeValue(focused, kAXValueAttribute, &valueRef);
    CFRelease(focused);
    if (err != kAXErrorSuccess || !valueRef) { if (err_out) *err_out = 7; return 0; }
    if (CFGetTypeID(valueRef) != CFStringGetTypeID()) {
        CFRelease(valueRef);
        if (err_out) *err_out = 8;
        return 0;
    }

    CFStringRef str = (CFStringRef)valueRef;
    CFIndex len = CFStringGetLength(str);
    long result = 0;
    if (range.location <= len) {
        UniChar c = CFStringGetCharacterAtIndex(str, range.location - 1);
        result = (long)c;
    }
    CFRelease(valueRef);
    return result;
}

static long wispwind_prev_char_debug(int *err_out) {
    if (err_out) *err_out = 0;
    AXUIElementRef sys = AXUIElementCreateSystemWide();
    if (!sys) { if (err_out) *err_out = 1; return 0; }

    AXUIElementRef focused = NULL;
    AXError err = AXUIElementCopyAttributeValue(sys, kAXFocusedUIElementAttribute, (CFTypeRef *)&focused);
    CFRelease(sys);
    if (err != kAXErrorSuccess || !focused) { if (err_out) *err_out = 2; return 0; }

    return wispwind_prev_char_from_element(focused, err_out);
}

static long wispwind_prev_char_for_pid_debug(int pid, int *err_out) {
    if (err_out) *err_out = 0;
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) { if (err_out) *err_out = 10; return 0; }

    CFBooleanRef yes = kCFBooleanTrue;
    AXUIElementSetAttributeValue(app, CFSTR("AXManualAccessibility"), yes);
    AXUIElementSetAttributeValue(app, CFSTR("AXEnhancedUserInterface"), yes);

    AXUIElementRef focused = NULL;
    AXError err = AXUIElementCopyAttributeValue(app, kAXFocusedUIElementAttribute, (CFTypeRef *)&focused);
    CFRelease(app);
    if (err != kAXErrorSuccess || !focused) { if (err_out) *err_out = 11; return 0; }

    return wispwind_prev_char_from_element(focused, err_out);
}

static long wispwind_prev_char(void) {
    return wispwind_prev_char_debug(NULL);
}

static void wispwind_release_modifiers(void) {
    CGEventRef e;
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);

    int codes[] = {59, 60, 56, 60, 55, 54, 58, 61, 63};
    for (int i = 0; i < (int)(sizeof(codes)/sizeof(codes[0])); i++) {
        e = CGEventCreateKeyboardEvent(src, (CGKeyCode)codes[i], false);
        if (e) {
            CGEventPost(kCGHIDEventTap, e);
            CFRelease(e);
        }
    }

    if (src) CFRelease(src);
}

static void wispwind_paste_cmd_v(void) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);

    CGEventRef cmdDown = CGEventCreateKeyboardEvent(src, (CGKeyCode)55, true);
    CGEventRef vDown   = CGEventCreateKeyboardEvent(src, (CGKeyCode)9,  true);
    CGEventRef vUp     = CGEventCreateKeyboardEvent(src, (CGKeyCode)9,  false);
    CGEventRef cmdUp   = CGEventCreateKeyboardEvent(src, (CGKeyCode)55, false);

    CGEventSetFlags(vDown, kCGEventFlagMaskCommand);
    CGEventSetFlags(vUp,   kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, cmdDown);
    usleep(15000);
    CGEventPost(kCGHIDEventTap, vDown);
    usleep(30000);
    CGEventPost(kCGHIDEventTap, vUp);
    usleep(15000);
    CGEventPost(kCGHIDEventTap, cmdUp);

    if (cmdDown) CFRelease(cmdDown);
    if (vDown)   CFRelease(vDown);
    if (vUp)     CFRelease(vUp);
    if (cmdUp)   CFRelease(cmdUp);
    if (src)     CFRelease(src);
}

static void wispwind_tap_backspace(void) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
    CGEventRef down = CGEventCreateKeyboardEvent(src, (CGKeyCode)51, true);
    CGEventRef up   = CGEventCreateKeyboardEvent(src, (CGKeyCode)51, false);
    CGEventPost(kCGHIDEventTap, down);
    usleep(3000);
    CGEventPost(kCGHIDEventTap, up);
    if (down) CFRelease(down);
    if (up)   CFRelease(up);
    if (src)  CFRelease(src);
}
*/
import "C"

func Init() error {
	return nil
}

type SmartContext struct {
	Prev    rune
	HasPrev bool
	Err     int
}

func CaptureContext() SmartContext {
	var cErr C.int
	code := int64(C.wispwind_prev_char_debug(&cErr))
	ctx := SmartContext{Err: int(cErr)}
	if code > 0 {
		ctx.Prev = rune(code)
		ctx.HasPrev = true
	}
	log.Printf("smart-paste: captured prev_char code=%d err=%d", code, ctx.Err)
	return ctx
}

func CaptureContextForPID(pid int32) SmartContext {
	var cErr C.int
	code := int64(C.wispwind_prev_char_for_pid_debug(C.int(pid), &cErr))
	if code <= 0 {
		time.Sleep(80 * time.Millisecond)
		code = int64(C.wispwind_prev_char_for_pid_debug(C.int(pid), &cErr))
	}
	if code <= 0 {
		code = int64(C.wispwind_prev_char_debug(&cErr))
	}
	ctx := SmartContext{Err: int(cErr)}
	if code > 0 {
		ctx.Prev = rune(code)
		ctx.HasPrev = true
	}
	log.Printf("smart-paste: captured prev_char pid=%d code=%d err=%d", pid, code, ctx.Err)
	return ctx
}

func PasteText(text string) {
	if text == "" {
		return
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	C.wispwind_set_clipboard_text(cText)
	time.Sleep(120 * time.Millisecond)

	C.wispwind_release_modifiers()
	time.Sleep(80 * time.Millisecond)

	C.wispwind_paste_cmd_v()
}

func PasteTextSmart(text string) {
	PasteTextSmartWithContext(text, CaptureContext())
}

func PasteTextSmartWithContext(text string, ctx SmartContext) {
	if text == "" {
		return
	}
	if needsLeadingSpace(text, ctx) {
		text = " " + text
	}
	PasteText(text)
}

func needsLeadingSpace(newText string, ctx SmartContext) bool {
	newText = strings.TrimLeft(newText, " \t")
	if newText == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(newText)
	switch first {
	case '.', ',', '!', '?', ':', ';', ')', ']', '}', '\'', '"':
		return false
	}

	if !ctx.HasPrev {
		return false
	}
	prev := ctx.Prev
	if unicode.IsSpace(prev) {
		return false
	}
	if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
		return true
	}
	switch prev {
	case '.', '!', '?', ':', ';', ',', ')', ']', '}', '"', '\'', '-', '–', '—':
		return true
	}
	return false
}

func HasSelection() bool {
	return false
}

func PasteTextWithOptions(text string, smartSpacing bool) {
	if smartSpacing {
		PasteTextSmart(text)
		return
	}
	PasteText(text)
}

type LiveWriter struct {
	insertedRunes int
	hasInserted   bool
	smartContext  SmartContext
}

func NewLiveWriter() *LiveWriter {
	return &LiveWriter{}
}

func (w *LiveWriter) SetSmartContext(ctx SmartContext) {
	w.smartContext = ctx
}

func (w *LiveWriter) Replace(text string) {
	hadInserted := w.hasInserted
	w.erase()
	if !hadInserted {
		PasteTextSmartWithContext(text, w.smartContext)
	} else {
		PasteText(text)
	}
	w.insertedRunes = utf8.RuneCountInString(text)
	w.hasInserted = true
}

func (w *LiveWriter) Clear() {
	w.erase()
	w.insertedRunes = 0
	w.hasInserted = false
	w.smartContext = SmartContext{}
}

func (w *LiveWriter) Forget() {
	w.insertedRunes = 0
	w.hasInserted = false
	w.smartContext = SmartContext{}
}

func (w *LiveWriter) erase() {
	if w.insertedRunes <= 0 {
		return
	}
	for i := 0; i < w.insertedRunes; i++ {
		C.wispwind_tap_backspace()
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}
