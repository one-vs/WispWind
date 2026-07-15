package widget

// GDI-rendered antialiased text and small vector shapes (dots, checkmark)
// for status animations.

import (
	"math"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

var procCreateFont = gdi32.NewProc("CreateFontW")

// textRenderer rasterizes text through GDI into a mask, then blends it into
// the canvas — real font antialiasing instead of a pixel font.
type textRenderer struct {
	w, h  int32
	memDC win.HDC
	bmp   win.HBITMAP
	old   win.HGDIOBJ
	bits  []byte
	font  win.HFONT
	fpx   int32
}

func (t *textRenderer) release() {
	if t.memDC != 0 {
		win.SelectObject(t.memDC, t.old)
		win.DeleteObject(win.HGDIOBJ(t.bmp))
		win.DeleteDC(t.memDC)
	}
	if t.font != 0 {
		win.DeleteObject(win.HGDIOBJ(t.font))
	}
	*t = textRenderer{}
}

func (t *textRenderer) ensure(w, h, fontPx int32) bool {
	if t.memDC != 0 && t.w >= w && t.h >= h && t.fpx == fontPx {
		return true
	}
	t.release()
	screenDC := win.GetDC(0)
	defer win.ReleaseDC(0, screenDC)
	memDC := win.CreateCompatibleDC(screenDC)
	if memDC == 0 {
		return false
	}
	bmi := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32}
	bmi.Size = uint32(unsafe.Sizeof(bmi))
	var bitsPtr unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(uintptr(memDC), uintptr(unsafe.Pointer(&bmi)),
		0, uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	if bmp == 0 || bitsPtr == nil {
		win.DeleteDC(memDC)
		return false
	}
	const antialiasedQuality = 4
	face := syscall.StringToUTF16Ptr("Segoe UI")
	font, _, _ := procCreateFont.Call(uintptr(-fontPx), 0, 0, 0, 400, 0, 0, 0,
		1 /* DEFAULT_CHARSET */, 0, 0, antialiasedQuality, 0,
		uintptr(unsafe.Pointer(face)))
	t.w, t.h = w, h
	t.memDC = memDC
	t.bmp = win.HBITMAP(bmp)
	t.old = win.SelectObject(memDC, win.HGDIOBJ(bmp))
	t.bits = unsafe.Slice((*byte)(bitsPtr), int(w)*int(h)*4)
	t.font = win.HFONT(font)
	t.fpx = fontPx
	win.SelectObject(memDC, win.HGDIOBJ(t.font))
	return true
}

// draw renders text right-aligned at (right, centerY) into the canvas.
func (t *textRenderer) draw(c *canvas, text string, right, centerY float64, fontPx int32, r, g, b byte, alpha float64) {
	if !t.ensure(int32(c.w), fontPx+8, fontPx) {
		return
	}
	u := syscall.StringToUTF16(text)
	if len(u) <= 1 {
		return
	}
	var sz win.SIZE
	win.GetTextExtentPoint32(t.memDC, &u[0], int32(len(u)-1), &sz)
	if sz.CX <= 0 {
		return
	}
	// Clear mask, draw white-on-black.
	for i := range t.bits[:int(t.w)*int(t.h)*4] {
		t.bits[i] = 0
	}
	win.SetBkMode(t.memDC, win.TRANSPARENT)
	win.SetTextColor(t.memDC, 0xFFFFFF)
	win.TextOut(t.memDC, 0, 0, &u[0], int32(len(u)-1))

	ox := int(right) - int(sz.CX)
	oy := int(centerY) - int(sz.CY)/2
	for y := 0; y < int(sz.CY) && y < int(t.h); y++ {
		for x := 0; x < int(sz.CX) && x < int(t.w); x++ {
			cov := float64(t.bits[(y*int(t.w)+x)*4+1]) / 255 // green channel as coverage
			if cov <= 0.01 {
				continue
			}
			c.blend(ox+x, oy+y, r, g, b, alpha*cov)
		}
	}
}

// fillCircle draws an antialiased disk.
func (c *canvas) fillCircle(cx, cy, r float64, cr, cg, cb byte, alpha float64) {
	c.fillRoundRect(cx-r, cy-r, r*2, r*2, r, cr, cg, cb, byte(alpha*255))
}

// line draws an antialiased capsule segment of half-thickness th.
func (c *canvas) line(x0, y0, x1, y1, th float64, r, g, b byte, alpha float64) {
	minX := math.Min(x0, x1) - th - 1
	maxX := math.Max(x0, x1) + th + 1
	minY := math.Min(y0, y1) - th - 1
	maxY := math.Max(y0, y1) + th + 1
	dx := x1 - x0
	dy := y1 - y0
	lenSq := dx*dx + dy*dy
	for py := int(minY); py <= int(maxY); py++ {
		for px := int(minX); px <= int(maxX); px++ {
			fx := float64(px) + 0.5
			fy := float64(py) + 0.5
			t := 0.0
			if lenSq > 0 {
				t = math.Max(0, math.Min(1, ((fx-x0)*dx+(fy-y0)*dy)/lenSq))
			}
			ddx := fx - (x0 + t*dx)
			ddy := fy - (y0 + t*dy)
			d := math.Sqrt(ddx*ddx+ddy*ddy) - th
			cov := 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			c.blend(px, py, r, g, b, alpha*cov)
		}
	}
}
