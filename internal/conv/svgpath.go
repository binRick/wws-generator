package conv

import (
	"fmt"
	"math"
	"strconv"
)

// parsePathData parses an SVG path "d" attribute into a list of subpaths whose
// commands are all absolute and limited to M/L/C/Q/Z. Coordinates are in the
// element's local user space (no document transform applied yet).
func parsePathData(d string) ([]Subpath, error) {
	toks, err := tokenizePath(d)
	if err != nil {
		return nil, err
	}
	p := &pathState{toks: toks}
	return p.run()
}

type pathState struct {
	toks []pathTok
	i    int

	subs    []Subpath
	cur     *Subpath
	pen     Point // current point
	start   Point // subpath start
	prevC   Point // last cubic control reflection source
	prevQ   Point // last quad control reflection source
	prevCmd byte
}

func (p *pathState) run() ([]Subpath, error) {
	for p.i < len(p.toks) {
		t := p.toks[p.i]
		if !t.isCmd {
			return nil, fmt.Errorf("path: expected command, got number %v", t.num)
		}
		cmd := t.cmd
		p.i++
		if err := p.exec(cmd); err != nil {
			return nil, err
		}
	}
	p.flush()
	return p.subs, nil
}

func (p *pathState) flush() {
	if p.cur != nil && len(p.cur.Cmds) > 0 {
		p.subs = append(p.subs, *p.cur)
	}
	p.cur = nil
}

func (p *pathState) emit(c Cmd) {
	if p.cur == nil {
		p.cur = &Subpath{}
	}
	p.cur.Cmds = append(p.cur.Cmds, c)
}

// number reads the next token, requiring it to be a number.
func (p *pathState) number() (float64, error) {
	if p.i >= len(p.toks) || p.toks[p.i].isCmd {
		return 0, fmt.Errorf("path: expected number")
	}
	v := p.toks[p.i].num
	p.i++
	return v, nil
}

// flag reads an arc flag (0 or 1).
func (p *pathState) flag() (float64, error) {
	return p.number()
}

// peekIsNumber reports whether more coordinate pairs follow (for implicit repeats).
func (p *pathState) peekIsNumber() bool {
	return p.i < len(p.toks) && !p.toks[p.i].isCmd
}

