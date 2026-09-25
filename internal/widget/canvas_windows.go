package widget

// Minimal software renderer producing premultiplied BGRA for
// UpdateLayeredWindow. Shapes are rasterized from signed distance functions,
// giving cheap antialiasing at the tiny sizes the overlay uses.

import "math"

type canvas struct {
	pix  []byte // BGRA premultiplied, top-down
	w, h int
}

func newCanvas(pix []byte, w, h int) *canvas {
	return &canvas{pix: pix, w: w, h: h}
}

func (c *canvas) clear() {
	for i := range c.pix {
		c.pix[i] = 0
	}
}

// blend composites a straight-alpha color over the pixel (premultiplied out).
func (c *canvas) blend(x, y int, r, g, b byte, a float64) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	i := (y*c.w + x) * 4
	sa := a
	sb := float64(b) * sa
	sg := float64(g) * sa
	sr := float64(r) * sa
	inv := 1 - sa
	c.pix[i+0] = byte(sb + float64(c.pix[i+0])*inv)
	c.pix[i+1] = byte(sg + float64(c.pix[i+1])*inv)
	c.pix[i+2] = byte(sr + float64(c.pix[i+2])*inv)
	c.pix[i+3] = byte(sa*255 + float64(c.pix[i+3])*inv)
}

// sdRoundRect is the signed distance from point (px,py) to a rounded
// rectangle with top-left (x,y), size (w,h) and corner radius rad.
func sdRoundRect(px, py, x, y, w, h, rad float64) float64 {
	cx := x + w/2
	cy := y + h/2
	dx := math.Abs(px-cx) - (w/2 - rad)
	dy := math.Abs(py-cy) - (h/2 - rad)
	ax := math.Max(dx, 0)
	ay := math.Max(dy, 0)
	return math.Sqrt(ax*ax+ay*ay) + math.Min(math.Max(dx, dy), 0) - rad
}

// fillRoundRect draws an antialiased rounded rectangle.
func (c *canvas) fillRoundRect(x, y, w, h, rad float64, r, g, b, a byte) {
	x0 := int(math.Floor(x - 1))
	y0 := int(math.Floor(y - 1))
	x1 := int(math.Ceil(x + w + 1))
	y1 := int(math.Ceil(y + h + 1))
	alpha := float64(a) / 255
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			d := sdRoundRect(float64(px)+0.5, float64(py)+0.5, x, y, w, h, rad)
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

// shadowRoundRect draws a soft shadow: alpha falls off smoothly with the
// distance outside the rounded rect.
func (c *canvas) shadowRoundRect(x, y, w, h, rad, blur float64, maxAlpha byte) {
	if blur < 1 {
		blur = 1
	}
	x0 := int(math.Floor(x - blur - 1))
	y0 := int(math.Floor(y - blur - 1))
	x1 := int(math.Ceil(x + w + blur + 1))
	y1 := int(math.Ceil(y + h + blur + 1))
	base := float64(maxAlpha) / 255
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			d := sdRoundRect(float64(px)+0.5, float64(py)+0.5, x, y, w, h, rad)
			t := 1 - d/blur
			if t <= 0 {
				continue
			}
			if t > 1 {
				t = 1
			}
			// smoothstep for a soft gaussian-ish falloff
			t = t * t * (3 - 2*t)
			c.blend(px, py, 0, 0, 0, base*t)
		}
	}
}
