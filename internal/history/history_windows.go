package history

// Popup panel with recent dictations. Opened by hotkey; a click on a row
// copies the full text to the clipboard, Esc or losing focus closes it.

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/atotto/clipboard"
	"github.com/lxn/win"
)

type Item struct {
	Time time.Time
	Text string
}

const (
	panelW     = 520
	rowH       = 74
	headerH    = 34
	maxRows    = 7
	pad        = 12
	copiedHold = 650 * time.Millisecond
)

var (
	gdi32               = syscall.NewLazyDLL("gdi32.dll")
	user32              = syscall.NewLazyDLL("user32.dll")
	procCreateRoundRgn  = gdi32.NewProc("CreateRoundRectRgn")
	procCreateSolidBrsh = gdi32.NewProc("CreateSolidBrush")
	procCreatePen       = gdi32.NewProc("CreatePen")
	procSetWindowRgn    = user32.NewProc("SetWindowRgn")
	procFillRect        = user32.NewProc("FillRect")

	mu     sync.Mutex
	opened bool

	classOnce sync.Once
	className = syscall.StringToUTF16Ptr("WispWindHistory")
)

type panel struct {
	hwnd     win.HWND
	items    []Item
	hover    int
	copied   int // row index flashed as "copied", -1 none
	fontMain win.HFONT
	fontTime win.HFONT
}

var current *panel // accessed only on the panel thread via pointer in GWLP? keep simple: package var guarded by mu

// Toggle opens the panel with the given items, or closes it if already open.
func Toggle(items []Item) {
	mu.Lock()
	if opened {
		mu.Unlock()
		Close()
		return
	}
	opened = true
	mu.Unlock()

	go runPanel(items)
}

func Close() {
	mu.Lock()
	p := current
	mu.Unlock()
	if p != nil && p.hwnd != 0 {
		win.PostMessage(p.hwnd, win.WM_CLOSE, 0, 0)
	}
}

func runPanel(items []Item) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		mu.Lock()
		opened = false
		current = nil
		mu.Unlock()
	}()

	if len(items) > maxRows {
		items = items[:maxRows]
	}

	p := &panel{items: items, hover: -1, copied: -1}
	mu.Lock()
	current = p
	mu.Unlock()

	instance := win.GetModuleHandle(nil)
	classOnce.Do(func() {
		wc := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   syscall.NewCallback(panelProc),
			HInstance:     instance,
			HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
			LpszClassName: className,
		}
		win.RegisterClassEx(&wc)
	})

	h := int32(headerH + pad)
	if len(items) == 0 {
		h += rowH
	} else {
		h += int32(len(items) * rowH)
	}
	w := int32(panelW)

	x, y := placeNearCursor(w, h)
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW,
		className,
		syscall.StringToUTF16Ptr("WispWind History"),
		win.WS_POPUP,
		x, y, w, h,
		0, 0, instance, nil,
	)
	p.hwnd = hwnd

	// Rounded corners.
	rgn, _, _ := procCreateRoundRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), 14, 14)
	if rgn != 0 {
		procSetWindowRgn.Call(uintptr(hwnd), rgn, 1)
	}

	p.fontMain = makeFont(16, false)
	p.fontTime = makeFont(13, false)

	win.ShowWindow(hwnd, win.SW_SHOW)
	win.SetForegroundWindow(hwnd)

	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}

	win.DeleteObject(win.HGDIOBJ(p.fontMain))
	win.DeleteObject(win.HGDIOBJ(p.fontTime))
}

func placeNearCursor(w, h int32) (int32, int32) {
	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return 200, 200
	}
	x := pt.X - w/2
	y := pt.Y + 14
	sw := int32(win.GetSystemMetrics(win.SM_CXSCREEN))
	sh := int32(win.GetSystemMetrics(win.SM_CYSCREEN))
	if x+w > sw-8 {
		x = sw - w - 8
	}
	if x < 8 {
		x = 8
	}
	if y+h > sh-8 {
		y = pt.Y - h - 14
	}
	if y < 8 {
		y = 8
	}
	return x, y
}

