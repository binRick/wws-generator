package conv

import "math"

// Point is a 2D coordinate.
type Point struct{ X, Y float64 }

// Rect is an axis-aligned bounding box.
type Rect struct{ MinX, MinY, MaxX, MaxY float64 }

func (r Rect) W() float64 { return r.MaxX - r.MinX }
func (r Rect) H() float64 { return r.MaxY - r.MinY }

func emptyRect() Rect {
	return Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
}

func (r *Rect) add(p Point) {
	if p.X < r.MinX {
		r.MinX = p.X
	}
	if p.Y < r.MinY {
		r.MinY = p.Y
	}
	if p.X > r.MaxX {
		r.MaxX = p.X
	}
	if p.Y > r.MaxY {
		r.MaxY = p.Y
	}
}

// Matrix is a 2D affine transform [a b c d e f] applied as:
//
//	x' = a*x + c*y + e
//	y' = b*x + d*y + f
//
// This matches the SVG transform matrix(a,b,c,d,e,f) convention.
type Matrix [6]float64

// Identity matrix.
func Identity() Matrix { return Matrix{1, 0, 0, 1, 0, 0} }

// Apply transforms a point.
func (m Matrix) Apply(p Point) Point {
	return Point{
		X: m[0]*p.X + m[2]*p.Y + m[4],
		Y: m[1]*p.X + m[3]*p.Y + m[5],
	}
}

// Mul returns m*n such that (m.Mul(n)).Apply(p) == m.Apply(n.Apply(p)).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func Translate(tx, ty float64) Matrix { return Matrix{1, 0, 0, 1, tx, ty} }
func Scale(sx, sy float64) Matrix     { return Matrix{sx, 0, 0, sy, 0, 0} }

// RotateDeg returns a rotation about the origin, angle in degrees, clockwise
// positive in SVG's y-down coordinate space.
func RotateDeg(deg float64) Matrix {
	r := deg * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	return Matrix{c, s, -s, c, 0, 0}
}

func skewXDeg(deg float64) Matrix { return Matrix{1, 0, math.Tan(deg * math.Pi / 180), 1, 0, 0} }
func skewYDeg(deg float64) Matrix { return Matrix{1, math.Tan(deg * math.Pi / 180), 0, 1, 0, 0} }

// Op is a path command operator. We only ever emit M, L, C, Q, Z.
type Op byte

const (
	OpM Op = 'M'
	OpL Op = 'L'
	OpC Op = 'C'
	OpQ Op = 'Q'
	OpZ Op = 'Z'
)

// Cmd is a single path command with its point arguments:
//
//	M/L -> 1 point, Q -> 2 points (ctrl, end), C -> 3 points (c1, c2, end), Z -> 0.
type Cmd struct {
	Op Op
	P  []Point
}

// Role classifies the laser operation a subpath maps to, derived from its SVG
// stroke/fill. Red stroke -> cut; a non-none fill -> fillEngrave; any other
// stroke -> engrave (line).
type Role int

const (
	RoleCut Role = iota
	RoleEngrave
	RoleFillEngrave
)

// Subpath is a connected run of commands starting with M.
type Subpath struct {
	Cmds   []Cmd
	Closed bool
	Role   Role   // cut / engrave / fillEngrave
	Color  string // resolved hex color (e.g. "#000000"); "" for cut (uses cutRed)
	Elem   int    // source SVG element index, so marks from one element stay grouped
	Group  int    // nearest ancestor <g> index (-1 if none); keeps a layer's marks together
}

// transform applies m to every point of the subpath, returning a new subpath.
func (sp Subpath) transform(m Matrix) Subpath {
	out := Subpath{Closed: sp.Closed, Role: sp.Role, Color: sp.Color, Elem: sp.Elem, Group: sp.Group, Cmds: make([]Cmd, len(sp.Cmds))}
	for i, c := range sp.Cmds {
		nc := Cmd{Op: c.Op, P: make([]Point, len(c.P))}
		for j, p := range c.P {
			nc.P[j] = m.Apply(p)
		}
		out.Cmds[i] = nc
	}
	return out
}

// Flatten converts a subpath into a polyline (slice of points) by subdividing
// curves to within tol millimetres. The polyline is implicitly closed (the
// caller treats first==last as the same vertex for area/containment).
func (sp Subpath) Flatten(tol float64) []Point {
	var pts []Point
	var cur Point
	var start Point
	have := false
	for _, c := range sp.Cmds {
		switch c.Op {
		case OpM:
			cur = c.P[0]
			start = cur
			pts = append(pts, cur)
			have = true
		case OpL:
			cur = c.P[0]
			pts = append(pts, cur)
		case OpQ:
			if have {
				flattenQuad(cur, c.P[0], c.P[1], tol, &pts)
				cur = c.P[1]
			}
		case OpC:
			if have {
				flattenCubic(cur, c.P[0], c.P[1], c.P[2], tol, &pts)
				cur = c.P[2]
			}
		case OpZ:
			cur = start
		}
	}
	return pts
}

func flattenQuad(p0, p1, p2 Point, tol float64, out *[]Point) {
	// Convert quadratic to cubic and reuse the cubic flattener.
	c1 := Point{p0.X + 2.0/3.0*(p1.X-p0.X), p0.Y + 2.0/3.0*(p1.Y-p0.Y)}
	c2 := Point{p2.X + 2.0/3.0*(p1.X-p2.X), p2.Y + 2.0/3.0*(p1.Y-p2.Y)}
	flattenCubic(p0, c1, c2, p2, tol, out)
}