func (p *pathState) exec(cmd byte) error {
	rel := cmd >= 'a' && cmd <= 'z'
	abs := func(x, y float64) Point {
		if rel {
			return Point{p.pen.X + x, p.pen.Y + y}
		}
		return Point{x, y}
	}

	switch upper(cmd) {
	case 'M':
		x, err := p.number()
		if err != nil {
			return err
		}
		y, err := p.number()
		if err != nil {
			return err
		}
		p.flush()
		pt := abs(x, y)
		p.pen, p.start = pt, pt
		p.emit(Cmd{Op: OpM, P: []Point{pt}})
		// Subsequent pairs after M are implicit L.
		for p.peekIsNumber() {
			x, _ := p.number()
			y, err := p.number()
			if err != nil {
				return err
			}
			pt := abs(x, y)
			p.pen = pt
			p.emit(Cmd{Op: OpL, P: []Point{pt}})
		}
	case 'L':
		for {
			x, err := p.number()
			if err != nil {
				return err
			}
			y, err := p.number()
			if err != nil {
				return err
			}
			pt := abs(x, y)
			p.pen = pt
			p.emit(Cmd{Op: OpL, P: []Point{pt}})
			if !p.peekIsNumber() {
				break
			}
		}
	case 'H':
		for {
			x, err := p.number()
			if err != nil {
				return err
			}
			var pt Point
			if rel {
				pt = Point{p.pen.X + x, p.pen.Y}
			} else {
				pt = Point{x, p.pen.Y}
			}
			p.pen = pt
			p.emit(Cmd{Op: OpL, P: []Point{pt}})
			if !p.peekIsNumber() {
				break
			}
		}
	case 'V':
		for {
			y, err := p.number()
			if err != nil {
				return err
			}
			var pt Point
			if rel {
				pt = Point{p.pen.X, p.pen.Y + y}
			} else {
				pt = Point{p.pen.X, y}
			}
			p.pen = pt
			p.emit(Cmd{Op: OpL, P: []Point{pt}})
			if !p.peekIsNumber() {
				break
			}
		}
	case 'C':
		for {
			c1, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			c2, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			end, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			p.emit(Cmd{Op: OpC, P: []Point{c1, c2, end}})
			p.prevC = c2
			p.pen = end
			if !p.peekIsNumber() {
				break
			}
		}
	case 'S':
		for {
			c2, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			end, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			c1 := p.pen
			if upper(p.prevCmd) == 'C' || upper(p.prevCmd) == 'S' {
				c1 = Point{2*p.pen.X - p.prevC.X, 2*p.pen.Y - p.prevC.Y}
			}
			p.emit(Cmd{Op: OpC, P: []Point{c1, c2, end}})
			p.prevC = c2
			p.pen = end
			p.prevCmd = 'C'
			if !p.peekIsNumber() {
				break
			}
		}
		return nil
	case 'Q':
		for {
			c1, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			end, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			p.emit(Cmd{Op: OpQ, P: []Point{c1, end}})
			p.prevQ = c1
			p.pen = end
			if !p.peekIsNumber() {
				break
			}
		}
	case 'T':
		for {
			end, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			c1 := p.pen
			if upper(p.prevCmd) == 'Q' || upper(p.prevCmd) == 'T' {
				c1 = Point{2*p.pen.X - p.prevQ.X, 2*p.pen.Y - p.prevQ.Y}
			}
			p.emit(Cmd{Op: OpQ, P: []Point{c1, end}})
			p.prevQ = c1
			p.pen = end
			p.prevCmd = 'Q'
			if !p.peekIsNumber() {
				break
			}
		}
		return nil
	case 'A':
		for {
			rx, err := p.number()
			if err != nil {
				return err
			}
			ry, err := p.number()
			if err != nil {
				return err
			}
			rot, err := p.number()
			if err != nil {
				return err
			}
			large, err := p.flag()
			if err != nil {
				return err
			}
			sweep, err := p.flag()
			if err != nil {
				return err
			}
			end, err := p.readPoint(abs)
			if err != nil {
				return err
			}
			for _, c := range arcToCubics(p.pen, end, rx, ry, rot, large != 0, sweep != 0) {
				p.emit(c)
			}
			p.pen = end
			if !p.peekIsNumber() {
				break
			}
		}
	case 'Z':
		if p.cur != nil {
			p.cur.Closed = true
			p.emit(Cmd{Op: OpZ})
		}
		p.pen = p.start
	default:
		return fmt.Errorf("path: unknown command %q", cmd)
	}
	p.prevCmd = cmd
	return nil
}

func (p *pathState) readPoint(abs func(x, y float64) Point) (Point, error) {
	x, err := p.number()
	if err != nil {
		return Point{}, err
	}
	y, err := p.number()
	if err != nil {
		return Point{}, err
	}
	return abs(x, y), nil
}

func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

// pathTok is either a command letter or a number.
type pathTok struct {
	isCmd bool
	cmd   byte
	num   float64
}

func tokenizePath(d string) ([]pathTok, error) {
	var toks []pathTok
	i, n := 0, len(d)
	for i < n {
		c := d[i]
		switch {
		case c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isCmdLetter(c):
			toks = append(toks, pathTok{isCmd: true, cmd: c})
			i++
		case c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9'):
			j, v, err := readNumber(d, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, pathTok{num: v})
			i = j
		default:
			return nil, fmt.Errorf("path: unexpected character %q at %d", c, i)
		}
	}
	return toks, nil
}

