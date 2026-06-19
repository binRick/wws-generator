package conv

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ConvertDXF converts a DXF drawing into a .wws file. Geometry is read from the
// ENTITIES section; layer name/color decide cut vs engrave (default cut).
func ConvertDXF(data []byte, opt Options) ([]byte, Summary, error) {
	subs, err := ParseDXF(data, opt.Scale)
	if err != nil {
		return nil, Summary{}, err
	}
	if len(subs) == 0 {
		return nil, Summary{}, fmt.Errorf("no drawable geometry found in DXF")
	}
	return convertSubpaths(subs, opt)
}

type dxfPair struct {
	code int
	val  string
}

func dxfPairs(data []byte) ([]dxfPair, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	pairs := make([]dxfPair, 0, len(lines)/2)
	for i := 0; i+1 < len(lines); i += 2 {
		cs := strings.TrimSpace(lines[i])
		if cs == "" {
			break // trailing blank / end of file
		}
		code, err := strconv.Atoi(cs)
		if err != nil {
			return nil, fmt.Errorf("dxf: bad group code %q", cs)
		}
		pairs = append(pairs, dxfPair{code, lines[i+1]})
	}
	return pairs, nil
}

// dxfUnitScale maps the $INSUNITS header value to a mm factor.
func dxfUnitScale(units int) float64 {
	switch units {
	case 1: // inches
		return 25.4
	case 2: // feet
		return 304.8
	case 4: // mm
		return 1
	case 5: // cm
		return 10
	case 6: // m
		return 1000
	default: // unitless / unknown: assume mm (typical for laser DXF)
		return 1
	}
}

// ParseDXF reads a DXF document and returns its geometry as subpaths in
// millimetres (Y flipped from CAD's y-up to screen y-down). scaleOverride, if
// > 0, forces the drawing-unit -> mm factor; otherwise $INSUNITS is used.
func ParseDXF(data []byte, scaleOverride float64) ([]Subpath, error) {
	pairs, err := dxfPairs(data)
	if err != nil {
		return nil, err
	}

	units := 0
	layerColor := map[string]int{}
	entStart, entEnd := -1, -1
	section := ""
	for i := 0; i < len(pairs); i++ {
		p := pairs[i]
		switch {
		case p.code == 0 && p.val == "SECTION":
			if i+1 < len(pairs) && pairs[i+1].code == 2 {
				section = pairs[i+1].val
			}
			if section == "ENTITIES" {
				entStart = i + 2
			}
		case p.code == 0 && p.val == "ENDSEC":
			if section == "ENTITIES" && entEnd < 0 {
				entEnd = i
			}
			section = ""
		case section == "HEADER" && p.code == 9 && p.val == "$INSUNITS":
			if i+1 < len(pairs) && pairs[i+1].code == 70 {
				units, _ = strconv.Atoi(strings.TrimSpace(pairs[i+1].val))
			}
		case section == "TABLES" && p.code == 0 && p.val == "LAYER":
			name, col := "", 7
			for j := i + 1; j < len(pairs) && pairs[j].code != 0; j++ {
				switch pairs[j].code {
				case 2:
					name = pairs[j].val
				case 62:
					col, _ = strconv.Atoi(strings.TrimSpace(pairs[j].val))
				}
			}
			if name != "" {
				layerColor[name] = col
			}
		}
	}
	if entStart < 0 || entEnd < 0 || entStart > entEnd {
		return nil, fmt.Errorf("dxf: no ENTITIES section found")
	}

	scale := scaleOverride
	if scale <= 0 {
		scale = dxfUnitScale(units)
	}
	m := Scale(scale, -scale) // mm scale + CAD y-up -> screen y-down

	// Split the ENTITIES section into one group per entity (code 0 starts each).
	type entity struct {
		typ   string
		pairs []dxfPair
	}
	var ents []entity
	for _, p := range pairs[entStart:entEnd] {
		if p.code == 0 {
			ents = append(ents, entity{typ: p.val})
		} else if len(ents) > 0 {
			ents[len(ents)-1].pairs = append(ents[len(ents)-1].pairs, p)
		}
	}

	groupOf := map[string]int{}
	groupIdx := func(layer string) int {
		if g, ok := groupOf[layer]; ok {
			return g
		}
		g := len(groupOf)
		groupOf[layer] = g
		return g
	}

	var subs []Subpath
	elem := 0
	emit := func(sp Subpath, layer string, aci int) {
		if len(sp.Cmds) == 0 {
			return
		}
		role, color := dxfRole(layer, aci)
		sp.Role, sp.Color, sp.Elem, sp.Group = role, color, elem, groupIdx(layer)
		subs = append(subs, sp.transform(m))
		elem++
	}

	for i := 0; i < len(ents); i++ {
		e := ents[i]
		layer := pairStr(e.pairs, 8, "0")
		aci := pairInt(e.pairs, 62, 256)
		if aci == 256 || aci == 0 { // BYLAYER / BYBLOCK
			aci = layerColor[layer]
			if aci == 0 {
				aci = 7
			}
		}
		switch e.typ {
		case "LINE":
			sp := Subpath{Cmds: []Cmd{
				{Op: OpM, P: []Point{{pairF(e.pairs, 10, 0), pairF(e.pairs, 20, 0)}}},
				{Op: OpL, P: []Point{{pairF(e.pairs, 11, 0), pairF(e.pairs, 21, 0)}}},
			}}
			emit(sp, layer, aci)
		case "LWPOLYLINE":
			emit(lwPolyline(e.pairs), layer, aci)
		case "POLYLINE":
			// Old-style: following VERTEX entities, terminated by SEQEND.
			closed := pairInt(e.pairs, 70, 0)&1 != 0
			var verts []vertex
			for i+1 < len(ents) && ents[i+1].typ == "VERTEX" {
				i++
				verts = append(verts, vertex{
					p:     Point{pairF(ents[i].pairs, 10, 0), pairF(ents[i].pairs, 20, 0)},
					bulge: pairF(ents[i].pairs, 42, 0),
				})
			}
			if i+1 < len(ents) && ents[i+1].typ == "SEQEND" {
				i++
			}
			emit(polylineFrom(verts, closed), layer, aci)
		case "CIRCLE":
			cx, cy, r := pairF(e.pairs, 10, 0), pairF(e.pairs, 20, 0), pairF(e.pairs, 40, 0)
			if r > 0 {
				emit(ellipseAt(cx, cy, r, r)[0], layer, aci)
			}
		case "ARC":
			emit(arcEntity(e.pairs), layer, aci)
		case "ELLIPSE":
			emit(ellipseEntity(e.pairs), layer, aci)
		case "SPLINE":
			emit(splineEntity(e.pairs), layer, aci)
		}
	}
	return subs, nil
}

