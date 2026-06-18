package conv

import (
	"math"
	"sort"
)

// span is a half-open horizontal interval [X0, X1) of grid cells.
type span struct{ X0, X1 int }

// mask is the rasterised footprint of a piece at a chosen rotation. The
// undilated material region is used to mark occupancy; the dilated region
// (grown by the spacing) is used for collision tests so placed pieces keep
// their distance.
type mask struct {
	w, h  int      // undilated cell dimensions
	rows  [][]span // material spans per row (len h), x relative to origin
	d     int      // dilation radius in cells
	dRows [][]span // dilated spans; dRows[k] is row dy=k-d, x relative to origin
	ox    float64  // mm x of the (rotated) footprint min corner (translation used)
	oy    float64  // mm y
}

// buildMask rotates a piece's flattened loops by deg, rasterises the material
// region at the given resolution, and dilates by `dilate` cells.
func buildMask(loops [][]Point, deg, res float64, dilate int) *mask {
	rot := RotateDeg(deg)
	rl := make([][]Point, len(loops))
	bb := emptyRect()
	for i, lp := range loops {
		r := make([]Point, len(lp))
		for j, p := range lp {
			rp := rot.Apply(p)
			r[j] = rp
			bb.add(rp)
		}
		rl[i] = r
	}
	// Translate so the footprint min corner is at (0,0).
	for i := range rl {
		for j := range rl[i] {
			rl[i][j].X -= bb.MinX
			rl[i][j].Y -= bb.MinY
		}
	}

	w := int(math.Ceil(bb.W()/res)) + 1
	h := int(math.Ceil(bb.H()/res)) + 1
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	mat := make([][]bool, h)
	for y := range mat {
		mat[y] = make([]bool, w)
	}
	fillEvenOdd(rl, res, w, h, mat)

	m := &mask{w: w, h: h, d: dilate, ox: bb.MinX, oy: bb.MinY}
	m.rows = spansFromGrid(mat, w, h)
	if dilate > 0 {
		m.dRows = dilatedSpans(mat, w, h, dilate)
	} else {
		// No dilation: dRows aligns 1:1 with rows (d=0 => dy=k).
		m.dRows = m.rows
	}
	return m
}

// fillEvenOdd rasterises loops using the even-odd rule via scanlines through
// cell centres.
func fillEvenOdd(loops [][]Point, res float64, w, h int, mat [][]bool) {
	for gy := 0; gy < h; gy++ {
		y := (float64(gy) + 0.5) * res
		var xs []float64
		for _, lp := range loops {
			n := len(lp)
			if n < 2 {
				continue
			}
			for i := 0; i < n; i++ {
				a := lp[i]
				b := lp[(i+1)%n]
				if (a.Y > y) != (b.Y > y) {
					x := a.X + (y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
					xs = append(xs, x)
				}
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Floor(xs[i]/res + 0.5))
			x1 := int(math.Floor(xs[i+1]/res + 0.5))
			if x0 < 0 {
				x0 = 0
			}
			if x1 > w {
				x1 = w
			}
			for x := x0; x < x1; x++ {
				mat[gy][x] = true
			}
		}
	}
}

func spansFromGrid(mat [][]bool, w, h int) [][]span {
	rows := make([][]span, h)
	for y := 0; y < h; y++ {
		rows[y] = rowSpans(mat[y], w)
	}
	return rows
}

func rowSpans(row []bool, w int) []span {
	var spans []span
	x := 0
	for x < w {
		if !row[x] {
			x++
			continue
		}
		start := x
		for x < w && row[x] {
			x++
		}
		spans = append(spans, span{start, x})
	}
	return spans
}

// dilatedSpans grows the material grid by a square structuring element of
// radius d and returns spans indexed so that dRows[k] corresponds to dy=k-d.
func dilatedSpans(mat [][]bool, w, h, d int) [][]span {
	dw := w + 2*d
	dh := h + 2*d
	dmat := make([][]bool, dh)
	for y := range dmat {
		dmat[y] = make([]bool, dw)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !mat[y][x] {
				continue
			}
			for yy := y - d; yy <= y+d; yy++ {
				drow := dmat[yy+d]
				for xx := x - d; xx <= x+d; xx++ {
					drow[xx+d] = true
				}
			}
		}
	}
	out := make([][]span, dh)
	for y := 0; y < dh; y++ {
		raw := rowSpans(dmat[y], dw)
		// shift x back by d so spans are relative to the undilated origin
		for i := range raw {
			raw[i].X0 -= d
			raw[i].X1 -= d
		}
		out[y] = raw
	}
	return out
}

// ---- occupancy bitset helpers ----

func anySet(row []uint64, x0, x1 int) bool {
	if x0 < 0 {
		x0 = 0
	}
	if max := len(row) * 64; x1 > max {
		x1 = max
	}
	if x0 >= x1 {
		return false
	}
	w0, w1 := x0>>6, (x1-1)>>6
	for w := w0; w <= w1; w++ {
		lo := 0
		if w == w0 {
			lo = x0 & 63
		}
		hi := 63
		if w == w1 {
			hi = (x1 - 1) & 63
		}
		var m uint64
		if hi-lo == 63 {
			m = ^uint64(0)
		} else {
			m = ((uint64(1) << uint(hi-lo+1)) - 1) << uint(lo)
		}
		if row[w]&m != 0 {
			return true
		}
	}
	return false
}

func setRange(row []uint64, x0, x1 int) {
	if x0 < 0 {
		x0 = 0
	}
	if max := len(row) * 64; x1 > max {
		x1 = max
	}
	if x0 >= x1 {
		return
	}
	w0, w1 := x0>>6, (x1-1)>>6
	for w := w0; w <= w1; w++ {
		lo := 0
		if w == w0 {
			lo = x0 & 63
		}
		hi := 63
		if w == w1 {
			hi = (x1 - 1) & 63
		}
		var m uint64
		if hi-lo == 63 {
			m = ^uint64(0)
		} else {
			m = ((uint64(1) << uint(hi-lo+1)) - 1) << uint(lo)
		}
		row[w] |= m
	}
}
