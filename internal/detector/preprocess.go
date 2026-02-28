package detector

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
)

type LetterboxMeta struct {
	Scale        float32
	PadX, PadY   int
	NewH, NewW   int
	InputSize    int
	OrigW, OrigH int
}

func preprocessToNCHW(img image.Image, inputSize int, dst []float32) (LetterboxMeta, error) {
	if inputSize <= 0 {
		return LetterboxMeta{}, errors.New("input size must be > 0")
	}

	b := img.Bounds()
	origW := b.Dx()
	origH := b.Dy()

	if origW <= 0 || origH <= 0 {
		return LetterboxMeta{}, errors.New("invalid image dimensions")
	}

	scale := float32(inputSize) / float32(max(origW, origH))
	newW := int(float32(origW) * scale)
	newH := int(float32(origH) * scale)

	// paddding to center
	padX := (inputSize - newW) / 2
	padY := (inputSize - newH) / 2

	// 1) Resize to (newW, newH)
	resized := resizeBilinearToRGBA(img, newW, newH)

	// 2) Create canvas inputSize x inputSize filled with 114 gray
	canvas := image.NewRGBA(image.Rect(0, 0, inputSize, inputSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{114, 114, 114, 255}}, image.Point{}, draw.Src)

	// 3) Paste resized onto canvas at (padX, padY)
	draw.Draw(canvas, image.Rect(padX, padY, padX+newW, padY+newH), resized, image.Point{}, draw.Src)

	// 4) Fill dst in NCHW (RGB order)
	need := 3 * inputSize * inputSize
	if len(dst) < need {
		return LetterboxMeta{}, errors.New("dst buffer too small")
	}

	W, H := inputSize, inputSize
	plane := W * H

	// dst layout: [R plane][G plane][B plane]
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			off := (y*W + x) * 4
			r := canvas.Pix[off+0]
			g := canvas.Pix[off+1]
			bb := canvas.Pix[off+2]

			i := y*W + x
			dst[0*plane+i] = float32(r) / 255.0
			dst[1*plane+i] = float32(g) / 255.0
			dst[2*plane+i] = float32(bb) / 255.0
		}
	}

	return LetterboxMeta{
		Scale:     float32(scale),
		PadX:      padX,
		PadY:      padY,
		NewH:      newH,
		NewW:      newW,
		InputSize: inputSize,
		OrigW:     origW,
		OrigH:     origH,
	}, nil
}

func resizeBilinearToRGBA(src image.Image, dstW, dstH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()

	// Division by zero check
	if dstW <= 0 || dstH <= 0 || sw <= 0 || sh <= 0 {
		return dst
	}

	scaleX := float64(sw) / float64(dstW)
	scaleY := float64(sh) / float64(dstH)

	for y := 0; y < dstH; y++ {

		fy := (float64(y)+0.5)*scaleY - 0.5
		y0 := int(math.Floor(fy))

		y1 := y0 + 1
		wy := fy - float64(y0)

		if y0 < 0 {
			y0, y1, wy = 0, 0, 0
		}

		if y1 >= sh {
			y1 = sh - 1
		}

		if y0 >= sh {
			y0 = sh - 1
		}

		for x := 0; x < dstW; x++ {
			fx := (float64(x)+0.5)*scaleX - 0.5

			x0 := int(math.Floor(fx))
			x1 := x0 + 1
			wx := fx - float64(x0)

			if x0 < 0 {
				x0, x1, wx = 0, 0, 0
			}

			if x1 >= sw {
				x1 = sw - 1
			}

			if x0 >= sw {
				x0 = sw - 1
			}

			c00 := colorRGBA(src.At(sb.Min.X+x0, sb.Min.Y+y0))
			c10 := colorRGBA(src.At(sb.Min.X+x1, sb.Min.Y+y0))
			c01 := colorRGBA(src.At(sb.Min.X+x0, sb.Min.Y+y1))
			c11 := colorRGBA(src.At(sb.Min.X+x1, sb.Min.Y+y1))

			r := bilerp(c00.R, c10.R, c01.R, c11.R, wx, wy)
			g := bilerp(c00.G, c10.G, c01.G, c11.G, wx, wy)
			b := bilerp(c00.B, c10.B, c01.B, c11.B, wx, wy)
			a := bilerp(c00.A, c10.A, c01.A, c11.A, wx, wy)

			dst.SetRGBA(x, y, color.RGBA{uint8(r), uint8(g), uint8(b), uint8(a)})
		}
	}

	return dst
}

type rgbaF struct{ R, G, B, A float64 }

func colorRGBA(c color.Color) rgbaF {
	r, g, b, a := c.RGBA() // 0-65535

	return rgbaF{
		R: float64(r) / 257.0, // convert to 0-255
		G: float64(g) / 257.0,
		B: float64(b) / 257.0,
		A: float64(a) / 257.0,
	}
}

func bilerp(c00, c10, c01, c11 float64, wx, wy float64) float64 {
	// linear in x
	top := c00*(1-wx) + c10*wx
	bottom := c01*(1-wx) + c11*wx

	// linear in y
	return top*(1-wy) + bottom*wy
}