// dxfRole maps a layer name / color index to a laser operation. Names hinting at
// engraving win; otherwise everything cuts (the safe laser default).
func dxfRole(layer string, aci int) (Role, string) {
	l := strings.ToLower(layer)
	switch {
	case strings.Contains(l, "engrav") || strings.Contains(l, "raster") || strings.Contains(l, "fill"):
		return RoleFillEngrave, "#000000"
	case strings.Contains(l, "score") || strings.Contains(l, "mark"):
		return RoleEngrave, "#0000FF"
	default:
		return RoleCut, cutRed
	}
}

type vertex struct {
	p     Point
	bulge float64
}

func lwPolyline(pairs []dxfPair) Subpath {
	closed := pairInt(pairs, 70, 0)&1 != 0
	var verts []vertex
	var cur *vertex
	for _, p := range pairs {
		switch p.code {
		case 10:
			verts = append(verts, vertex{p: Point{X: atof(p.val)}})
			cur = &verts[len(verts)-1]
		case 20:
			if cur != nil {
				cur.p.Y = atof(p.val)
			}
		case 42:
			if cur != nil {
				cur.bulge = atof(p.val)
			}
		}
	}
	return polylineFrom(verts, closed)
}

func polylineFrom(verts []vertex, closed bool) Subpath {
	if len(verts) == 0 {
		return Subpath{}
	}
	sp := Subpath{Closed: closed}
	sp.Cmds = append(sp.Cmds, Cmd{Op: OpM, P: []Point{verts[0].p}})
	n := len(verts)
	last := n - 1
	if closed {
		last = n
	}
	for i := 0; i < last; i++ {
		a := verts[i]
		b := verts[(i+1)%n]
		if a.bulge != 0 {
			sp.Cmds = append(sp.Cmds, bulgeCubics(a.p, b.p, a.bulge)...)
		} else {
			sp.Cmds = append(sp.Cmds, Cmd{Op: OpL, P: []Point{b.p}})
		}
	}
	if closed {
		sp.Cmds = append(sp.Cmds, Cmd{Op: OpZ})
	}
	return sp
}

