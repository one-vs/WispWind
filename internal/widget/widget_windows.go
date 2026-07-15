package widget

// Overlay widget rendered with per-pixel alpha via UpdateLayeredWindow.
// All drawing happens in software into a premultiplied BGRA buffer, which
// gives antialiased rounded corners, a soft drop shadow and smooth easing
// animations — none of which classic GDI + SetWindowRgn can do.

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

const (
	// Content sizes at 96 DPI; scaled by the monitor DPI at show time.
	baseCompactW = 32
	baseCompactH = 24
	baseWideW    = 360
	baseWideH    = 54
	// Transparent margin around the content that hosts the drop shadow.
	baseMargin = 10

	levelCount = 80

	fadeInDuration = 140 * time.Millisecond
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	procMonitorFromPoint    = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	procSetDpiAwareness     = user32.NewProc("SetProcessDpiAwarenessContext")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procGetDpiForMonitor    = shcore.NewProc("GetDpiForMonitor")

	_ = kernel32
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type size struct {
	CX int32
	CY int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor win.RECT
	RcWork    win.RECT
	DwFlags   uint32
}

type state struct {
	mu      sync.Mutex
	hwnd    win.HWND
	ready   chan struct{}
	wake    chan struct{}
	visible bool
	wide    bool
	status  string
	started time.Time
	shownAt time.Time
	scale   float64
	width   int32 // full window size including shadow margin
	height  int32

	posX  int32
	posY  int32
	theme string

	levels  []float64 // raw RMS ring buffer
	levelAt int
	amp     float64 // eased on-screen wave amplitude
}

var overlay = &state{
	levels: make([]float64, levelCount),
	status: "idle",
	scale:  1,
	theme:  "green",
	wake:   make(chan struct{}, 1),
}

// SetTheme switches the wave color scheme (green, purple, yellow, red, blue).
func SetTheme(name string) {
	if _, ok := themes[name]; !ok {
		name = "green"
	}
	overlay.mu.Lock()
	overlay.theme = name
	overlay.mu.Unlock()
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
	show("idle", false)
}

func Show(status string) {
	show(status, true)
}

func show(status string, wide bool) {
	Start()
	<-overlay.ready

	scale := cursorScale()
	var w, h int32
	if wide {
		w, h = scaled(baseWideW, scale), scaled(baseWideH, scale)
	} else {
		w, h = scaled(baseCompactW, scale), scaled(baseCompactH, scale)
	}
	margin := scaled(baseMargin, scale)
	w += margin * 2
	h += margin * 2

	overlay.mu.Lock()
	overlay.status = status
	overlay.started = time.Now()
	overlay.shownAt = time.Now()
	overlay.visible = true
	overlay.wide = wide
	overlay.scale = scale
	overlay.width = w
	overlay.height = h
	for i := range overlay.levels {
		overlay.levels[i] = 0
	}
	overlay.amp = 0
	hwnd := overlay.hwnd
	overlay.mu.Unlock()

	x, y := positionNearCursor(w, h)
	overlay.mu.Lock()
	overlay.posX, overlay.posY = x, y
	overlay.mu.Unlock()
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, x, y, w, h,
		win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	poke()
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
	overlay.mu.Unlock()
	poke()
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
	overlay.mu.Unlock()
}

func poke() {
	select {
	case overlay.wake <- struct{}{}:
	default:
	}
}

func scaled(v int, scale float64) int32 {
	return int32(math.Round(float64(v) * scale))
}

// cursorScale returns the DPI scale factor of the monitor under the cursor.
func cursorScale() float64 {
	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return 1
	}
	const monitorDefaultToNearest = 2
	mon, _, _ := procMonitorFromPoint.Call(uintptr(pt.X), uintptr(pt.Y), monitorDefaultToNearest)
	if mon == 0 {
		return 1
	}
	if procGetDpiForMonitor.Find() != nil {
		return 1
	}
	var dpiX, dpiY uint32
	const mdtEffectiveDPI = 0
	ret, _, _ := procGetDpiForMonitor.Call(mon, mdtEffectiveDPI,
		uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if ret != 0 || dpiX == 0 {
		return 1
	}
	return float64(dpiX) / 96.0
}

// positionNearCursor computes a spot just below the cursor, clamped to the
// work area of the monitor the cursor is on (not the whole virtual screen,
// so the widget never jumps to another monitor).
func positionNearCursor(w, h int32) (int32, int32) {
	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return 100, 100
	}

	left := int32(win.GetSystemMetrics(win.SM_XVIRTUALSCREEN))
	top := int32(win.GetSystemMetrics(win.SM_YVIRTUALSCREEN))
	right := left + int32(win.GetSystemMetrics(win.SM_CXVIRTUALSCREEN))
	bottom := top + int32(win.GetSystemMetrics(win.SM_CYVIRTUALSCREEN))

	const monitorDefaultToNearest = 2
	mon, _, _ := procMonitorFromPoint.Call(uintptr(pt.X), uintptr(pt.Y), monitorDefaultToNearest)
	if mon != 0 {
		var mi monitorInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ret, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ret != 0 {
			left, top = mi.RcWork.Left, mi.RcWork.Top
			right, bottom = mi.RcWork.Right, mi.RcWork.Bottom
		}
	}

	x := pt.X + 16
	y := pt.Y + 18
	if x+w > right {
		x = right - w - 4
	}
	if y+h > bottom {
		y = pt.Y - h - 12 // flip above the cursor
	}
	if x < left {
		x = left + 4
	}
	if y < top {
		y = top + 4
	}
	return x, y
}

