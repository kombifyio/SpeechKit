//go:build ignore

package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"
)

func main() {
	generateICO("speechkit.ico", color.RGBA{60, 140, 220, 255}) // Blue
}

func generateICO(name string, c color.RGBA) {
	size := 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy, r := size/2, size/2, size/2-2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}

	f, _ := os.Create(name)
	defer f.Close()

	// ICO header
	binary.Write(f, binary.LittleEndian, uint16(0))     // reserved
	binary.Write(f, binary.LittleEndian, uint16(1))     // type: icon
	binary.Write(f, binary.LittleEndian, uint16(1))     // count

	// Directory entry
	bmpSize := 40 + size*size*4 + size*size/8
	f.Write([]byte{byte(size), byte(size)}) // width, height
	f.Write([]byte{0, 0})                   // colors, reserved
	binary.Write(f, binary.LittleEndian, uint16(1))            // planes
	binary.Write(f, binary.LittleEndian, uint16(32))           // bpp
	binary.Write(f, binary.LittleEndian, uint32(bmpSize))      // size
	binary.Write(f, binary.LittleEndian, uint32(22))           // offset

	// BMP info header (BITMAPINFOHEADER)
	binary.Write(f, binary.LittleEndian, uint32(40))           // header size
	binary.Write(f, binary.LittleEndian, int32(size))          // width
	binary.Write(f, binary.LittleEndian, int32(size*2))        // height (doubled for ICO)
	binary.Write(f, binary.LittleEndian, uint16(1))            // planes
	binary.Write(f, binary.LittleEndian, uint16(32))           // bpp
	binary.Write(f, binary.LittleEndian, uint32(0))            // compression
	binary.Write(f, binary.LittleEndian, uint32(0))            // image size
	binary.Write(f, binary.LittleEndian, int32(0))             // x ppm
	binary.Write(f, binary.LittleEndian, int32(0))             // y ppm
	binary.Write(f, binary.LittleEndian, uint32(0))            // colors used
	binary.Write(f, binary.LittleEndian, uint32(0))            // important colors

	// Pixel data (bottom-up BGRA)
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			f.Write([]byte{byte(b >> 8), byte(g >> 8), byte(r >> 8), byte(a >> 8)})
		}
	}

	// AND mask (all transparent)
	maskRowBytes := (size + 31) / 32 * 4
	mask := make([]byte, maskRowBytes*size)
	f.Write(mask)
}
