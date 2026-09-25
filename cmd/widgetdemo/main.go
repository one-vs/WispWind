package main

// Console harness driving the overlay through all states with synthetic
// levels — reproduces render crashes without a microphone.

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"wispwind/internal/widget"
)

func main() {
	fmt.Println("widget demo start")
	widget.Start()
	themes := []string{"green", "purple", "red"}
	for cycle := 0; cycle < 3; cycle++ {
		widget.SetTheme(themes[cycle])
		widget.Show("listening")
		start := time.Now()
		for time.Since(start) < 3*time.Second {
			lvl := 0.05 + 0.12*math.Abs(math.Sin(time.Since(start).Seconds()*3)) + rand.Float64()*0.03
			widget.SetLevel(lvl)
			time.Sleep(30 * time.Millisecond)
		}
		widget.SetStatus("processing")
		time.Sleep(1500 * time.Millisecond)
		widget.SetStatus("done")
		time.Sleep(500 * time.Millisecond)
		widget.Hide()
		time.Sleep(300 * time.Millisecond)
	}
	fmt.Println("demo finished OK")
}
