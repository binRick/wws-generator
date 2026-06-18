package conv

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
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
	opt := NestOptions{MaterialW: 130, MaterialH: 130, Spacing: spacing, Grid: 1.0, Rotations: []float64{0, 90, 180, 270}}
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

// --- wws -> detailed json ---

func TestDescribeSample(t *testing.T) {
	data, err := os.ReadFile(sampleWWS)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Describe(data, DescribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "3.0.4" || p.Units != "mm" {
		t.Fatalf("version/units = %q/%q", p.Version, p.Units)
	}
	if len(p.Canvases) != 1 || len(p.Canvases[0].Items) != 1 {
		t.Fatalf("want 1 canvas / 1 item, got %d / %v", len(p.Canvases), p.Canvases)
	}
	it := p.Canvases[0].Items[0]
	if it.Type != "rect" {
		t.Fatalf("item type = %q, want rect", it.Type)
	}
	if it.Laser == nil || it.Laser.Operation != "cut" {
		t.Fatalf("laser = %+v, want operation cut", it.Laser)
	}
	if it.Style.Stroke != "#E61F19" {
		t.Fatalf("stroke = %q, want #E61F19", it.Style.Stroke)
	}
	if it.BBox == nil {
		t.Fatal("item bbox missing")
	}
	// Output must be valid JSON.
	if _, err := p.ToJSON(false); err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
}

// A generated file (svg2wws) decoded back to JSON should expose every piece as a
// cut path with geometry and a transform.
func TestDescribeGenerated(t *testing.T) {
	out, _ := convertSample(t, 300, 200, 3) // 5 pieces, one canvas
	p, err := Describe(out, DescribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Canvases) != 1 || len(p.Canvases[0].Items) != 5 {
		t.Fatalf("want 1 canvas / 5 items, got %d canvases", len(p.Canvases))
	}
	for _, it := range p.Canvases[0].Items {
		if it.Type != "path" {
			t.Fatalf("item type = %q, want path", it.Type)
		}
		if _, ok := it.Geometry["d"].(string); !ok {
			t.Fatalf("path item missing geometry.d: %+v", it.Geometry)
		}
		if it.Laser == nil || it.Laser.Operation != "cut" {
			t.Fatalf("laser = %+v, want cut", it.Laser)
		}
		if it.BBox == nil || it.BBox.X < -0.5 || it.BBox.Y < -0.5 ||
			it.BBox.X+it.BBox.W > 300.5 || it.BBox.Y+it.BBox.H > 200.5 {
			t.Fatalf("item bbox out of material: %+v", it.BBox)
		}
	}
}

// --- end-to-end: svg -> wws (uses the in-repo sample) ---

const sampleSVG = "../../samples/test-parts.svg"
const sampleWWS = "../../samples/square-100.known-good.wws"

func convertSample(t *testing.T, mw, mh, spacing float64) ([]byte, Summary) {
	t.Helper()
	data, err := os.ReadFile(sampleSVG)
	if err != nil {
		t.Fatalf("read sample svg: %v", err)
	}
	out, sum, err := Convert(bytes.NewReader(data), Options{
		Name: "t", MaterialW: mw, MaterialH: mh, Spacing: spacing, Grid: 1.0,
		Rotations: []float64{0, 90, 180, 270}, Time: 1,
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return out, sum
}

func TestConvertEndToEnd(t *testing.T) {
	const spacing = 3.0
	out, sum := convertSample(t, 300, 200, spacing)
	if sum.Pieces != 5 {
		t.Fatalf("pieces = %d, want 5", sum.Pieces)
	}
	var f map[string]any
	if err := json.Unmarshal(out, &f); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if f["version"] != "3.0.4" {
		t.Fatalf("version = %v, want 3.0.4", f["version"])
	}
	// Collect object ids + extents; check within material and top-left anchor.
	canvases := f["canvasList"].([]any)
	ids := map[string]bool{}
	minL, minT := math.Inf(1), math.Inf(1)
	for _, cv := range canvases {
		c := cv.(map[string]any)
		for _, ov := range c["objects"].([]any) {
			o := ov.(map[string]any)
			ids[o["id"].(string)] = true
			L, T := o["left"].(float64), o["top"].(float64)
			W, H := o["width"].(float64), o["height"].(float64)
			if L < -0.5 || T < -0.5 || L+W > 300.5 || T+H > 200.5 {
				t.Fatalf("object outside material: left=%.2f top=%.2f w=%.2f h=%.2f", L, T, W, H)
			}
			minL, minT = math.Min(minL, L), math.Min(minT, T)
		}
	}
	if math.Abs(minL-spacing) > 0.5 || math.Abs(minT-spacing) > 0.5 {
		t.Fatalf("layout not anchored top-left at spacing: min=(%.2f,%.2f), want ~(%.1f,%.1f)", minL, minT, spacing, spacing)
	}
	// processList keys must match the object ids exactly.
	pl := f["processList"].(map[string]any)
	if len(pl) != len(ids) {
		t.Fatalf("processList has %d entries, want %d", len(pl), len(ids))
	}
	for k := range pl {
		if !ids[k] {
			t.Fatalf("processList key %s has no matching object", k)
		}
	}
}

func TestMultiSheetSpill(t *testing.T) {
	_, sum := convertSample(t, 120, 120, 3)
	if sum.Sheets < 2 {
		t.Fatalf("expected spill onto >=2 sheets on a 120x120 sheet, got %d", sum.Sheets)
	}
}

func TestOversizedErrors(t *testing.T) {
	data, err := os.ReadFile(sampleSVG)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Convert(bytes.NewReader(data), Options{
		MaterialW: 50, MaterialH: 50, Spacing: 3, Grid: 1.0, Rotations: []float64{0}, Time: 1,
	})
	if err == nil {
		t.Fatal("expected an error for a piece larger than the sheet")
	}
}

// --- end-to-end: wws -> svg ---

func TestWWSToSVGsSample(t *testing.T) {
	data, err := os.ReadFile(sampleWWS)
	if err != nil {
		t.Fatalf("read sample wws: %v", err)
	}
	out, err := WWSToSVGs(data)
	if err != nil {
		t.Fatalf("wws2svg: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("canvases = %d, want 1", len(out))
	}
	cs := out[0]
	if cs.Empty || cs.Objects != 1 {
		t.Fatalf("expected 1 object, got %d (empty=%v)", cs.Objects, cs.Empty)
	}
	if !strings.Contains(cs.SVG, "<svg") || !strings.Contains(cs.SVG, "<rect") {
		t.Fatalf("SVG missing expected elements:\n%s", cs.SVG)
	}
}

// --- round trip: svg -> wws -> svg keeps every piece ---

func TestRoundTrip(t *testing.T) {
	out, _ := convertSample(t, 300, 200, 3) // 5 pieces, one canvas
	svgs, err := WWSToSVGs(out)
	if err != nil {
		t.Fatalf("wws2svg of generated file: %v", err)
	}
	if len(svgs) != 1 {
		t.Fatalf("canvases = %d, want 1", len(svgs))
	}
	if n := strings.Count(svgs[0].SVG, "<path"); n != 5 {
		t.Fatalf("round-trip path count = %d, want 5", n)
	}
}

// --- wws -> svg transform validation ---

// With angle 0 (any flip), an object's transformed geometry must span exactly
// its Fabric AABB: [left, left+width*scaleX] x [top, top+height*scaleY].
func TestFabricAABBAngle0Flip(t *testing.T) {
	o := map[string]any{"type": "rect", "width": 40.0, "height": 20.0,
		"left": 10.0, "top": 5.0, "scaleX": 2.0, "scaleY": 3.0, "flipX": true}
	em := ownMatrix(o).Mul(Translate(-20, -10))
	bb := emptyRect()
	for _, c := range [][2]float64{{0, 0}, {40, 0}, {40, 20}, {0, 20}} {
		bb.add(em.Apply(Point{c[0], c[1]}))
	}
	for _, pair := range [][2]float64{{bb.MinX, 10}, {bb.MinY, 5}, {bb.MaxX, 90}, {bb.MaxY, 65}} {
		if !approx(pair[0], pair[1]) {
			t.Fatalf("AABB mismatch: got %+v want [10,90]x[5,65]", bb)
		}
	}
}

// Rotation is about the origin point, so the origin corner maps to (left,top)
// regardless of angle/scale, and affine scaling preserves edge lengths.
func TestFabricRotationOrigin(t *testing.T) {
	o := map[string]any{"type": "rect", "width": 40.0, "height": 20.0,
		"left": 10.0, "top": 5.0, "scaleX": 2.0, "scaleY": 1.5, "angle": 37.0}
	em := ownMatrix(o).Mul(Translate(-20, -10))
	p00 := em.Apply(Point{0, 0})
	if !approx(p00.X, 10) || !approx(p00.Y, 5) {
		t.Fatalf("origin corner = %+v, want (10,5)", p00)
	}
	pw := em.Apply(Point{40, 0})
	ph := em.Apply(Point{0, 20})
	if d := math.Hypot(pw.X-p00.X, pw.Y-p00.Y); !approx(d, 80) {
		t.Fatalf("width edge = %.4f, want 80", d)
	}
	if d := math.Hypot(ph.X-p00.X, ph.Y-p00.Y); !approx(d, 30) {
		t.Fatalf("height edge = %.4f, want 30", d)
	}
}

// A grouped child composes through the group matrix: its origin corner lands
// where the group transform maps the child's (left,top) in group space.
func TestFabricGroupCompose(t *testing.T) {
	g := map[string]any{"type": "group", "width": 100.0, "height": 80.0,
		"left": 50.0, "top": 40.0, "angle": 20.0}
	gown := ownMatrix(g)
	child := map[string]any{"type": "rect", "width": 10.0, "height": 10.0,
		"left": -30.0, "top": -20.0, "originX": "center", "originY": "center"}
	childEM := gown.Mul(ownMatrix(child)).Mul(Translate(-5, -5))
	got := childEM.Apply(Point{5, 5}) // center of the rect, origin=center
	want := gown.Apply(Point{-30, -20})
	if !approx(got.X, want.X) || !approx(got.Y, want.Y) {
		t.Fatalf("group-composed centre = %+v, want %+v", got, want)
	}
}
