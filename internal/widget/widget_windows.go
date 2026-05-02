package widget

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

const (
	compactWidth  = 32
	compactHeight = 24
	wideWidth     = 360
	wideHeight    = 44
)

var (
	gdi32              = syscall.NewLazyDLL("gdi32.dll")
	user32             = syscall.NewLazyDLL("user32.dll")
	procCreateBrush    = gdi32.NewProc("CreateSolidBrush")
	procCreatePen      = gdi32.NewProc("CreatePen")
	procRoundRectRgn   = gdi32.NewProc("CreateRoundRectRgn")
	procSetWindowRgn   = user32.NewProc("SetWindowRgn")
	procSetLayeredAttr = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowComp  = user32.NewProc("SetWindowCompositionAttribute")
)

var overlay = &state{
	levels: make([]float64, 80),
	status: "idle",
}

type state struct {
	mu      sync.Mutex
	hwnd    win.HWND
	levels  []float64
	levelAt int
	status  string
	started time.Time
	visible bool
	wide    bool
	width   int32
	height  int32
	ready   chan struct{}
}

func Start() {
	overlay.mu.Lock()
	if overlay.ready != nil {
		overlay.mu.Unlock()
		return
	}
	overlay.ready = make(chan struct{})
	overlay.mu.Unlock()

	go run()
}

func ShowIdle() {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.status = "idle"
	overlay.visible = true
	hwnd := overlay.hwnd
	overlay.mu.Unlock()
	resize(hwnd, compactWidth, compactHeight, false)
	moveNearCursor(hwnd)
	win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
	redraw(hwnd)
}

func Show(status string) {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.status = status
	overlay.started = time.Now()
	overlay.visible = true
	for i := range overlay.levels {
		overlay.levels[i] = 0
	}
	hwnd := overlay.hwnd
	overlay.mu.Unlock()
	resize(hwnd, wideWidth, wideHeight, true)
	moveNearCursor(hwnd)
	win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
	redraw(hwnd)
}

func Hide() {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.status = "idle"
	overlay.visible = false
	hwnd := overlay.hwnd
	overlay.mu.Unlock()
	win.ShowWindow(hwnd, win.SW_HIDE)
}

// moveNearCursor positions the overlay just below the mouse cursor,
// keeping it within the virtual screen bounds (all monitors).
func moveNearCursor(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return
	}

	// Virtual screen bounds (covers all monitors)
	vx := int32(win.GetSystemMetrics(win.SM_XVIRTUALSCREEN))
	vy := int32(win.GetSystemMetrics(win.SM_YVIRTUALSCREEN))
	vw := int32(win.GetSystemMetrics(win.SM_CXVIRTUALSCREEN))
	vh := int32(win.GetSystemMetrics(win.SM_CYVIRTUALSCREEN))

	// Offset slightly so it doesn't cover the cursor itself
	x := pt.X + 16
	y := pt.Y + 16

	// Clamp to virtual screen
	var rect win.RECT
	win.GetWindowRect(hwnd, &rect)
	w := rect.Right - rect.Left
	h := rect.Bottom - rect.Top

	if x+w > vx+vw {
		x = vx + vw - w - 4
	}
	if y+h > vy+vh {
		y = vy + vh - h - 4
	}
	if x < vx {
		x = vx + 4
	}
	if y < vy {
		y = vy + 4
	}

	win.SetWindowPos(hwnd, win.HWND_TOPMOST, x, y, 0, 0,
		win.SWP_NOACTIVATE|win.SWP_NOSIZE|win.SWP_SHOWWINDOW)
}

func SetStatus(status string) {
	overlay.mu.Lock()
	overlay.status = status
	hwnd := overlay.hwnd
	visible := overlay.visible
	overlay.mu.Unlock()
	if visible {
		redraw(hwnd)
	}
}

func SetLevel(level float64) {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	overlay.mu.Lock()
	overlay.levels[overlay.levelAt] = level
	overlay.levelAt = (overlay.levelAt + 1) % len(overlay.levels)
	hwnd := overlay.hwnd
	visible := overlay.visible
	overlay.mu.Unlock()
	if visible {
		redraw(hwnd)
	}
}

