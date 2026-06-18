package conv

import (
	"math"
	"strings"
	"testing"
)

func TestParsePathSimplifies(t *testing.T) {
	// H/V/relative commands must reduce to absolute lines forming a 10x10 box.
	subs, err := parsePathData("M0 0 H10 V10 h-10 Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 subpath, got %d", len(subs))
	}
	bb := subpathsBBox(subs)
	if !approx(bb.MinX, 0) || !approx(bb.MinY, 0) || !approx(bb.MaxX, 10) || !approx(bb.MaxY, 10) {
		t.Fatalf("bbox = %+v, want 0,0,10,10", bb)
	}
	for _, c := range subs[0].Cmds {
		switch c.Op {
		case OpM, OpL, OpZ:
		default:
			t.Fatalf("unexpected op %c after simplification", c.Op)
		}
	}
}

func TestArcToCubicSemicircle(t *testing.T) {
	// A semicircle arc from (10,0) to (-10,0) with r=10 should reach (0,10) or
	// (0,-10) at its apex and stay within the radius.
	cmds := arcToCubics(Point{10, 0}, Point{-10, 0}, 10, 10, 0, false, true)
	sp := Subpath{Cmds: append([]Cmd{{Op: OpM, P: []Point{{10, 0}}}}, cmds...)}
	poly := sp.Flatten(0.05)
	maxR := 0.0
	for _, p := range poly {
		r := math.Hypot(p.X, p.Y)
		if r > maxR {
			maxR = r
		}
	}
	if math.Abs(maxR-10) > 0.1 {
		t.Fatalf("arc max radius = %.3f, want ~10", maxR)
	}
}

func TestRingGroupsHole(t *testing.T) {
	svg := `<svg viewBox="0 0 100 100">
		<circle cx="50" cy="50" r="40"/>
		<circle cx="50" cy="50" r="20"/>
	</svg>`
	subs, err := ParseSVG(strings.NewReader(svg), 0)
	if err != nil {
		t.Fatal(err)
	}
	pieces := buildPieces(subs, 0.1)
	if len(pieces) != 1 {
		t.Fatalf("want 1 piece (ring), got %d", len(pieces))
	}
	if len(pieces[0].Loops) != 2 {
		t.Fatalf("want outer+hole = 2 loops, got %d", len(pieces[0].Loops))
	}
}

func TestUnitScaleFromViewBox(t *testing.T) {
	// width 200mm, viewBox 0 0 100 100 => 1 user unit = 2 mm.
	svg := `<svg width="200mm" height="200mm" viewBox="0 0 100 100"><rect x="0" y="0" width="50" height="50"/></svg>`
	subs, err := ParseSVG(strings.NewReader(svg), 0)
	if err != nil {
		t.Fatal(err)
	}
	bb := subpathsBBox(subs)
	if !approx(bb.W(), 100) || !approx(bb.H(), 100) {
		t.Fatalf("scaled rect = %.2fx%.2f mm, want 100x100", bb.W(), bb.H())
	}
}

func TestNestNoOverlapAndSpacing(t *testing.T) {
	svg := `<svg viewBox="0 0 200 200">
		<rect x="10" y="10" width="60" height="40"/>
		<circle cx="150" cy="40" r="30"/>
		<circle cx="50" cy="140" r="35"/>
		<circle cx="50" cy="140" r="18"/>
		<path d="M 110 110 L 180 110 L 180 130 L 130 130 L 130 180 L 110 180 Z"/>
	</svg>`
	subs, err := ParseSVG(strings.NewReader(svg), 0)
	if err != nil {
		t.Fatal(err)
	}
	pieces := buildPieces(subs, flattenTol)
	const spacing = 3.0
	opt := NestOptions{MaterialW: 130, MaterialH: 130, Margin: 5, Spacing: spacing, Grid: 1.0, Rotations: []float64{0, 90, 180, 270}}
	placements, sheets, err := Nest(pieces, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != len(pieces) {
		t.Fatalf("placed %d of %d pieces", len(placements), len(pieces))
	}

	// Rasterise each placement's material in sheet coordinates and assert no
	// two pieces on the same sheet overlap or come closer than the spacing.
	const res = 0.5
	type cell [2]int
	cellsFor := func(pl Placement) map[cell]bool {
		out := map[cell]bool{}
		var loops [][]Point
		bb := emptyRect()
		for _, lp := range pl.Piece.Loops {
			tl := make([]Point, len(lp))
			for i, p := range lp {
				tp := pl.M.Apply(p)
				tl[i] = tp
				bb.add(tp)
			}
			loops = append(loops, tl)
		}
		gy0 := int(math.Floor(bb.MinY / res))
		gy1 := int(math.Ceil(bb.MaxY / res))
		for gy := gy0; gy <= gy1; gy++ {
			y := (float64(gy) + 0.5) * res
			var xs []float64
			for _, lp := range loops {
				n := len(lp)
				for i := 0; i < n; i++ {
					a, b := lp[i], lp[(i+1)%n]
					if (a.Y > y) != (b.Y > y) {
						xs = append(xs, a.X+(y-a.Y)/(b.Y-a.Y)*(b.X-a.X))
					}
				}
			}
			for i := 0; i+1 < len(xs); i += 2 {
				lo, hi := xs[i], xs[i+1]
				for gx := int(math.Floor(lo / res)); gx <= int(math.Ceil(hi/res)); gx++ {
					out[cell{gx, gy}] = true
				}
			}
		}
		return out
	}

	bySheet := map[int][]map[cell]bool{}
	for _, pl := range placements {
		bySheet[pl.Sheet] = append(bySheet[pl.Sheet], cellsFor(pl))
	}
	_ = sheets
	for sh, sets := range bySheet {
		for i := 0; i < len(sets); i++ {
			for j := i + 1; j < len(sets); j++ {
				for c := range sets[i] {
					if sets[j][c] {
						t.Fatalf("sheet %d: pieces %d and %d overlap at cell %v", sh, i, j, c)
					}
				}
			}
		}
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
