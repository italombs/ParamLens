//go:build ignore

// Draws the application icon (the logo of the interface header: a teal rounded square with a
// white ring) as a 256×256 PNG for go-winres:
//
//	go run scripts/make-icon.go cmd/paramlens/winres/icon.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	const size, ss = 256, 4 // supersampling for smooth edges
	teal := color.NRGBA{0x0f, 0x7b, 0x74, 0xff}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inRounded := func(x, y float64) bool {
		const m, r = 8.0, 56.0 // margin, corner radius
		cx := math.Max(m+r, math.Min(size-m-r, x))
		cy := math.Max(m+r, math.Min(size-m-r, y))
		return x >= m && x <= size-m && y >= m && y <= size-m && math.Hypot(x-cx, y-cy) <= r
	}
	inRing := func(x, y float64) bool {
		d := math.Hypot(x-128, y-128)
		return d <= 66 && d >= 42
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var bg, ring int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x, y := float64(px)+(float64(sx)+.5)/ss, float64(py)+(float64(sy)+.5)/ss
					if inRounded(x, y) {
						bg++
						if inRing(x, y) {
							ring++
						}
					}
				}
			}
			if bg == 0 {
				continue
			}
			n := float64(ss * ss)
			w := float64(ring) / float64(bg) // white share inside the square
			mix := func(c uint8) uint8 { return uint8(math.Round(float64(c)*(1-w) + 255*w)) }
			img.SetNRGBA(px, py, color.NRGBA{mix(teal.R), mix(teal.G), mix(teal.B), uint8(math.Round(255 * float64(bg) / n))})
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
