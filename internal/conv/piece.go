package conv

import (
	"math"
	"sort"
)

// Piece is one independently-placeable part: an outer boundary plus any holes
// nested directly inside it. Geometry is in local millimetre coordinates.
type Piece struct {
	Subpaths []Subpath // exact geometry (outer + holes), for output
	Loops    [][]Point // flattened loops (outer first, then holes), for nesting
	Area     float64   // |area| of the outer loop, mm^2 (used to order placement)
	BBox     Rect      // exact bounding box of all subpaths
}

// buildPieces groups flat subpaths into pieces using even/odd containment.
// A loop nested inside an even number of other loops is an outer boundary that
// starts a new piece; a loop at odd depth is a hole of its immediate parent.
func buildPieces(subs []Subpath, tol float64) []Piece {
	type loop struct {
		idx    int
		poly   []Point
		bbox   Rect
		area   float64
		inside Point // a point guaranteed inside the loop
		parent int   // index into loops, -1 if none
		depth  int
	}

	var loops []loop
	for i, sp := range subs {
		poly := sp.Flatten(tol)
		if len(poly) < 2 {
			continue
		}
		bb := polylineBBox(poly)
		ip, ok := interiorPoint(poly, bb)
		if !ok {
			// Degenerate / open: use bbox centre as a fallback.
			ip = Point{(bb.MinX + bb.MaxX) / 2, (bb.MinY + bb.MaxY) / 2}
		}
		loops = append(loops, loop{
			idx:    i,
			poly:   poly,
			bbox:   bb,
			area:   math.Abs(polygonArea(poly)),
			inside: ip,
			parent: -1,
		})
	}

	// parent = smallest-area loop that contains this loop.
	for i := range loops {
		best := -1
		bestArea := math.Inf(1)
		for j := range loops {
			if i == j {
				continue
			}
			if loops[j].area <= loops[i].area {
				continue
			}
			if !bboxContains(loops[j].bbox, loops[i].bbox) {
				continue
			}
			if pointInPolygon(loops[i].inside, loops[j].poly) {
				if loops[j].area < bestArea {
					bestArea = loops[j].area
					best = j
				}
			}
		}
		loops[i].parent = best
	}

	// depth = number of ancestors.
	var depthOf func(i int) int
	memo := make(map[int]int)
	depthOf = func(i int) int {
		if d, ok := memo[i]; ok {
			return d
		}
		if loops[i].parent < 0 {
			memo[i] = 0
			return 0
		}
		d := depthOf(loops[i].parent) + 1
		memo[i] = d
		return d
	}
	for i := range loops {
		loops[i].depth = depthOf(i)
	}

	// Each even-depth loop is a piece outer; collect odd-depth children.
	pieceOf := make(map[int]*Piece) // outer loop index -> piece
	var order []int                 // outer loop indices in encounter order
	for i := range loops {
		if loops[i].depth%2 == 0 {
			p := &Piece{}
			pieceOf[i] = p
			order = append(order, i)
		}
	}
	for i := range loops {
		if loops[i].depth%2 == 1 {
			// hole: attach to its parent outer (parent is an even-depth loop)
			if p, ok := pieceOf[loops[i].parent]; ok {
				p.Subpaths = append(p.Subpaths, subs[loops[i].idx])
				p.Loops = append(p.Loops, loops[i].poly)
			}
		}
	}
	// Prepend outers (so Loops[0]/Subpaths[0] is the boundary).
	for _, oi := range order {
		p := pieceOf[oi]
		p.Subpaths = append([]Subpath{subs[loops[oi].idx]}, p.Subpaths...)
		p.Loops = append([][]Point{loops[oi].poly}, p.Loops...)
		p.Area = loops[oi].area
		p.BBox = subpathsBBox(p.Subpaths)
	}

	pieces := make([]Piece, 0, len(order))
	for _, oi := range order {
		pieces = append(pieces, *pieceOf[oi])
	}
	// Largest first — first-fit-decreasing packs better.
	sort.SliceStable(pieces, func(a, b int) bool { return pieces[a].Area > pieces[b].Area })
	return pieces
}

func bboxContains(outer, inner Rect) bool {
	return inner.MinX >= outer.MinX && inner.MaxX <= outer.MaxX &&
		inner.MinY >= outer.MinY && inner.MaxY <= outer.MaxY
}

// interiorPoint returns a point strictly inside a simple polygon using a
// horizontal scanline through the vertical middle of its bounding box.
func interiorPoint(poly []Point, bb Rect) (Point, bool) {
	if len(poly) < 3 {
		return Point{}, false
	}
	y := (bb.MinY + bb.MaxY) / 2
	var xs []float64
	n := len(poly)
	for i := 0; i < n; i++ {
		a := poly[i]
		b := poly[(i+1)%n]
		if (a.Y > y) != (b.Y > y) {
			x := a.X + (y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
			xs = append(xs, x)
		}
	}
	if len(xs) < 2 {
		return Point{}, false
	}
	sort.Float64s(xs)
	// Midpoint of the widest interior span (between consecutive even/odd pairs).
	bestMid, bestW := 0.0, -1.0
	for i := 0; i+1 < len(xs); i += 2 {
		w := xs[i+1] - xs[i]
		if w > bestW {
			bestW = w
			bestMid = (xs[i] + xs[i+1]) / 2
		}
	}
	if bestW <= 0 {
		return Point{}, false
	}
	return Point{bestMid, y}, true
}
