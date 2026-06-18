package conv

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// ParseSVG reads an SVG document and returns all geometry as subpaths in
// millimetres. scaleOverride, if > 0, forces the user-unit -> mm factor;
// otherwise it is derived from the root width/height + viewBox (falling back to
// 1 unit = 1 mm).
func ParseSVG(r io.Reader, scaleOverride float64) ([]Subpath, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	w := &svgWalker{}
	var rootSeen bool

	type frame struct {
		mat  Matrix
		name string
	}
	stack := []frame{{mat: Identity()}}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := stack[len(stack)-1]
			local := Identity()
			if tr := attr(t, "transform"); tr != "" {
				m, err := parseTransform(tr)
				if err != nil {
					return nil, err
				}
				local = m
			}
			cur := parent.mat.Mul(local)

			name := strings.ToLower(t.Name.Local)
			if name == "svg" && !rootSeen {
				rootSeen = true
				s := scaleOverride
				if s <= 0 {
					s = w.rootScale(t)
				}
				w.scale = s
				cur = Scale(s, s).Mul(cur)
			}
			if err := w.element(name, t, cur); err != nil {
				return nil, err
			}
			stack = append(stack, frame{mat: cur, name: name})
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if w.scale == 0 {
		// No <svg> root encountered with a scale; assume 1 unit = 1 mm.
		w.scale = 1
	}
	return w.subs, nil
}

type svgWalker struct {
	subs  []Subpath
	scale float64
}

// rootScale derives the user-unit -> mm factor from the root element.
func (w *svgWalker) rootScale(e xml.StartElement) float64 {
	vb := attr(e, "viewBox")
	widthAttr := attr(e, "width")
	if vb != "" && widthAttr != "" {
		fields := strings.FieldsFunc(vb, func(r rune) bool { return r == ' ' || r == ',' })
		if len(fields) == 4 {
			vbw, err1 := strconv.ParseFloat(fields[2], 64)
			mmw, hasUnit, err2 := lengthToMM(widthAttr)
			if err1 == nil && err2 == nil && hasUnit && vbw > 0 {
				return mmw / vbw
			}
		}
	}
	// width/height with explicit units but no viewBox: still 1 unit = its length?
	// Without a viewBox we cannot relate user units to the physical size, so
	// default to 1 unit = 1 mm.
	return 1
}

func (w *svgWalker) element(name string, e xml.StartElement, m Matrix) error {
	var sps []Subpath
	var err error
	switch name {
	case "path":
		d := attr(e, "d")
		if d == "" {
			return nil
		}
		sps, err = parsePathData(d)
	case "rect":
		sps = rectSubpaths(e)
	case "circle":
		sps = circleSubpaths(e)
	case "ellipse":
		sps = ellipseSubpaths(e)
	case "line":
		sps = lineSubpaths(e)
	case "polyline":
		sps = polySubpaths(e, false)
	case "polygon":
		sps = polySubpaths(e, true)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("svg <%s>: %w", name, err)
	}
	for _, sp := range sps {
		w.subs = append(w.subs, sp.transform(m))
	}
	return nil
}

func rectSubpaths(e xml.StartElement) []Subpath {
	x := fattr(e, "x", 0)
	y := fattr(e, "y", 0)
	width := fattr(e, "width", 0)
	height := fattr(e, "height", 0)
	if width <= 0 || height <= 0 {
		return nil
	}
	rx := fattr(e, "rx", -1)
	ry := fattr(e, "ry", -1)
	if rx < 0 && ry < 0 {
		rx, ry = 0, 0
	} else if rx < 0 {
		rx = ry
	} else if ry < 0 {
		ry = rx
	}
	rx = math.Min(rx, width/2)
	ry = math.Min(ry, height/2)

	var cmds []Cmd
	if rx == 0 || ry == 0 {
		cmds = []Cmd{
			{Op: OpM, P: []Point{{x, y}}},
			{Op: OpL, P: []Point{{x + width, y}}},
			{Op: OpL, P: []Point{{x + width, y + height}}},
			{Op: OpL, P: []Point{{x, y + height}}},
			{Op: OpZ},
		}
	} else {
		// Rounded rectangle: corners as quadratic-ish cubics via arcs.
		k := 0.5522847498307936 // 4/3*(sqrt(2)-1)
		cx, cy := rx*k, ry*k
		cmds = []Cmd{
			{Op: OpM, P: []Point{{x + rx, y}}},
			{Op: OpL, P: []Point{{x + width - rx, y}}},
			{Op: OpC, P: []Point{{x + width - rx + cx, y}, {x + width, y + ry - cy}, {x + width, y + ry}}},
			{Op: OpL, P: []Point{{x + width, y + height - ry}}},
			{Op: OpC, P: []Point{{x + width, y + height - ry + cy}, {x + width - rx + cx, y + height}, {x + width - rx, y + height}}},
			{Op: OpL, P: []Point{{x + rx, y + height}}},
			{Op: OpC, P: []Point{{x + rx - cx, y + height}, {x, y + height - ry + cy}, {x, y + height - ry}}},
			{Op: OpL, P: []Point{{x, y + ry}}},
			{Op: OpC, P: []Point{{x, y + ry - cy}, {x + rx - cx, y}, {x + rx, y}}},
			{Op: OpZ},
		}
	}
	return []Subpath{{Cmds: cmds, Closed: true}}
}

func ellipseSubpaths(e xml.StartElement) []Subpath {
	cx := fattr(e, "cx", 0)
	cy := fattr(e, "cy", 0)
	rx := fattr(e, "rx", 0)
	ry := fattr(e, "ry", 0)
	return ellipseAt(cx, cy, rx, ry)
}

