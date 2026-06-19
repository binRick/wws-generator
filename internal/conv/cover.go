package conv

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
)

// coverDataURI renders the first sheet's layout to a small PNG and returns it
// as a data: URI for use as the library thumbnail.
func coverDataURI(placements []Placement, opt BuildOptions, flattenTol float64) string {
	const maxDim = 360.0
	const pad = 6

	W, H := opt.MaterialW, opt.MaterialH
	if W <= 0 || H <= 0 {
		W, H = 100, 100
	}
	scale := maxDim / math.Max(W, H)
	imgW := int(W*scale) + 2*pad
	imgH := int(H*scale) + 2*pad
	if imgW < 1 {
		imgW = 1
	}
	if imgH < 1 {
		imgH = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	bg := color.RGBA{255, 255, 255, 255}
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.Set(x, y, bg)
		}
	}
	// Sheet border.
	border := color.RGBA{210, 210, 210, 255}
	drawRect(img, pad, pad, pad+int(W*scale), pad+int(H*scale), border)

	line := color.RGBA{230, 31, 25, 255} // cut red
	mark := color.RGBA{60, 60, 60, 255}  // engrave (dark gray)
	toPx := func(p Point) (int, int) {
		return pad + int(p.X*scale), pad + int(p.Y*scale)
	}
	drawSubs := func(pl Placement, sps []Subpath, c color.RGBA) {
		for _, sp := range sps {
			poly := sp.transform(pl.M).Flatten(flattenTol)
			for i := 0; i+1 < len(poly); i++ {
				x0, y0 := toPx(poly[i])
				x1, y1 := toPx(poly[i+1])
				drawLine(img, x0, y0, x1, y1, c)
			}
		}
	}
	for _, pl := range placements {
		if pl.Sheet != 0 {
			continue
		}
		drawSubs(pl, pl.Piece.Subpaths, line)
		for _, mk := range pl.Piece.Marks {
			drawSubs(pl, mk.Subpaths, mark)
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func drawRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	drawLine(img, x0, y0, x1, y0, c)
	drawLine(img, x1, y0, x1, y1, c)
	drawLine(img, x1, y1, x0, y1, c)
	drawLine(img, x0, y1, x0, y0, c)
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 >= x1 {
		sx = -1
	}
	sy := 1
	if y0 >= y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= 0 && y0 >= 0 && x0 < img.Bounds().Dx() && y0 < img.Bounds().Dy() {
			img.Set(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