func run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Per-monitor v2 DPI awareness so coordinates and sizes are physical.
	const dpiAwarenessContextPerMonitorV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	if procSetDpiAwareness.Find() == nil {
		procSetDpiAwareness.Call(dpiAwarenessContextPerMonitorV2)
	}

	className := syscall.StringToUTF16Ptr("WispWindOverlay")
	instance := win.GetModuleHandle(nil)
	wndProc := syscall.NewCallback(windowProc)

	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   wndProc,
		HInstance:     instance,
		LpszClassName: className,
	}
	win.RegisterClassEx(&wc)

	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW|win.WS_EX_LAYERED|win.WS_EX_NOACTIVATE,
		className,
		syscall.StringToUTF16Ptr("WispWind"),
		win.WS_POPUP,
		0, 0, baseCompactW, baseCompactH,
		0, 0, instance, nil,
	)

	overlay.mu.Lock()
	overlay.hwnd = hwnd
	close(overlay.ready)
	overlay.mu.Unlock()

	go renderLoop(hwnd)

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
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// frame owns the GDI resources for one window size.
type frame struct {
	w, h  int32
	memDC win.HDC
	bmp   win.HBITMAP
	old   win.HGDIOBJ
	bits  []byte // premultiplied BGRA, w*h*4, top-down
}

func (f *frame) release() {
	if f.memDC != 0 {
		win.SelectObject(f.memDC, f.old)
		win.DeleteObject(win.HGDIOBJ(f.bmp))
		win.DeleteDC(f.memDC)
	}
	*f = frame{}
}

func (f *frame) ensure(w, h int32) bool {
	if f.w == w && f.h == h && f.memDC != 0 {
		return true
	}
	f.release()

	screenDC := win.GetDC(0)
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	if memDC == 0 {
		return false
	}

	bmi := bitmapInfoHeader{
		Width:    w,
		Height:   -h, // top-down
		Planes:   1,
		BitCount: 32,
	}
	bmi.Size = uint32(unsafe.Sizeof(bmi))

	var bitsPtr unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(uintptr(memDC), uintptr(unsafe.Pointer(&bmi)),
		0 /* DIB_RGB_COLORS */, uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	if bmp == 0 || bitsPtr == nil {
		win.DeleteDC(memDC)
		return false
	}

	f.w, f.h = w, h
	f.memDC = memDC
	f.bmp = win.HBITMAP(bmp)
	f.old = win.SelectObject(memDC, win.HGDIOBJ(bmp))
	f.bits = unsafe.Slice((*byte)(bitsPtr), int(w)*int(h)*4)
	return true
}

func renderLoop(hwnd win.HWND) {
	var f frame
	var tr textRenderer
	defer f.release()
	defer tr.release()

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		overlay.mu.Lock()
		visible := overlay.visible
		overlay.mu.Unlock()

		if !visible {
			// Sleep until someone shows the widget again; no CPU burned
			// while hidden.
			<-overlay.wake
			continue
		}

		safeRenderFrame(hwnd, &f, &tr)
		<-ticker.C
	}
}