func circleSubpaths(e xml.StartElement) []Subpath {
	cx := fattr(e, "cx", 0)
	cy := fattr(e, "cy", 0)
	r := fattr(e, "r", 0)
	return ellipseAt(cx, cy, r, r)
}

func ellipseAt(cx, cy, rx, ry float64) []Subpath {
	if rx <= 0 || ry <= 0 {
		return nil
	}
	k := 0.5522847498307936
	ox, oy := rx*k, ry*k
	cmds := []Cmd{
		{Op: OpM, P: []Point{{cx + rx, cy}}},
		{Op: OpC, P: []Point{{cx + rx, cy + oy}, {cx + ox, cy + ry}, {cx, cy + ry}}},
		{Op: OpC, P: []Point{{cx - ox, cy + ry}, {cx - rx, cy + oy}, {cx - rx, cy}}},
		{Op: OpC, P: []Point{{cx - rx, cy - oy}, {cx - ox, cy - ry}, {cx, cy - ry}}},
		{Op: OpC, P: []Point{{cx + ox, cy - ry}, {cx + rx, cy - oy}, {cx + rx, cy}}},
		{Op: OpZ},
	}
	return []Subpath{{Cmds: cmds, Closed: true}}
}

func lineSubpaths(e xml.StartElement) []Subpath {
	x1 := fattr(e, "x1", 0)
	y1 := fattr(e, "y1", 0)
	x2 := fattr(e, "x2", 0)
	y2 := fattr(e, "y2", 0)
	cmds := []Cmd{
		{Op: OpM, P: []Point{{x1, y1}}},
		{Op: OpL, P: []Point{{x2, y2}}},
	}
	return []Subpath{{Cmds: cmds}}
}

func polySubpaths(e xml.StartElement, closed bool) []Subpath {
	pts := parsePoints(attr(e, "points"))
	if len(pts) < 2 {
		return nil
	}
	cmds := []Cmd{{Op: OpM, P: []Point{pts[0]}}}
	for _, p := range pts[1:] {
		cmds = append(cmds, Cmd{Op: OpL, P: []Point{p}})
	}
	if closed {
		cmds = append(cmds, Cmd{Op: OpZ})
	}
	return []Subpath{{Cmds: cmds, Closed: closed}}
}

func parsePoints(s string) []Point {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	var nums []float64
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err == nil {
			nums = append(nums, v)
		}
	}
	var pts []Point
	for i := 0; i+1 < len(nums); i += 2 {
		pts = append(pts, Point{nums[i], nums[i+1]})
	}
	return pts
}

// parseTransform parses an SVG transform attribute into a single matrix.
func parseTransform(s string) (Matrix, error) {
	m := Identity()
	i := 0
	n := len(s)
	for i < n {
		// read function name
		for i < n && (s[i] == ' ' || s[i] == ',' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
			i++
		}
		fn := strings.ToLower(s[start:i])
		for i < n && s[i] != '(' {
			i++
		}
		if i >= n {
			break
		}
		i++ // skip '('
		argStart := i
		for i < n && s[i] != ')' {
			i++
		}
		args := parseFloatList(s[argStart:i])
		if i < n {
			i++ // skip ')'
		}
		t, err := transformFunc(fn, args)
		if err != nil {
			return m, err
		}
		m = m.Mul(t)
	}
	return m, nil
}

func transformFunc(fn string, a []float64) (Matrix, error) {
	switch fn {
	case "matrix":
		if len(a) != 6 {
			return Identity(), fmt.Errorf("transform matrix needs 6 args")
		}
		return Matrix{a[0], a[1], a[2], a[3], a[4], a[5]}, nil
	case "translate":
		if len(a) == 1 {
			return Translate(a[0], 0), nil
		}
		if len(a) >= 2 {
			return Translate(a[0], a[1]), nil
		}
	case "scale":
		if len(a) == 1 {
			return Scale(a[0], a[0]), nil
		}
		if len(a) >= 2 {
			return Scale(a[0], a[1]), nil
		}
	case "rotate":
		if len(a) == 1 {
			return RotateDeg(a[0]), nil
		}
		if len(a) >= 3 {
			return Translate(a[1], a[2]).Mul(RotateDeg(a[0])).Mul(Translate(-a[1], -a[2])), nil
		}
	case "skewx":
		if len(a) >= 1 {
			return skewXDeg(a[0]), nil
		}
	case "skewy":
		if len(a) >= 1 {
			return skewYDeg(a[0]), nil
		}
	}
	return Identity(), nil
}

func parseFloatList(s string) []float64 {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	var out []float64
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// lengthToMM parses an SVG length, returning the value in mm and whether it had
// an explicit physical unit.
func lengthToMM(s string) (float64, bool, error) {
	s = strings.TrimSpace(s)
	unit := ""
	if len(s) >= 2 {
		suffix := strings.ToLower(s[len(s)-2:])
		switch suffix {
		case "mm", "cm", "in", "pt", "pc", "px":
			unit = suffix
			s = strings.TrimSpace(s[:len(s)-2])
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, err
	}
	switch unit {
	case "mm":
		return v, true, nil
	case "cm":
		return v * 10, true, nil
	case "in":
		return v * 25.4, true, nil
	case "pt":
		return v * 25.4 / 72, true, nil
	case "pc":
		return v * 25.4 / 6, true, nil
	case "px":
		return v * 25.4 / 96, true, nil
	default:
		return v, false, nil
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

func fattr(e xml.StartElement, name string, def float64) float64 {
	s := strings.TrimSpace(attr(e, name))
	if s == "" {
		return def
	}
	// Coordinates are user units (the root scale handles unit conversion).
	// Strip any trailing unit suffix and parse the leading number.
	end := len(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E' {
			continue
		}
		end = i
		break
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return def
	}
	return v
}