func flattenCubic(p0, p1, p2, p3 Point, tol float64, out *[]Point) {
	// Recursive subdivision based on flatness (max control-point deviation
	// from the chord).
	var rec func(p0, p1, p2, p3 Point, depth int)
	rec = func(p0, p1, p2, p3 Point, depth int) {
		d1 := segDist(p1, p0, p3)
		d2 := segDist(p2, p0, p3)
		if depth >= 24 || d1+d2 <= tol {
			*out = append(*out, p3)
			return
		}
		// de Casteljau split at t=0.5
		p01 := mid(p0, p1)
		p12 := mid(p1, p2)
		p23 := mid(p2, p3)
		p012 := mid(p01, p12)
		p123 := mid(p12, p23)
		m := mid(p012, p123)
		rec(p0, p01, p012, m, depth+1)
		rec(m, p123, p23, p3, depth+1)
	}
	rec(p0, p1, p2, p3, 0)
}

func mid(a, b Point) Point { return Point{(a.X + b.X) / 2, (a.Y + b.Y) / 2} }

// segDist is the perpendicular distance from p to the line through a,b.
func segDist(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	return math.Abs((p.X-a.X)*dy-(p.Y-a.Y)*dx) / l
}

// BBox returns the exact bounding box of a subpath, including Bézier extrema,
// matching how Fabric.js computes path bounds.
func subpathsBBox(sps []Subpath) Rect {
	r := emptyRect()
	var cur Point
	for _, sp := range sps {
		for _, c := range sp.Cmds {
			switch c.Op {
			case OpM, OpL:
				r.add(c.P[0])
				cur = c.P[0]
			case OpQ:
				r.add(c.P[1])
				quadExtrema(cur, c.P[0], c.P[1], &r)
				cur = c.P[1]
			case OpC:
				r.add(c.P[2])
				cubicExtrema(cur, c.P[0], c.P[1], c.P[2], &r)
				cur = c.P[2]
			case OpZ:
			}
		}
	}
	return r
}

func quadExtrema(p0, p1, p2 Point, r *Rect) {
	addQuadAxis(p0.X, p1.X, p2.X, p0.Y, p1.Y, p2.Y, r, true)
	addQuadAxis(p0.Y, p1.Y, p2.Y, p0.X, p1.X, p2.X, r, false)
}

func addQuadAxis(a0, a1, a2, b0, b1, b2 float64, r *Rect, isX bool) {
	den := a0 - 2*a1 + a2
	if den == 0 {
		return
	}
	t := (a0 - a1) / den
	if t <= 0 || t >= 1 {
		return
	}
	u := 1 - t
	av := u*u*a0 + 2*u*t*a1 + t*t*a2
	bv := u*u*b0 + 2*u*t*b1 + t*t*b2
	if isX {
		r.add(Point{X: av, Y: bv})
	} else {
		r.add(Point{X: bv, Y: av})
	}
}

func cubicExtrema(p0, p1, p2, p3 Point, r *Rect) {
	addCubicAxis(p0.X, p1.X, p2.X, p3.X, p0.Y, p1.Y, p2.Y, p3.Y, r, true)
	addCubicAxis(p0.Y, p1.Y, p2.Y, p3.Y, p0.X, p1.X, p2.X, p3.X, r, false)
}

func addCubicAxis(a0, a1, a2, a3, b0, b1, b2, b3 float64, r *Rect, isX bool) {
	// Derivative is quadratic: roots where d/dt = 0.
	A := -a0 + 3*a1 - 3*a2 + a3
	B := 2 * (a0 - 2*a1 + a2)
	C := -a0 + a1
	var ts []float64
	if A == 0 {
		if B != 0 {
			ts = append(ts, -C/B)
		}
	} else {
		disc := B*B - 4*A*C
		if disc >= 0 {
			sq := math.Sqrt(disc)
			ts = append(ts, (-B+sq)/(2*A), (-B-sq)/(2*A))
		}
	}
	for _, t := range ts {
		if t <= 0 || t >= 1 {
			continue
		}
		u := 1 - t
		av := u*u*u*a0 + 3*u*u*t*a1 + 3*u*t*t*a2 + t*t*t*a3
		bv := u*u*u*b0 + 3*u*u*t*b1 + 3*u*t*t*b2 + t*t*t*b3
		if isX {
			r.add(Point{X: av, Y: bv})
		} else {
			r.add(Point{X: bv, Y: av})
		}
	}
}

// polygonArea returns the signed area of a closed polyline (shoelace).
func polygonArea(pts []Point) float64 {
	n := len(pts)
	if n < 3 {
		return 0
	}
	var a float64
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return a / 2
}

// pointInPolygon tests containment via ray casting.
func pointInPolygon(p Point, poly []Point) bool {
	n := len(poly)
	in := false
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		yi, yj := poly[i].Y, poly[j].Y
		if (yi > p.Y) != (yj > p.Y) {
			x := poly[j].X + (p.Y-poly[j].Y)/(poly[i].Y-poly[j].Y)*(poly[i].X-poly[j].X)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

func polylineBBox(pts []Point) Rect {
	r := emptyRect()
	for _, p := range pts {
		r.add(p)
	}
	return r
}