func run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className := syscall.StringToUTF16Ptr("WispWindOverlay")
	instance := win.GetModuleHandle(nil)
	wndProc := syscall.NewCallback(windowProc)

	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   wndProc,
		HInstance:     instance,
		HbrBackground: win.HBRUSH(createBrush(rgb(24, 24, 26))),
		LpszClassName: className,
	}
	win.RegisterClassEx(&wc)

	x := (win.GetSystemMetrics(win.SM_CXSCREEN) - compactWidth) / 2
	y := win.GetSystemMetrics(win.SM_CYSCREEN) - compactHeight - 88
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW|win.WS_EX_LAYERED|win.WS_EX_NOACTIVATE,
		className,
		syscall.StringToUTF16Ptr("WispWind"),
		win.WS_POPUP,
		x, y, compactWidth, compactHeight,
		0, 0, instance, nil,
	)
	setLayeredAlpha(hwnd, 235)
	setRoundedRegion(hwnd)

	overlay.mu.Lock()
	overlay.hwnd = hwnd
	overlay.width = compactWidth
	overlay.height = compactHeight
	close(overlay.ready)
	overlay.mu.Unlock()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			overlay.mu.Lock()
			h := overlay.hwnd
			visible := overlay.visible
			overlay.mu.Unlock()
			if visible {
				redraw(h)
			}
		}
	}()

	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) != 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
}

func windowProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_NCHITTEST:
		return win.HTCAPTION
	case win.WM_PAINT:
		paint(hwnd)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func paint(hwnd win.HWND) {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(hwnd, &ps)
	defer win.EndPaint(hwnd, &ps)

	bg := createBrush(rgb(24, 24, 26))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(bg))
	pen := createPen(rgb(24, 24, 26))
	oldPen := win.SelectObject(hdc, win.HGDIOBJ(pen))
	w := currentWidth()
	h := currentHeight()
	win.RoundRect(hdc, 0, 0, w+1, h+1, h, h)
	win.SelectObject(hdc, oldPen)
	win.DeleteObject(win.HGDIOBJ(pen))
	win.SelectObject(hdc, oldBrush)
	win.DeleteObject(win.HGDIOBJ(bg))

	overlay.mu.Lock()
	levels := append([]float64(nil), overlay.levels...)
	levelAt := overlay.levelAt
	status := overlay.status
	elapsed := time.Since(overlay.started)
	wide := overlay.wide
	w = overlay.width
	h = overlay.height
	overlay.mu.Unlock()

	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, win.RGB(175, 175, 175))
	if wide {
		if status == "processing" {
			drawProcessingWave(hdc, w, h, elapsed)
		} else {
			drawWaveform(hdc, levels, levelAt, w, h)
			win.SetTextColor(hdc, win.RGB(165, 165, 165))
			drawText(hdc, w-44, 14, formatElapsed(elapsed))
		}
	} else {
		drawIdleGlyph(hdc, h)
	}
}

func drawWaveform(hdc win.HDC, levels []float64, levelAt int, w, h int32) {
	wave := createBrush(rgb(232, 232, 232))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(wave))
	defer func() {
		win.SelectObject(hdc, oldBrush)
		win.DeleteObject(win.HGDIOBJ(wave))
	}()

	baseY := h / 2
	left := int32(22)
	right := int32(w - 66)
	barCount := int32(52)
	step := float64(right-left) / float64(barCount)
	for i := int32(0); i < barCount; i++ {
		idx := (levelAt + int(i*int32(len(levels))/barCount)) % len(levels)
		if levels[idx] < 0.008 {
			continue
		}
		lvl := smoothLevel(levels, idx)
		lvl = math.Max(0.08, math.Min(1, lvl*7))
		barH := int32(4 + lvl*22)
		x := left + int32(float64(i)*step)
		drawRoundBar(hdc, x, baseY-barH/2, 3, barH)
	}
}

func drawIdleGlyph(hdc win.HDC, h int32) {
	white := createBrush(rgb(255, 255, 255))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(white))
	defer func() {
		win.SelectObject(hdc, oldBrush)
		win.DeleteObject(win.HGDIOBJ(white))
	}()

	baseY := h / 2
	heights := []int32{6, 12, 6}
	for i, barH := range heights {
		x := int32(10 + i*5)
		win.Rectangle_(hdc, x, baseY-barH/2, x+2, baseY+barH/2)
	}
}

