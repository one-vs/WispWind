package widget

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"
)

// TestRenderPreview draws frames of every widget state and writes PNGs so the
// overlay can be inspected without running the app.
func TestRenderPreview(t *testing.T) {
	out := os.Getenv("WIDGET_PREVIEW")
	scale := 1.5
	w := int(float64(baseWideW+2*baseMargin) * scale)
	h := int(float64(baseWideH+2*baseMargin) * scale)

	var tr textRenderer
	defer tr.release()

	render := func(amp float64, status string, theme string, elapsed time.Duration) []byte {
		buf := make([]byte, w*h*4)
		c := newCanvas(buf, w, h)
		c.clear()
		margin := float64(baseMargin) * scale
		cw := float64(w) - margin*2
		ch := float64(h) - margin*2
		radius := ch / 2
		th := themes[theme]
		c.shadowRoundRect(margin, margin+1.5, cw, ch, radius, 4*scale, 55)
		c.fillRoundRect(margin, margin, cw, ch, radius, 24, 24, 27, 240)
		c.fillRoundRect(margin, margin, cw, ch/2.2, radius, 255, 255, 255, 10)
		switch status {
		case "processing":
			drawProcessingDots(c, margin, cw, ch, elapsed, scale, th)
		case "done":
			drawDone(c, margin, cw, ch, scale, th)
		default:
			drawSiriWave(c, margin, cw, ch, elapsed, scale, amp, th)
			tr.draw(c, fmt.Sprintf("%d", int(elapsed.Seconds())), margin+cw-13*scale, margin+ch/2, 22, 150, 150, 156, 0.62)
		}
		return buf
	}

	buf := render(0.85, "listening", "green", 128*time.Second)
	nonzero := 0
	for _, b := range buf {
		if b != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("render produced empty frame")
	}
	if out == "" {
		return
	}

	frames := map[string][]byte{
		out:                     buf,
		out + ".purple.png":     render(0.85, "listening", "purple", 71*time.Second),
		out + ".processing.png": render(0, "processing", "green", 1300*time.Millisecond),
		out + ".done.png":       render(0, "done", "green", 0),
	}
	for path, b := range frames {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := (y*w + x) * 4
				img.SetRGBA(x, y, color.RGBA{R: b[i+2], G: b[i+1], B: b[i], A: b[i+3]})
			}
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
