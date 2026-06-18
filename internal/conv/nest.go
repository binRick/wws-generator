package conv

import (
	"fmt"
	"math"
)

// NestOptions controls layout onto material sheets. All distances are mm.
type NestOptions struct {
	MaterialW float64
	MaterialH float64
	Spacing   float64   // space around items: gap between pieces AND border inset
	Grid      float64   // raster resolution (mm per cell)
	Rotations []float64 // candidate rotation angles in degrees
}

// Placement is a piece positioned on a sheet, with the transform mapping the
// piece's local coordinates to sheet millimetres.
type Placement struct {
	Piece *Piece
	Sheet int
	M     Matrix
}

// sheetGrid is one material sheet's occupancy bitmap.
type sheetGrid struct {
	gw, gh int
	occ    [][]uint64
}

func newSheet(gw, gh int) *sheetGrid {
	words := (gw + 63) / 64
	occ := make([][]uint64, gh)
	for i := range occ {
		occ[i] = make([]uint64, words)
	}
	return &sheetGrid{gw: gw, gh: gh, occ: occ}
}

func (s *sheetGrid) fits(m *mask, px, py int) bool {
	// Undilated footprint must lie inside the usable grid.
	if px < 0 || py < 0 || px+m.w > s.gw || py+m.h > s.gh {
		return false
	}
	for k := 0; k < len(m.dRows); k++ {
		gy := py + k - m.d
		if gy < 0 || gy >= s.gh {
			continue
		}
		for _, sp := range m.dRows[k] {
			if anySet(s.occ[gy], px+sp.X0, px+sp.X1) {
				return false
			}
		}
	}
	return true
}

func (s *sheetGrid) place(m *mask, px, py int) {
	for r := 0; r < m.h; r++ {
		gy := py + r
		for _, sp := range m.rows[r] {
			setRange(s.occ[gy], px+sp.X0, px+sp.X1)
		}
	}
}

// findSpot scans for the top-left-most valid position for the mask. Returns
// (px, py, ok).
func (s *sheetGrid) findSpot(m *mask) (int, int, bool) {
	for py := 0; py+m.h <= s.gh; py++ {
		for px := 0; px+m.w <= s.gw; px++ {
			if s.fits(m, px, py) {
				return px, py, true
			}
		}
	}
	return 0, 0, false
}

// Nest lays out the pieces onto as many sheets as needed. It returns the
// placements and the number of sheets used. It errors if any single piece is
// too large to fit on an empty sheet.
func Nest(pieces []Piece, opt NestOptions) ([]Placement, int, error) {
	res := opt.Grid
	// Items get `Spacing` of clear space on every side, so the usable nesting
	// area is the sheet inset by that border; the layout is anchored top-left.
	usableW := opt.MaterialW - 2*opt.Spacing
	usableH := opt.MaterialH - 2*opt.Spacing
	if usableW <= 0 || usableH <= 0 {
		return nil, 0, fmt.Errorf("material %.1fx%.1f mm too small for %.1f mm space around items",
			opt.MaterialW, opt.MaterialH, opt.Spacing)
	}
	gw := int(math.Floor(usableW / res))
	gh := int(math.Floor(usableH / res))
	if gw < 1 || gh < 1 {
		return nil, 0, fmt.Errorf("grid resolution %.2f mm too coarse for usable area %.1fx%.1f mm",
			res, usableW, usableH)
	}
	dilate := int(math.Round(opt.Spacing / res))

	rotations := opt.Rotations
	if len(rotations) == 0 {
		rotations = []float64{0}
	}

	var sheets []*sheetGrid
	var placements []Placement

	for i := range pieces {
		p := &pieces[i]

		// Precompute a mask per rotation.
		masks := make([]*mask, len(rotations))
		for r, deg := range rotations {
			masks[r] = buildMask(p.Loops, deg, res, dilate)
		}

		placed := false
		for si := 0; si < len(sheets) && !placed; si++ {
			if bestRot, px, py, ok := bestOnSheet(sheets[si], masks); ok {
				sheets[si].place(masks[bestRot], px, py)
				placements = append(placements, mkPlacement(p, si, rotations[bestRot], masks[bestRot], px, py, opt, res))
				placed = true
			}
		}
		if placed {
			continue
		}

		// Open a new sheet.
		sg := newSheet(gw, gh)
		bestRot, px, py, ok := bestOnSheet(sg, masks)
		if !ok {
			eff := p.BBox
			return nil, 0, fmt.Errorf("piece is %.1fx%.1f mm and does not fit on a %.1fx%.1f mm sheet (usable %.1fx%.1f after %.1f mm spacing); enlarge --material or reduce --spacing",
				eff.W(), eff.H(), opt.MaterialW, opt.MaterialH, usableW, usableH, opt.Spacing)
		}
		sg.place(masks[bestRot], px, py)
		sheets = append(sheets, sg)
		placements = append(placements, mkPlacement(p, len(sheets)-1, rotations[bestRot], masks[bestRot], px, py, opt, res))
	}

	return placements, len(sheets), nil
}

// bestOnSheet finds the best rotation+position on a sheet, preferring the
// top-most then left-most placement.
func bestOnSheet(s *sheetGrid, masks []*mask) (rot, bx, by int, ok bool) {
	bestY, bestX := math.MaxInt, math.MaxInt
	for r, m := range masks {
		px, py, found := s.findSpot(m)
		if !found {
			continue
		}
		if py < bestY || (py == bestY && px < bestX) {
			bestY, bestX = py, px
			rot, bx, by = r, px, py
			ok = true
		}
	}
	return
}

func mkPlacement(p *Piece, sheet int, deg float64, m *mask, px, py int, opt NestOptions, res float64) Placement {
	tx := opt.Spacing + float64(px)*res - m.ox
	ty := opt.Spacing + float64(py)*res - m.oy
	mat := Translate(tx, ty).Mul(RotateDeg(deg))
	return Placement{Piece: p, Sheet: sheet, M: mat}
}
