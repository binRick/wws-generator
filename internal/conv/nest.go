package conv

import (
	"fmt"
	"math"
)

// NestOptions controls layout onto material sheets. All distances are mm.
type NestOptions struct {
	MaterialW float64
	MaterialH float64
	Spacing   float64   // gap between pieces
	Margin    float64   // border inset between the layout and the material edge
	Grid      float64   // raster resolution (mm per cell)
	Rotations []float64 // candidate rotation angles in degrees

	// EngraveAlign, when set, lets a piece carrying fill-engrave/engrave marks
	// rotate (even when Rotations would not) so its engraving lies in a short,
	// horizontal band — fewer raster scan lines, so the laser engraves it faster.
	EngraveAlign bool

	// GroupEngrave, when set, nests engrave-bearing pieces onto their own
	// sheet(s), separate from cut-only pieces, so all engraving is consolidated.
	GroupEngrave bool
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

// Nest lays out the pieces onto as many sheets as needed and returns the
// placements plus the sheet count. With GroupEngrave set (and a mix of both
// kinds present), cut-only pieces are nested onto their own sheets first, then
// engrave-bearing pieces onto separate sheets after them — so all engraving
// lands together on the later canvas(es) and the cut-only sheets stay clean.
func Nest(pieces []Piece, opt NestOptions) ([]Placement, int, error) {
	if !opt.GroupEngrave {
		return nestPieces(pieces, opt)
	}
	var cutOnly, engrave []Piece
	for _, p := range pieces {
		if len(p.Marks) > 0 {
			engrave = append(engrave, p)
		} else {
			cutOnly = append(cutOnly, p)
		}
	}
	if len(cutOnly) == 0 || len(engrave) == 0 {
		return nestPieces(pieces, opt) // nothing to separate
	}
	cutPl, cutSheets, err := nestPieces(cutOnly, opt)
	if err != nil {
		return nil, 0, err
	}
	engPl, engSheets, err := nestPieces(engrave, opt)
	if err != nil {
		return nil, 0, err
	}
	for i := range engPl {
		engPl[i].Sheet += cutSheets // engraving onto the sheets after the cut-only ones
	}
	return append(cutPl, engPl...), cutSheets + engSheets, nil
}

// nestPieces lays pieces onto a fresh pool of sheets (first-fit-decreasing). It
// errors if any single piece is too large to fit on an empty sheet.
func nestPieces(pieces []Piece, opt NestOptions) ([]Placement, int, error) {
	res := opt.Grid
	if res <= 0 {
		return nil, 0, fmt.Errorf("grid resolution must be positive (got %.3f)", res)
	}
	if opt.MaterialW <= 0 || opt.MaterialH <= 0 {
		return nil, 0, fmt.Errorf("material must be positive (got %.1fx%.1f mm)", opt.MaterialW, opt.MaterialH)
	}
	// The usable nesting area is the sheet inset by Margin on every side (the
	// border against the material edge); the layout is anchored at that inset.
	// Pieces are kept Spacing apart from each other via mask dilation.
	usableW := opt.MaterialW - 2*opt.Margin
	usableH := opt.MaterialH - 2*opt.Margin
	if usableW <= 0 || usableH <= 0 {
		return nil, 0, fmt.Errorf("material %.1fx%.1f mm too small for a %.1f mm margin",
			opt.MaterialW, opt.MaterialH, opt.Margin)
	}
	gw := int(math.Floor(usableW / res))
	gh := int(math.Floor(usableH / res))
	if gw < 1 || gh < 1 {
		return nil, 0, fmt.Errorf("grid resolution %.2f mm too coarse for usable area %.1fx%.1f mm",
			res, usableW, usableH)
	}
	dilate := int(math.Round(opt.Spacing / res))

	base := opt.Rotations
	if len(base) == 0 {
		base = []float64{0}
	}

	var sheets []*sheetGrid
	var placements []Placement

	for i := range pieces {
		p := &pieces[i]

		// Pick candidate rotations. Cut-only pieces use the requested set as-is.
		// A piece with engrave content may additionally rotate to lay that
		// engraving flat (horizontal), but only when doing so meaningfully
		// shortens it — otherwise it stays put, honouring "don't rotate".
		rots := base
		if opt.EngraveAlign && len(p.Marks) > 0 {
			if engraveHeightAt(p, 90) < engraveHeightAt(p, 0)*0.8 {
				rots = engraveRotations(base)
			}
		}

		opts := make([]rotOpt, len(rots))
		for r, deg := range rots {
			opts[r] = rotOpt{deg: deg, mask: buildMask(p.Loops, deg, res, dilate), engH: engraveHeightAt(p, deg)}
		}

		placed := false
		for si := 0; si < len(sheets) && !placed; si++ {
			if best, px, py, ok := bestOnSheet(sheets[si], opts); ok {
				sheets[si].place(opts[best].mask, px, py)
				placements = append(placements, mkPlacement(p, si, opts[best].deg, opts[best].mask, px, py, opt, res))
				placed = true
			}
		}
		if placed {
			continue
		}

		// Open a new sheet.
		sg := newSheet(gw, gh)
		best, px, py, ok := bestOnSheet(sg, opts)
		if !ok {
			eff := p.BBox
			return nil, 0, fmt.Errorf("piece is %.1fx%.1f mm and does not fit on a %.1fx%.1f mm sheet (usable %.1fx%.1f after a %.1f mm margin); enlarge --material or reduce --margin",
				eff.W(), eff.H(), opt.MaterialW, opt.MaterialH, usableW, usableH, opt.Margin)
		}
		sg.place(opts[best].mask, px, py)
		sheets = append(sheets, sg)
		placements = append(placements, mkPlacement(p, len(sheets)-1, opts[best].deg, opts[best].mask, px, py, opt, res))
	}

	return placements, len(sheets), nil
}

// rotOpt is one candidate orientation: its footprint mask plus the height its
// engrave content would occupy (0 if the piece has no engraving).
type rotOpt struct {
	deg  float64
	mask *mask
	engH float64
}

// engraveRotations augments the requested angles with the four axis-aligned
// orientations, so an engrave piece can always be laid flat even under
// --rotations 1.
func engraveRotations(base []float64) []float64 {
	seen := map[float64]bool{}
	var out []float64
	add := func(d float64) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, d := range base {
		add(d)
	}
	for _, d := range []float64{0, 90, 180, 270} {
		add(d)
	}
	return out
}

// engraveHeightAt returns the height (mm) of the piece's combined engrave
// geometry rotated by deg; 0 if the piece carries no marks.
func engraveHeightAt(p *Piece, deg float64) float64 {
	if len(p.Marks) == 0 {
		return 0
	}
	m := RotateDeg(deg)
	var sps []Subpath
	for _, mk := range p.Marks {
		for _, sp := range mk.Subpaths {
			sps = append(sps, sp.transform(m))
		}
	}
	return subpathsBBox(sps).H()
}

// bestOnSheet finds the best orientation+position on a sheet: smallest engrave
// height first (flattest engraving), then top-most, then left-most.
func bestOnSheet(s *sheetGrid, opts []rotOpt) (best, bx, by int, ok bool) {
	bestEngH := math.Inf(1)
	bestY, bestX := math.MaxInt, math.MaxInt
	for r, o := range opts {
		px, py, found := s.findSpot(o.mask)
		if !found {
			continue
		}
		better := false
		switch {
		case !ok:
			better = true
		case o.engH < bestEngH-1e-9:
			better = true
		case o.engH > bestEngH+1e-9:
			better = false
		case py < bestY:
			better = true
		case py > bestY:
			better = false
		case px < bestX:
			better = true
		}
		if better {
			bestEngH, bestY, bestX = o.engH, py, px
			best, bx, by = r, px, py
			ok = true
		}
	}
	return
}

func mkPlacement(p *Piece, sheet int, deg float64, m *mask, px, py int, opt NestOptions, res float64) Placement {
	tx := opt.Margin + float64(px)*res - m.ox
	ty := opt.Margin + float64(py)*res - m.oy
	mat := Translate(tx, ty).Mul(RotateDeg(deg))
	return Placement{Piece: p, Sheet: sheet, M: mat}
}
