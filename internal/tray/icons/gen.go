//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	generate("idle.png", color.RGBA{120, 120, 120, 255})      // Gray
	generate("idle-dark.png", color.RGBA{200, 200, 200, 255}) // Light gray for dark mode
	generate("recording.png", color.RGBA{220, 40, 40, 255})   // Red
	generate("processing.png", color.RGBA{220, 180, 40, 255}) // Yellow/amber
}

func generate(name string, c color.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	// Draw filled circle
	cx, cy, r := 8, 8, 6
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
	f, _ := os.Create(name)
	defer f.Close()
	png.Encode(f, img)
}