// safeRenderFrame isolates a rendering panic to the current frame: resources
// are reset and the app keeps running instead of dying.
func safeRenderFrame(hwnd win.HWND, f *frame, tr *textRenderer) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("widget render panic (frame skipped): %v\n%s", r, debug.Stack())
			f.release()
			tr.release()
		}
	}()
	renderFrame(hwnd, f, tr)
}

func renderFrame(hwnd win.HWND, f *frame, tr *textRenderer) {
	overlay.mu.Lock()
	w, h := overlay.width, overlay.height
	wide := overlay.wide
	status := overlay.status
	elapsed := time.Since(overlay.started)
	sinceShow := time.Since(overlay.shownAt)
	scale := overlay.scale
	th, ok := themes[overlay.theme]
	if !ok {
		th = themes["green"]
	}
	// Wave amplitude follows the average of the last few RMS samples with
	// an asymmetric ease: fast attack, slower decay — like Siri.
	var sum float64
	const recent = 5
	for i := 0; i < recent; i++ {
		idx := (overlay.levelAt - 1 - i + len(overlay.levels)*2) % len(overlay.levels)
		sum += overlay.levels[idx]
	}
	target := math.Min(1, sum/recent*8)
	k := 0.20 // decay
	if target > overlay.amp {
		k = 0.45 // attack
	}
	overlay.amp += (target - overlay.amp) * k
	amp := overlay.amp
	overlay.mu.Unlock()

	if w <= 0 || h <= 0 || !f.ensure(w, h) {
		return
	}

	c := newCanvas(f.bits, int(w), int(h))
	c.clear()

	margin := float64(scaled(baseMargin, scale))
	cw := float64(w) - margin*2
	ch := float64(h) - margin*2
	radius := ch / 2

	// Solid translucent pill with a soft drop shadow and a subtle top
	// highlight. (Screen-capture blur was tried and disabled: recapturing
	// the desktop behind the widget was unreliable.)
	c.shadowRoundRect(margin, margin+1.5, cw, ch, radius, 4*scale, 55)
	c.fillRoundRect(margin, margin, cw, ch, radius, 24, 24, 27, 240)
	c.fillRoundRect(margin, margin, cw, ch/2.2, radius, 255, 255, 255, 10)

	switch {
	case wide && status == "processing":
		drawProcessingDots(c, margin, cw, ch, elapsed, scale, th)
	case wide && status == "done":
		drawDone(c, margin, cw, ch, scale, th)
	case wide:
		drawSiriWave(c, margin, cw, ch, elapsed, scale, amp, th)
		seconds := fmt.Sprintf("%d", int(elapsed.Seconds()))
		tr.draw(c, seconds, margin+cw-22*scale, margin+ch/2,
			int32(math.Round(15*scale)), 150, 150, 156, 0.62)
	default:
		drawIdle(c, margin, ch, scale, th)
	}

	// Fade-in via the constant-alpha channel of the blend function.
	alpha := byte(255)
	if sinceShow < fadeInDuration {
		alpha = byte(255 * float64(sinceShow) / float64(fadeInDuration))
	}

	blend := blendFunction{
		BlendOp:             0, // AC_SRC_OVER
		SourceConstantAlpha: alpha,
		AlphaFormat:         1, // AC_SRC_ALPHA
	}
	sz := size{CX: w, CY: h}
	var srcPt win.POINT
	const ulwAlpha = 2
	procUpdateLayeredWindow.Call(uintptr(hwnd), 0, 0,
		uintptr(unsafe.Pointer(&sz)), uintptr(f.memDC),
		uintptr(unsafe.Pointer(&srcPt)), 0,
		uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

type rgbColor struct{ r, g, b byte }

// theme defines the three wave layer colors (bright core → deep) plus an
// accent used for dots, checkmark and the idle glyph.
type themeColors struct {
	layers [3]rgbColor
	accent rgbColor
}

var themes = map[string]themeColors{
	"green": {
		layers: [3]rgbColor{{170, 255, 140}, {70, 220, 120}, {50, 190, 160}},
		accent: rgbColor{120, 230, 140},
	},
	"purple": {
		layers: [3]rgbColor{{215, 160, 255}, {160, 100, 245}, {110, 80, 210}},
		accent: rgbColor{190, 130, 255},
	},
	"yellow": {
		layers: [3]rgbColor{{255, 230, 130}, {250, 185, 70}, {230, 140, 50}},
		accent: rgbColor{255, 205, 90},
	},
	"red": {
		layers: [3]rgbColor{{255, 140, 130}, {245, 85, 95}, {205, 55, 75}},
		accent: rgbColor{255, 120, 120},
	},
	"blue": {
		layers: [3]rgbColor{{140, 200, 255}, {80, 150, 245}, {60, 110, 205}},
		accent: rgbColor{120, 180, 255},
	},
}

// waveShape holds the motion parameters of one layer; colors come from the
// active theme.
type waveShape struct {
	freq  float64 // horizontal wavelength multiplier
	speed float64 // phase animation speed
	phase float64 // phase offset
	amp   float64 // amplitude relative to the master amplitude
	alpha float64
}

var waveShapes = []waveShape{
	{freq: 2.4, speed: 4.6, phase: 0.0, amp: 1.00, alpha: 0.92},
	{freq: 3.1, speed: 3.4, phase: 1.9, amp: 0.88, alpha: 0.62},
	{freq: 1.7, speed: 5.6, phase: 4.1, amp: 0.76, alpha: 0.52},
	{freq: 2.8, speed: 2.7, phase: 2.6, amp: 0.66, alpha: 0.45},
	{freq: 2.1, speed: 5.1, phase: 5.3, amp: 0.58, alpha: 0.40},
	{freq: 3.5, speed: 3.9, phase: 0.9, amp: 0.50, alpha: 0.34},
	{freq: 1.4, speed: 4.3, phase: 3.4, amp: 0.44, alpha: 0.30},
	{freq: 2.6, speed: 6.0, phase: 1.3, amp: 0.38, alpha: 0.26},
}

// layerColor interpolates across the theme's three gradient stops so any
// number of wave layers gets a smooth bright→deep color ramp.
func layerColor(th themeColors, i, n int) rgbColor {
	if n <= 1 {
		return th.layers[0]
	}
	t := float64(i) / float64(n-1) * 2 // 0..2 across 3 stops
	k := int(t)
	if k >= 2 {
		return th.layers[2]
	}
	f := t - float64(k)
	a, b := th.layers[k], th.layers[k+1]
	return rgbColor{
		r: byte(float64(a.r) + (float64(b.r)-float64(a.r))*f),
		g: byte(float64(a.g) + (float64(b.g)-float64(a.g))*f),
		b: byte(float64(a.b) + (float64(b.b)-float64(a.b))*f),
	}
}

// drawSiriWave renders overlapping sine waves whose amplitude follows the
// voice level, with a soft glow pass under a crisp core stroke.
func drawSiriWave(c *canvas, margin, cw, ch float64, elapsed time.Duration, scale float64, amp float64, th themeColors) {
	baseY := margin + ch/2
	left := margin + 16*scale
	right := margin + cw - 54*scale
	width := right - left
	if width <= 0 {
		return
	}

	// Keep a subtle breathing motion even in silence.
	amp = 0.08 + 0.92*math.Max(0, math.Min(1, amp))
	maxH := ch/2 - 2.5*scale
	t := elapsed.Seconds()

	for li := len(waveShapes) - 1; li >= 0; li-- {
		l := waveShapes[li]
		col := layerColor(th, li, len(waveShapes))
		prevY := 0.0
		for xi := 0; xi <= int(width); xi++ {
			u := float64(xi) / width // 0..1 across the wave area
			envelope := math.Pow(math.Sin(u*math.Pi), 1.15)
			// A touch of per-layer amplitude modulation so lobes drift.
			mod := 0.72 + 0.28*math.Sin(u*5.1+t*l.speed*0.6+l.phase*2.1)
			y := amp * l.amp * envelope * mod * maxH *
				math.Sin(u*math.Pi*2*l.freq+t*l.speed+l.phase)
			if xi == 0 {
				prevY = y
			}
			x := left + float64(xi)
			// Fade both passes with the local swing: near the baseline the
			// three layers overlap, and full-strength glow there melts into
			// one fat bright line.
			mag := math.Min(1, math.Abs(y)/(4*scale))
			if glowA := l.alpha * 0.22 * mag; glowA > 0.01 {
				drawWaveSegment(c, x, baseY+prevY, baseY+y, 3.4*scale, col.r, col.g, col.b, glowA)
			}
			coreA := l.alpha * (0.25 + 0.75*mag)
			drawWaveSegment(c, x, baseY+prevY, baseY+y, 1.3*scale, col.r, col.g, col.b, coreA)
			prevY = y
		}
	}
}

// drawProcessingDots renders three bouncing dots — shown while the transcript
// is being processed (STT/LLM round-trip).
func drawProcessingDots(c *canvas, margin, cw, ch float64, elapsed time.Duration, scale float64, th themeColors) {
	cx := margin + cw/2
	cy := margin + ch/2
	r := 3.4 * scale
	gap := 13 * scale
	t := elapsed.Seconds()
	for i := -1; i <= 1; i++ {
		phase := t*5.2 - float64(i+1)*0.55
		bounce := math.Max(0, math.Sin(phase)) * 5.5 * scale
		pulse := 0.55 + 0.45*math.Max(0, math.Sin(phase))
		c.fillCircle(cx+float64(i)*gap, cy-bounce+2.2*scale, r, th.accent.r, th.accent.g, th.accent.b, pulse)
	}
}

// drawDone renders a checkmark in a soft accent circle — flashed briefly
// after the text has been inserted.
func drawDone(c *canvas, margin, cw, ch float64, scale float64, th themeColors) {
	cx := margin + cw/2
	cy := margin + ch/2
	r := 11 * scale
	c.fillCircle(cx, cy, r, th.accent.r, th.accent.g, th.accent.b, 0.22)
	thick := 1.6 * scale
	c.line(cx-4.4*scale, cy+0.4*scale, cx-1.2*scale, cy+3.6*scale, thick, th.accent.r, th.accent.g, th.accent.b, 0.95)
	c.line(cx-1.2*scale, cy+3.6*scale, cx+4.8*scale, cy-3.4*scale, thick, th.accent.r, th.accent.g, th.accent.b, 0.95)
}

// drawWaveSegment fills the vertical span between two adjacent curve points
// with a soft-edged stroke of half-thickness th.
func drawWaveSegment(c *canvas, x, y0, y1, th float64, r, g, b byte, alpha float64) {
	lo := math.Min(y0, y1) - th - 1
	hi := math.Max(y0, y1) + th + 1
	for py := int(math.Floor(lo)); py <= int(math.Ceil(hi)); py++ {
		fy := float64(py) + 0.5
		var d float64
		switch {
		case fy < math.Min(y0, y1):
			d = math.Min(y0, y1) - fy
		case fy > math.Max(y0, y1):
			d = fy - math.Max(y0, y1)
		default:
			d = 0
		}
		cov := 1 - d/th
		if cov <= 0 {
			continue
		}
		if cov > 1 {
			cov = 1
		}
		cov = cov * cov * (3 - 2*cov)
		c.blend(int(x), py, r, g, b, alpha*cov)
	}
}

func drawIdle(c *canvas, margin, ch float64, scale float64, th themeColors) {
	baseY := margin + ch/2
	heights := []float64{6, 12, 6}
	for i, bh := range heights {
		bh *= scale
		x := margin + (9+float64(i)*5)*scale
		w := 2.4 * scale
		c.fillRoundRect(x, baseY-bh/2, w, bh, w/2, th.accent.r, th.accent.g, th.accent.b, 255)
	}
}