func drawProcessingWave(hdc win.HDC, w, h int32, elapsed time.Duration) {
	wave := createBrush(rgb(232, 232, 232))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(wave))
	defer func() {
		win.SelectObject(hdc, oldBrush)
		win.DeleteObject(win.HGDIOBJ(wave))
	}()

	baseY := h / 2
	left := int32(26)
	right := int32(w - 26)
	barCount := int32(48)
	step := float64(right-left) / float64(barCount)
	phase := elapsed.Seconds() * 4.2
	for i := int32(0); i < barCount; i++ {
		x := left + int32(float64(i)*step)
		t := float64(i)*0.36 - phase
		lvl := 0.18 + 0.82*(math.Sin(t)+1)/2
		envelope := math.Sin(float64(i) / float64(barCount-1) * math.Pi)
		barH := int32(4 + lvl*envelope*24)
		drawRoundBar(hdc, x, baseY-barH/2, 3, barH)
	}
}

func smoothLevel(levels []float64, idx int) float64 {
	prev := levels[(idx-1+len(levels))%len(levels)]
	curr := levels[idx]
	next := levels[(idx+1)%len(levels)]
	return prev*0.25 + curr*0.5 + next*0.25
}

func drawRoundBar(hdc win.HDC, x, y, w, h int32) {
	if h < w {
		h = w
	}
	win.RoundRect(hdc, x, y, x+w, y+h, w, w)
}

func drawText(hdc win.HDC, x, y int32, text string) {
	u := syscall.StringToUTF16(text)
	if len(u) == 0 {
		return
	}
	win.TextOut(hdc, x, y, &u[0], int32(len(u)-1))
}

func formatElapsed(d time.Duration) string {
	seconds := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func redraw(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	win.InvalidateRect(hwnd, nil, true)
}

func setRoundedRegion(hwnd win.HWND) {
	overlay.mu.Lock()
	w := overlay.width
	h := overlay.height
	if w == 0 {
		w = compactWidth
	}
	if h == 0 {
		h = compactHeight
	}
	overlay.mu.Unlock()
	rgn, _, _ := procRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), uintptr(h), uintptr(h))
	if rgn != 0 {
		procSetWindowRgn.Call(uintptr(hwnd), rgn, 1)
	}
}

func resize(hwnd win.HWND, w, h int32, wide bool) {
	if hwnd == 0 {
		return
	}
	overlay.mu.Lock()
	if overlay.width == w && overlay.height == h && overlay.wide == wide {
		overlay.mu.Unlock()
		return
	}
	overlay.width = w
	overlay.height = h
	overlay.wide = wide
	overlay.mu.Unlock()

	var rect win.RECT
	win.GetWindowRect(hwnd, &rect)
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, rect.Left, rect.Top, w, h, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	setRoundedRegion(hwnd)
}

func currentWidth() int32 {
	overlay.mu.Lock()
	defer overlay.mu.Unlock()
	if overlay.width == 0 {
		return compactWidth
	}
	return overlay.width
}

func currentHeight() int32 {
	overlay.mu.Lock()
	defer overlay.mu.Unlock()
	if overlay.height == 0 {
		return compactHeight
	}
	return overlay.height
}

func setLayeredAlpha(hwnd win.HWND, alpha byte) {
	const lwaAlpha = 0x2
	procSetLayeredAttr.Call(uintptr(hwnd), 0, uintptr(alpha), lwaAlpha)
}

func setBlurBehind(hwnd win.HWND) {
	// Undocumented but stable Windows 10/11 API for acrylic/blur effect
	type accentPolicy struct {
		AccentState   uint32
		AccentFlags   uint32
		GradientColor uint32
		AnimationId   uint32
	}
	type wndCompData struct {
		Attrib uint32
		PVData unsafe.Pointer
		CbData uint32
	}
	const wcaAccentPolicy = 19
	const accentEnableBlurBehind = 3
	accent := accentPolicy{
		AccentState:   accentEnableBlurBehind,
		GradientColor: 0x80202020, // dark grey tint (AABBGGRR)
	}
	data := wndCompData{
		Attrib: wcaAccentPolicy,
		PVData: unsafe.Pointer(&accent),
		CbData: uint32(unsafe.Sizeof(accent)),
	}
	procSetWindowComp.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&data)))
}

func createBrush(color uint32) win.HBRUSH {
	ret, _, _ := procCreateBrush.Call(uintptr(color))
	return win.HBRUSH(ret)
}

func createPen(color uint32) win.HPEN {
	const psSolid = 0
	ret, _, _ := procCreatePen.Call(psSolid, 1, uintptr(color))
	return win.HPEN(ret)
}

func rgb(r, g, b byte) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}