// bulgeCubics returns the cubic commands for an LWPOLYLINE bulge arc from p1 to
// p2 (bulge = tan(theta/4), positive = counter-clockwise).
func bulgeCubics(p1, p2 Point, bulge float64) []Cmd {
	dx, dy := p2.X-p1.X, p2.Y-p1.Y
	chord := math.Hypot(dx, dy)
	if chord == 0 || bulge == 0 {
		return []Cmd{{Op: OpL, P: []Point{p2}}}
	}
	theta := 4 * math.Atan(bulge) // signed included angle
	r := chord / (2 * math.Sin(theta/2))
	mx, my := (p1.X+p2.X)/2, (p1.Y+p2.Y)/2
	apothem := r * math.Cos(theta/2)
	// left normal of p1->p2
	nx, ny := -dy/chord, dx/chord
	cx, cy := mx+nx*apothem, my+ny*apothem
	rr := math.Abs(r)
	a0 := math.Atan2(p1.Y-cy, p1.X-cx)
	return arcCubics(cx, cy, rr, rr, a0, a0+theta)
}

func arcEntity(pairs []dxfPair) Subpath {
	cx, cy, r := pairF(pairs, 10, 0), pairF(pairs, 20, 0), pairF(pairs, 40, 0)
	if r <= 0 {
		return Subpath{}
	}
	a0 := pairF(pairs, 50, 0) * math.Pi / 180
	a1 := pairF(pairs, 51, 0) * math.Pi / 180
	for a1 <= a0 { // DXF arcs sweep CCW from start to end
		a1 += 2 * math.Pi
	}
	p0 := Point{cx + r*math.Cos(a0), cy + r*math.Sin(a0)}
	sp := Subpath{Cmds: []Cmd{{Op: OpM, P: []Point{p0}}}}
	sp.Cmds = append(sp.Cmds, arcCubics(cx, cy, r, r, a0, a1)...)
	return sp
}

func ellipseEntity(pairs []dxfPair) Subpath {
	cx, cy := pairF(pairs, 10, 0), pairF(pairs, 20, 0)
	mx, my := pairF(pairs, 11, 0), pairF(pairs, 21, 0) // major axis endpoint, relative to center
	ratio := pairF(pairs, 40, 1)
	a0 := pairF(pairs, 41, 0)
	a1 := pairF(pairs, 42, 2*math.Pi)
	majLen := math.Hypot(mx, my)
	if majLen == 0 {
		return Subpath{}
	}
	minLen := majLen * ratio
	rot := math.Atan2(my, mx) // major-axis rotation
	cosR, sinR := math.Cos(rot), math.Sin(rot)
	if a1 <= a0 {
		a1 += 2 * math.Pi
	}
	// Sample the parametric ellipse (param is on the unrotated ellipse).
	n := 64
	pt := func(t float64) Point {
		ex, ey := majLen*math.Cos(t), minLen*math.Sin(t)
		return Point{cx + ex*cosR - ey*sinR, cy + ex*sinR + ey*cosR}
	}
	sp := Subpath{Cmds: []Cmd{{Op: OpM, P: []Point{pt(a0)}}}}
	for i := 1; i <= n; i++ {
		t := a0 + (a1-a0)*float64(i)/float64(n)
		sp.Cmds = append(sp.Cmds, Cmd{Op: OpL, P: []Point{pt(t)}})
	}
	if math.Abs((a1-a0)-2*math.Pi) < 1e-6 {
		sp.Closed = true
		sp.Cmds = append(sp.Cmds, Cmd{Op: OpZ})
	}
	return sp
}

func splineEntity(pairs []dxfPair) Subpath {
	degree := pairInt(pairs, 71, 3)
	flags := pairInt(pairs, 70, 0)
	var knots, weights []float64
	var ctrl, fit []Point
	var cx *Point
	var fx *Point
	for _, p := range pairs {
		switch p.code {
		case 40:
			knots = append(knots, atof(p.val))
		case 41:
			weights = append(weights, atof(p.val))
		case 10:
			ctrl = append(ctrl, Point{X: atof(p.val)})
			cx = &ctrl[len(ctrl)-1]
		case 20:
			if cx != nil {
				cx.Y = atof(p.val)
			}
		case 11:
			fit = append(fit, Point{X: atof(p.val)})
			fx = &fit[len(fit)-1]
		case 21:
			if fx != nil {
				fx.Y = atof(p.val)
			}
		}
	}
	closed := flags&1 != 0

	var pts []Point
	switch {
	case len(ctrl) > degree && degree >= 1:
		samples := len(ctrl) * 12
		if samples < 32 {
			samples = 32
		}
		if samples > 600 {
			samples = 600
		}
		pts = nurbsCurve(degree, knots, ctrl, weights, samples)
	case len(fit) >= 2:
		pts = fit // fall back to the interpolation (fit) points
	case len(ctrl) >= 2:
		pts = ctrl // degenerate: connect control points
	default:
		return Subpath{}
	}
	if len(pts) < 2 {
		return Subpath{}
	}
	sp := Subpath{Closed: closed, Cmds: []Cmd{{Op: OpM, P: []Point{pts[0]}}}}
	for _, p := range pts[1:] {
		sp.Cmds = append(sp.Cmds, Cmd{Op: OpL, P: []Point{p}})
	}
	if closed {
		sp.Cmds = append(sp.Cmds, Cmd{Op: OpZ})
	}
	return sp
}

