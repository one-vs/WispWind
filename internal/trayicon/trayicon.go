package trayicon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

const size = 32

func Icon() []byte {
	return StatusIconICO(false)
}

func StatusIconICO(recording bool) []byte {
	pixels := drawIconPixels(recording)
	return encodeICO(pixels)
}

func StatusIconPNG(recording bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	pixels := drawIconPixels(recording)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			// pixels are stored as BGRA for ICO
			img.Set(x, y, color.RGBA{pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func drawIconPixels(recording bool) []byte {
	pixels := make([]byte, size*size*4)

	c := rgba{32, 32, 32, 255}
	radius := 7
	if recording {
		c = rgba{230, 62, 62, 255}
		radius = 3
	}
	fillRoundedRect(pixels, 7, 7, 25, 25, radius, c)

	return pixels
}

func encodeICO(pixels []byte) []byte {
	maskSize := size * 4
	imageSize := 40 + len(pixels) + maskSize
	icon := make([]byte, 22+imageSize)

	binary.LittleEndian.PutUint16(icon[2:], 1)
	binary.LittleEndian.PutUint16(icon[4:], 1)
	icon[6] = size
	icon[7] = size
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

type rgba struct {
	r, g, b, a byte
}

func setPixel(pixels []byte, x, y int, c rgba) {
	if x < 0 || y < 0 || x >= size || y >= size {
		return
	}
	i := (y*size + x) * 4
	pixels[i] = c.b
	pixels[i+1] = c.g
	pixels[i+2] = c.r
	pixels[i+3] = c.a
}

func fillRect(pixels []byte, x1, y1, x2, y2 int, c rgba) {
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			setPixel(pixels, x, y, c)
		}
	}
}

func fillCircle(pixels []byte, cx, cy, r int, c rgba) {
	rr := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rr {
				setPixel(pixels, x, y, c)
			}
		}
	}
}

func fillRoundedRect(pixels []byte, x1, y1, x2, y2, r int, c rgba) {
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			dx := 0
			if x < x1+r {
				dx = x1 + r - x
			} else if x >= x2-r {
				dx = x - (x2 - r - 1)
			}
			dy := 0
			if y < y1+r {
				dy = y1 + r - y
			} else if y >= y2-r {
				dy = y - (y2 - r - 1)
			}
			if dx == 0 || dy == 0 || dx*dx+dy*dy <= r*r {
				setPixel(pixels, x, y, c)
			}
		}
	}
}
