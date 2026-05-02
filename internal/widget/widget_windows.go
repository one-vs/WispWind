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
	procRoundRectRgn   = gdi32.NewProc("CreateRoundRectRgn")
	procSetWindowRgn   = user32.NewProc("SetWindowRgn")
	procSetLayeredAttr = user32.NewProc("SetLayeredWindowAttributes")
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
		HbrBackground: win.HBRUSH(createBrush(rgb(43, 43, 43))),
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
	setLayeredAlpha(hwnd, 246)
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

	bg := createBrush(rgb(43, 43, 43))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(bg))
	w := currentWidth()
	h := currentHeight()
	win.RoundRect(hdc, 0, 0, w, h, h, h)
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
	win.SetTextColor(hdc, win.RGB(235, 235, 235))
	if wide {
		if status == "processing" {
			drawProcessingWave(hdc, w, h, elapsed)
		} else {
			drawWaveform(hdc, levels, levelAt, w, h)
			win.SetTextColor(hdc, win.RGB(220, 220, 220))
			drawText(hdc, w-44, 14, formatElapsed(elapsed))
		}
	} else {
		drawIdleGlyph(hdc, h)
	}
}

func drawWaveform(hdc win.HDC, levels []float64, levelAt int, w, h int32) {
	white := createBrush(rgb(238, 238, 238))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(white))
	defer func() {
		win.SelectObject(hdc, oldBrush)
		win.DeleteObject(win.HGDIOBJ(white))
	}()

	baseY := h / 2
	left := int32(18)
	right := int32(w - 62)
	barCount := int32(len(levels))
	step := float64(right-left) / float64(barCount)
	for i := int32(0); i < barCount; i++ {
		idx := (levelAt + int(i)) % len(levels)
		lvl := math.Max(0.025, math.Min(1, levels[idx]*7))
		barH := int32(2 + lvl*28)
		x := left + int32(float64(i)*step)
		win.Rectangle_(hdc, x, baseY-barH/2, x+2, baseY+barH/2)
	}
}

func drawIdleGlyph(hdc win.HDC, h int32) {
	white := createBrush(rgb(238, 238, 238))
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
	white := createBrush(rgb(238, 238, 238))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(white))
	defer func() {
		win.SelectObject(hdc, oldBrush)
		win.DeleteObject(win.HGDIOBJ(white))
	}()

	baseY := h / 2
	left := int32(22)
	right := int32(w - 22)
	barCount := int32(56)
	step := float64(right-left) / float64(barCount)
	phase := elapsed.Seconds() * 5.0
	for i := int32(0); i < barCount; i++ {
		x := left + int32(float64(i)*step)
		t := float64(i)*0.42 - phase
		lvl := 0.22 + 0.78*(math.Sin(t)+1)/2
		envelope := math.Sin(float64(i) / float64(barCount-1) * math.Pi)
		barH := int32(4 + lvl*envelope*30)
		win.Rectangle_(hdc, x, baseY-barH/2, x+2, baseY+barH/2)
	}
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

func createBrush(color uint32) win.HBRUSH {
	ret, _, _ := procCreateBrush.Call(uintptr(color))
	return win.HBRUSH(ret)
}

func rgb(r, g, b byte) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}