// nurbsCurve samples a (rational) B-spline via de Boor's algorithm.
func nurbsCurve(degree int, knots []float64, ctrl []Point, weights []float64, samples int) []Point {
	n := len(ctrl)
	if len(weights) != n {
		weights = make([]float64, n)
		for i := range weights {
			weights[i] = 1
		}
	}
	if len(knots) < n+degree+1 {
		return append([]Point(nil), ctrl...) // malformed: fall back to control polygon
	}
	u0, u1 := knots[degree], knots[n]
	if u1 <= u0 {
		return append([]Point(nil), ctrl...)
	}
	out := make([]Point, 0, samples+1)
	for s := 0; s <= samples; s++ {
		u := u0 + (u1-u0)*float64(s)/float64(samples)
		if s == samples {
			u = u1 - (u1-u0)*1e-9
		}
		out = append(out, deBoor(degree, knots, ctrl, weights, u))
	}
	return out
}

func deBoor(p int, U []float64, P []Point, w []float64, u float64) Point {
	n := len(P)
	k := p
	for k < n-1 && u >= U[k+1] {
		k++
	}
	dx := make([]float64, p+1)
	dy := make([]float64, p+1)
	dw := make([]float64, p+1)
	for j := 0; j <= p; j++ {
		idx := k - p + j
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		dx[j] = P[idx].X * w[idx]
		dy[j] = P[idx].Y * w[idx]
		dw[j] = w[idx]
	}
	for r := 1; r <= p; r++ {
		for j := p; j >= r; j-- {
			i := k - p + j
			den := U[i+p-r+1] - U[i]
			a := 0.0
			if den != 0 {
				a = (u - U[i]) / den
			}
			dx[j] = (1-a)*dx[j-1] + a*dx[j]
			dy[j] = (1-a)*dy[j-1] + a*dy[j]
			dw[j] = (1-a)*dw[j-1] + a*dw[j]
		}
	}
	if dw[p] == 0 {
		return Point{dx[p], dy[p]}
	}
	return Point{dx[p] / dw[p], dy[p] / dw[p]}
}

// arcCubics approximates an elliptical arc from angle a0 to a1 (radians) as
// cubic Bézier segments of <= 90 degrees.
func arcCubics(cx, cy, rx, ry, a0, a1 float64) []Cmd {
	sweep := a1 - a0
	n := int(math.Ceil(math.Abs(sweep) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	d := sweep / float64(n)
	k := 4.0 / 3.0 * math.Tan(d/4)
	var cmds []Cmd
	a := a0
	for i := 0; i < n; i++ {
		b := a + d
		p0 := Point{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
		p1 := Point{cx + rx*math.Cos(b), cy + ry*math.Sin(b)}
		c1 := Point{p0.X - k*rx*math.Sin(a), p0.Y + k*ry*math.Cos(a)}
		c2 := Point{p1.X + k*rx*math.Sin(b), p1.Y - k*ry*math.Cos(b)}
		cmds = append(cmds, Cmd{Op: OpC, P: []Point{c1, c2, p1}})
		a = b
	}
	return cmds
}

// --- DXF pair accessors ---

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func pairF(pairs []dxfPair, code int, def float64) float64 {
	for _, p := range pairs {
		if p.code == code {
			return atof(p.val)
		}
	}
	return def
}

func pairInt(pairs []dxfPair, code, def int) int {
	for _, p := range pairs {
		if p.code == code {
			if v, err := strconv.Atoi(strings.TrimSpace(p.val)); err == nil {
				return v
			}
		}
	}
	return def
}

func pairStr(pairs []dxfPair, code int, def string) string {
	for _, p := range pairs {
		if p.code == code {
			return p.val
		}
	}
	return def
}
