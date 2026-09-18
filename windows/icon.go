package main

import (
	"bytes"
	_ "embed"
	"image/png"
)

// tray.png is the tray glyph (written by scripts/draw-icon.py): the crop corners and the smirk in white on
// nothing, out to the edges, like the system's own tray glyphs. The tray icon is built from it at run time,
// so the exe needs no resource file.
//
//go:embed tray.png
var trayPNG []byte

// iconPixels shrinks the glyph to size×size and returns it the way Windows bitmaps want it: rows from the
// top, blue-green-red-alpha, colours premultiplied by alpha. Each destination pixel averages its block.
// With dark set, the glyph is black instead of white, for a light taskbar.
func iconPixels(size int, dark bool) []byte {
	src, err := png.Decode(bytes.NewReader(trayPNG))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		y0, y1 := y*h/size, (y+1)*h/size
		for x := 0; x < size; x++ {
			x0, x1 := x*w/size, (x+1)*w/size
			var a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					_, _, _, pa := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA() // 16-bit
					a, n = a+uint64(pa), n+1
				}
			}
			if n == 0 {
				continue
			}
			alpha := byte(a / n >> 8)
			value := alpha // white, premultiplied
			if dark {
				value = 0
			}
			i := (y*size + x) * 4
			out[i], out[i+1], out[i+2], out[i+3] = value, value, value, alpha
		}
	}
	return out
}