func makeFont(size int32, bold bool) win.HFONT {
	lf := win.LOGFONT{
		LfHeight:  -size,
		LfQuality: win.CLEARTYPE_QUALITY,
	}
	if bold {
		lf.LfWeight = win.FW_SEMIBOLD
	} else {
		lf.LfWeight = win.FW_NORMAL
	}
	name := syscall.StringToUTF16("Segoe UI")
	copy(lf.LfFaceName[:], name)
	return win.CreateFontIndirect(&lf)
}

func getPanel() *panel {
	mu.Lock()
	defer mu.Unlock()
	return current
}

func panelProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	p := getPanel()
	if p == nil {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win.WM_PAINT:
		p.paint(hwnd)
		return 0
	case win.WM_MOUSEMOVE:
		y := int32(int16(uint16(lParam >> 16)))
		idx := p.rowAt(y)
		if idx != p.hover {
			p.hover = idx
			win.InvalidateRect(hwnd, nil, false)
		}
		return 0
	case win.WM_LBUTTONUP:
		y := int32(int16(uint16(lParam >> 16)))
		idx := p.rowAt(y)
		if idx >= 0 && idx < len(p.items) {
			_ = clipboard.WriteAll(p.items[idx].Text)
			p.copied = idx
			win.InvalidateRect(hwnd, nil, false)
			go func() {
				time.Sleep(copiedHold)
				win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
			}()
		}
		return 0
	case win.WM_KEYDOWN:
		if wParam == win.VK_ESCAPE {
			win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
		}
		return 0
	case win.WM_ACTIVATE:
		if wParam&0xFFFF == 0 { // WA_INACTIVE: clicked elsewhere
			win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
		}
		return 0
	case win.WM_CLOSE:
		win.DestroyWindow(hwnd)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func (p *panel) rowAt(y int32) int {
	idx := int((y - headerH) / rowH)
	if y < headerH || idx < 0 || idx >= len(p.items) {
		return -1
	}
	return idx
}

func colorRef(r, g, b byte) win.COLORREF {
	return win.COLORREF(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
}

func fillRect(hdc win.HDC, x0, y0, x1, y1 int32, c win.COLORREF) {
	brush, _, _ := procCreateSolidBrsh.Call(uintptr(c))
	r := win.RECT{Left: x0, Top: y0, Right: x1, Bottom: y1}
	procFillRect.Call(uintptr(hdc), uintptr(unsafe.Pointer(&r)), brush)
	win.DeleteObject(win.HGDIOBJ(brush))
}

func textOut(hdc win.HDC, x, y int32, s string) {
	u := syscall.StringToUTF16(s)
	if len(u) <= 1 {
		return
	}
	win.TextOut(hdc, x, y, &u[0], int32(len(u)-1))
}

// ellipsize trims s to fit maxPx using actual text measurement.
func ellipsize(hdc win.HDC, s string, maxPx int32) string {
	runes := []rune(s)
	for len(runes) > 0 {
		u := syscall.StringToUTF16(string(runes))
		var sz win.SIZE
		win.GetTextExtentPoint32(hdc, &u[0], int32(len(u)-1), &sz)
		if sz.CX <= maxPx {
			break
		}
		// shrink proportionally, at least one rune
		next := int(float64(len(runes)) * float64(maxPx) / float64(sz.CX))
		if next >= len(runes) {
			next = len(runes) - 1
		}
		runes = runes[:next]
		s = string(runes) + "…"
		u2 := syscall.StringToUTF16(s)
		var sz2 win.SIZE
		win.GetTextExtentPoint32(hdc, &u2[0], int32(len(u2)-1), &sz2)
		if sz2.CX <= maxPx {
			return s
		}
	}
	if len(runes) == 0 {
		return ""
	}
	return string(runes)
}

func (p *panel) paint(hwnd win.HWND) {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(hwnd, &ps)
	defer win.EndPaint(hwnd, &ps)

	var rc win.RECT
	win.GetClientRect(hwnd, &rc)
	w, h := rc.Right, rc.Bottom

	memDC := win.CreateCompatibleDC(hdc)
	bmp := win.CreateCompatibleBitmap(hdc, w, h)
	oldBmp := win.SelectObject(memDC, win.HGDIOBJ(bmp))
	defer func() {
		win.BitBlt(hdc, 0, 0, w, h, memDC, 0, 0, win.SRCCOPY)
		win.SelectObject(memDC, oldBmp)
		win.DeleteObject(win.HGDIOBJ(bmp))
		win.DeleteDC(memDC)
	}()

	bg := colorRef(28, 28, 31)
	fillRect(memDC, 0, 0, w, h, bg)
	win.SetBkMode(memDC, win.TRANSPARENT)

	// Header
	oldFont := win.SelectObject(memDC, win.HGDIOBJ(p.fontTime))
	win.SetTextColor(memDC, colorRef(140, 140, 145))
	textOut(memDC, pad, 9, "Последние диктовки — клик копирует, Esc закрывает")
	win.SelectObject(memDC, oldFont)

	if len(p.items) == 0 {
		win.SelectObject(memDC, win.HGDIOBJ(p.fontMain))
		win.SetTextColor(memDC, colorRef(120, 120, 125))
		textOut(memDC, pad, headerH+14, "История пуста")
		return
	}

	for i, item := range p.items {
		top := int32(headerH + i*rowH)
		// Card-style block per entry.
		blockColor := colorRef(38, 38, 43)
		if i == p.copied {
			blockColor = colorRef(34, 70, 44)
		} else if i == p.hover {
			blockColor = colorRef(50, 50, 56)
		}
		fillRoundRect(memDC, 8, top+3, w-8, top+rowH-3, 10, blockColor)

		win.SelectObject(memDC, win.HGDIOBJ(p.fontTime))
		if i == p.copied {
			win.SetTextColor(memDC, colorRef(120, 230, 140))
			textOut(memDC, pad+6, top+8, item.Time.Format("15:04")+"  ✓ скопировано")
		} else {
			win.SetTextColor(memDC, colorRef(130, 130, 136))
			textOut(memDC, pad+6, top+8, item.Time.Format("15:04"))
		}

		// Two lines of content for more context.
		win.SelectObject(memDC, win.HGDIOBJ(p.fontMain))
		win.SetTextColor(memDC, colorRef(222, 222, 226))
		line := oneLine(item.Text)
		maxPx := w - 2*pad - 12
		first := fitPrefix(memDC, line, maxPx)
		textOut(memDC, pad+6, top+26, first)
		if rest := trimLeft(line[len(first):]); rest != "" {
			textOut(memDC, pad+6, top+47, ellipsize(memDC, rest, maxPx))
		}
	}
}

func trimLeft(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	return s
}

// fitPrefix returns the longest prefix of s that fits maxPx, preferring to
// break at a space.
func fitPrefix(hdc win.HDC, s string, maxPx int32) string {
	runes := []rune(s)
	fit := len(runes)
	for fit > 0 {
		u := syscall.StringToUTF16(string(runes[:fit]))
		var sz win.SIZE
		win.GetTextExtentPoint32(hdc, &u[0], int32(len(u)-1), &sz)
		if sz.CX <= maxPx {
			break
		}
		next := int(float64(fit) * float64(maxPx) / float64(sz.CX))
		if next >= fit {
			next = fit - 1
		}
		fit = next
	}
	if fit <= 0 || fit >= len(runes) {
		return s[:len(string(runes[:fit]))]
	}
	// Prefer a word boundary within the last 12 runes.
	for k := fit; k > fit-12 && k > 0; k-- {
		if runes[k-1] == ' ' {
			return string(runes[:k])
		}
	}
	return string(runes[:fit])
}

func fillRoundRect(hdc win.HDC, x0, y0, x1, y1, rad int32, c win.COLORREF) {
	brush, _, _ := procCreateSolidBrsh.Call(uintptr(c))
	pen, _, _ := procCreatePen.Call(0, 1, uintptr(c))
	oldB := win.SelectObject(hdc, win.HGDIOBJ(brush))
	oldP := win.SelectObject(hdc, win.HGDIOBJ(pen))
	win.RoundRect(hdc, x0, y0, x1, y1, rad, rad)
	win.SelectObject(hdc, oldB)
	win.SelectObject(hdc, oldP)
	win.DeleteObject(win.HGDIOBJ(brush))
	win.DeleteObject(win.HGDIOBJ(pen))
}

func oneLine(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}
