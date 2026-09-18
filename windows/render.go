package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

// Agents shrink anything larger than this before the model sees it.
const maxEdge = 2000

var highlight = color.RGBA{0xFF, 0x2D, 0x55, 0xFF}

// shrink scales src down so its long edge is at most maxEdge, averaging the pixels it merges.
func shrink(src *image.RGBA) (*image.RGBA, float64) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxEdge {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		copy(out.Pix, src.Pix)
		return out, 1
	}
	scale := float64(maxEdge) / float64(long)
	ow, oh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		y0, y1 := y*h/oh, (y+1)*h/oh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < ow; x++ {
			x0, x1 := x*w/ow, (x+1)*w/ow
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					i := src.PixOffset(sx, sy)
					r += uint32(src.Pix[i])
					g += uint32(src.Pix[i+1])
					b += uint32(src.Pix[i+2])
					n++
				}
			}
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), 0xFF
		}
	}
	return out, scale
}

func scaleRect(r image.Rectangle, s float64) image.Rectangle {
	return image.Rect(int(float64(r.Min.X)*s), int(float64(r.Min.Y)*s), int(float64(r.Max.X)*s+0.5), int(float64(r.Max.Y)*s+0.5))
}

// outline draws a frame just outside r so it never covers what was pointed at.
func outline(img *image.RGBA, r image.Rectangle, width int) {
	outer := r.Inset(-width).Intersect(img.Bounds())
	for y := outer.Min.Y; y < outer.Max.Y; y++ {
		for x := outer.Min.X; x < outer.Max.X; x++ {
			if !(image.Point{x, y}.In(r)) {
				img.SetRGBA(x, y, highlight)
			}
		}
	}
}

// full is the whole screen with the dragged part outlined and the rest dimmed.
func full(screen *image.RGBA, region image.Rectangle) (*image.RGBA, image.Rectangle) {
	out, scale := shrink(screen)
	r := scaleRect(region, scale).Intersect(out.Bounds())
	b := out.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if (image.Point{x, y}.In(r)) {
				continue
			}
			i := out.PixOffset(x, y)
			out.Pix[i] = uint8(uint32(out.Pix[i]) * 65 / 100)
			out.Pix[i+1] = uint8(uint32(out.Pix[i+1]) * 65 / 100)
			out.Pix[i+2] = uint8(uint32(out.Pix[i+2]) * 65 / 100)
		}
	}
	outline(out, r, 4)
	return out, r
}

// crop is the dragged part at full sharpness with a margin around it.
func crop(screen *image.RGBA, region image.Rectangle) *image.RGBA {
	margin := region.Dx()
	if region.Dy() > margin {
		margin = region.Dy()
	}
	margin = margin * 15 / 100
	if margin < 40 {
		margin = 40
	}
	padded := region.Inset(-margin).Intersect(screen.Bounds())
	cut := image.NewRGBA(image.Rect(0, 0, padded.Dx(), padded.Dy()))
	for y := 0; y < padded.Dy(); y++ {
		s := screen.PixOffset(padded.Min.X, padded.Min.Y+y)
		copy(cut.Pix[cut.PixOffset(0, y):cut.PixOffset(0, y)+padded.Dx()*4], screen.Pix[s:s+padded.Dx()*4])
	}
	out, scale := shrink(cut)
	outline(out, scaleRect(region.Sub(padded.Min), scale), 3)
	return out
}

func writePNG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, img)
}