func isCmdLetter(c byte) bool {
	switch upper(c) {
	case 'M', 'L', 'H', 'V', 'C', 'S', 'Q', 'T', 'A', 'Z':
		return true
	}
	return false
}

// readNumber parses a (possibly signed, possibly exponential) float starting at i.
func readNumber(s string, i int) (int, float64, error) {
	start := i
	n := len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	seenDot := false
	for i < n {
		c := s[i]
		if c >= '0' && c <= '9' {
			i++
		} else if c == '.' && !seenDot {
			seenDot = true
			i++
		} else if c == 'e' || c == 'E' {
			i++
			if i < n && (s[i] == '+' || s[i] == '-') {
				i++
			}
		} else {
			break
		}
	}
	v, err := strconv.ParseFloat(s[start:i], 64)
	if err != nil {
		return start, 0, fmt.Errorf("path: bad number %q", s[start:i])
	}
	return i, v, nil
}

// arcToCubics converts an SVG elliptical arc (endpoint parameterization) to a
// series of cubic Bézier commands.
func arcToCubics(p0, p1 Point, rx, ry, xRotDeg float64, largeArc, sweep bool) []Cmd {
	if rx == 0 || ry == 0 {
		return []Cmd{{Op: OpL, P: []Point{p1}}}
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	phi := xRotDeg * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)

	// Step 1: compute (x1', y1')
	dx := (p0.X - p1.X) / 2
	dy := (p0.Y - p1.Y) / 2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy

	// Correct out-of-range radii.
	lambda := (x1p*x1p)/(rx*rx) + (y1p*y1p)/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}

	// Step 2: compute center (cx', cy')
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den != 0 {
		co = math.Sqrt(math.Max(0, num/den))
	}
	if largeArc == sweep {
		co = -co
	}
	cxp := co * (rx * y1p / ry)
	cyp := co * (-ry * x1p / rx)

	// Step 3: compute center (cx, cy)
	cx := cosP*cxp - sinP*cyp + (p0.X+p1.X)/2
	cy := sinP*cxp + cosP*cyp + (p0.Y+p1.Y)/2

	// Step 4: compute start and sweep angles.
	ang := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		l := math.Hypot(ux, uy) * math.Hypot(vx, vy)
		a := math.Acos(clamp(dot/l, -1, 1))
		if ux*vy-uy*vx < 0 {
			return -a
		}
		return a
	}
	theta1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	// Split into segments of <= 90 degrees.
	segs := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 2)))
	if segs == 0 {
		segs = 1
	}
	delta := dTheta / float64(segs)
	t := 4.0 / 3.0 * math.Tan(delta/4)

	var cmds []Cmd
	theta := theta1
	cur := p0
	for i := 0; i < segs; i++ {
		theta2 := theta + delta
		cosT1, sinT1 := math.Cos(theta), math.Sin(theta)
		cosT2, sinT2 := math.Cos(theta2), math.Sin(theta2)

		ep := ellipsePoint(cx, cy, rx, ry, cosP, sinP, cosT2, sinT2)
		// derivative-based control points
		d1x := -rx*cosP*sinT1 - ry*sinP*cosT1
		d1y := -rx*sinP*sinT1 + ry*cosP*cosT1
		d2x := -rx*cosP*sinT2 - ry*sinP*cosT2
		d2y := -rx*sinP*sinT2 + ry*cosP*cosT2
		c1 := Point{cur.X + t*d1x, cur.Y + t*d1y}
		c2 := Point{ep.X - t*d2x, ep.Y - t*d2y}
		cmds = append(cmds, Cmd{Op: OpC, P: []Point{c1, c2, ep}})
		cur = ep
		theta = theta2
	}
	return cmds
}

func ellipsePoint(cx, cy, rx, ry, cosP, sinP, cosT, sinT float64) Point {
	x := rx * cosT
	y := ry * sinT
	return Point{
		X: cosP*x - sinP*y + cx,
		Y: sinP*x + cosP*y + cy,
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
