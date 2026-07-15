package trayicon

// Antialiased 64px tray icons rendered from signed distance fields: a small
// audio waveform (five rounded bars). Idle is soft green, recording is red.

import (
	"encoding/binary"
	"math"
)

const size = 64

func Icon() []byte {
	return StatusIcon(false)
}

type color struct {
	r, g, b byte
}

func StatusIcon(recording bool) []byte {
	pixels := make([]byte, size*size*4)

	bright := color{120, 225, 140} // idle: widget green accent
	deep := color{60, 175, 110}
	if recording {
		bright = color{255, 105, 95}
		deep = color{215, 55, 70}
	}

	// Five rounded bars, symmetric waveform silhouette.
	heights := []float64{0.34, 0.62, 0.94, 0.62, 0.34}
	barW := 7.0
	gap := 4.4
	totalW := barW*float64(len(heights)) + gap*float64(len(heights)-1)
	left := (size - totalW) / 2
	cy := float64(size) / 2

	for i, hf := range heights {
		h := hf * (size - 10)
		x := left + float64(i)*(barW+gap)
		// Vertical gradient per bar: bright on top -> deep at bottom.
		fillBarAA(pixels, x, cy-h/2, barW, h, barW/2, bright, deep)
	}

	return encodeICO(pixels)
}

// fillBarAA draws an antialiased rounded bar with a vertical gradient.
func fillBarAA(pixels []byte, x, y, w, h, rad float64, top, bottom color) {
	x0 := int(math.Floor(x - 1))
	y0 := int(math.Floor(y - 1))
	x1 := int(math.Ceil(x + w + 1))
	y1 := int(math.Ceil(y + h + 1))
	for py := y0; py <= y1; py++ {
		var t float64
		if h > 0 {
			t = math.Max(0, math.Min(1, (float64(py)+0.5-y)/h))
		}
		c := color{
			r: byte(float64(top.r) + (float64(bottom.r)-float64(top.r))*t),
			g: byte(float64(top.g) + (float64(bottom.g)-float64(top.g))*t),
			b: byte(float64(top.b) + (float64(bottom.b)-float64(top.b))*t),
		}
		for px := x0; px <= x1; px++ {
			d := sdRoundRect(float64(px)+0.5, float64(py)+0.5, x, y, w, h, rad)
			cov := 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			blendPixel(pixels, px, py, c, cov)
		}
	}
}

func sdRoundRect(px, py, x, y, w, h, rad float64) float64 {
	cx := x + w/2
	cy := y + h/2
	dx := math.Abs(px-cx) - (w/2 - rad)
	dy := math.Abs(py-cy) - (h/2 - rad)
	ax := math.Max(dx, 0)
	ay := math.Max(dy, 0)
	return math.Sqrt(ax*ax+ay*ay) + math.Min(math.Max(dx, dy), 0) - rad
}

func blendPixel(pixels []byte, x, y int, c color, a float64) {
	if x < 0 || y < 0 || x >= size || y >= size {
		return
	}
	i := (y*size + x) * 4
	inv := 1 - a
	pixels[i] = byte(float64(c.b)*a + float64(pixels[i])*inv)
	pixels[i+1] = byte(float64(c.g)*a + float64(pixels[i+1])*inv)
	pixels[i+2] = byte(float64(c.r)*a + float64(pixels[i+2])*inv)
	na := a*255 + float64(pixels[i+3])*inv
	pixels[i+3] = byte(na)
}

func encodeICO(pixels []byte) []byte {
	maskSize := size * size / 8
	imageSize := 40 + len(pixels) + maskSize
	icon := make([]byte, 22+imageSize)

	binary.LittleEndian.PutUint16(icon[2:], 1)
	binary.LittleEndian.PutUint16(icon[4:], 1)
	icon[6] = byte(size % 256)
	icon[7] = byte(size % 256)
	binary.LittleEndian.PutUint16(icon[10:], 1)
	binary.LittleEndian.PutUint16(icon[12:], 32)
	binary.LittleEndian.PutUint32(icon[14:], uint32(imageSize))
	binary.LittleEndian.PutUint32(icon[18:], 22)

	offset := 22
	binary.LittleEndian.PutUint32(icon[offset:], 40)
	binary.LittleEndian.PutUint32(icon[offset+4:], size)
	binary.LittleEndian.PutUint32(icon[offset+8:], size*2)
	binary.LittleEndian.PutUint16(icon[offset+12:], 1)
	binary.LittleEndian.PutUint16(icon[offset+14:], 32)
	binary.LittleEndian.PutUint32(icon[offset+20:], uint32(len(pixels)))

	dst := offset + 40
	for y := size - 1; y >= 0; y-- {
		copy(icon[dst:], pixels[y*size*4:(y+1)*size*4])
		dst += size * 4
	}
	return icon
}
