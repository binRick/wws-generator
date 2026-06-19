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

	w := &svgWalker{css: map[string]map[string]string{}, defs: map[string][]Subpath{}}
	var rootSeen bool

	type frame struct {
		mat    Matrix
		defMat Matrix // matrix relative to the current def root (for <defs>/<use>)
		name   string
		group  int
		fill   string // inherited fill
		stroke string // inherited stroke
		inDefs bool   // inside a <defs>/<symbol> (content collected, not drawn)
		defID  string // id of the def root being collected ("" if none)
	}
	stack := []frame{{mat: Identity(), defMat: Identity(), group: -1}}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1].name == "style" {
				w.styleBuf = append(w.styleBuf, t...)
			}
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
			group := parent.group
			if name == "g" {
				group = w.groupSeq
				w.groupSeq++
			}
			fill, stroke := w.resolveStyle(t, parent.fill, parent.stroke)

			// Track <defs>/<symbol> so referenced geometry is collected, not drawn.
			inDefs := parent.inDefs || name == "defs" || name == "symbol"
			defID := parent.defID
			defMat := parent.defMat.Mul(local)
			if defID == "" && inDefs {
				if id := attr(t, "id"); id != "" {
					defID = id
					defMat = local
				}
			}

			if err := w.element(name, t, cur, defMat, group, fill, stroke, inDefs, defID); err != nil {
				return nil, err
			}
			stack = append(stack, frame{
				mat: cur, defMat: defMat, name: name, group: group,
				fill: fill, stroke: stroke, inDefs: inDefs, defID: defID,
			})
		case xml.EndElement:
			if len(stack) > 1 {
				if stack[len(stack)-1].name == "style" {
					w.parseCSS(string(w.styleBuf))
					w.styleBuf = nil
				}
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
	subs     []Subpath
	scale    float64
	css      map[string]map[string]string // class name -> {fill,stroke,...}
	defs     map[string][]Subpath         // id -> raw geometry (def-local coords), for <use>
	styleBuf []byte                       // accumulates <style> text content
	elem     int                          // running source-element index
	groupSeq int                          // running <g> index
}

// parseCSS reads a stylesheet body and records fill/stroke for each `.class`.
func (w *svgWalker) parseCSS(s string) {
	// Strip /* comments */.
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "*/")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+j+2:]
	}
	for len(s) > 0 {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			break
		}
		sel := strings.TrimSpace(s[:open])
		closeAt := strings.IndexByte(s, '}')
		if closeAt < 0 {
			break
		}
		props := parseDecls(s[open+1 : closeAt])
		for _, one := range strings.Split(sel, ",") {
			one = strings.TrimSpace(one)
			if cls := strings.TrimPrefix(one, "."); strings.HasPrefix(one, ".") && cls != "" {
				if w.css[cls] == nil {
					w.css[cls] = map[string]string{}
				}
				for k, v := range props {
					w.css[cls][k] = v
				}
			}
		}
		s = s[closeAt+1:]
	}
}

// resolveStyle computes the effective fill/stroke for an element, starting from
// the inherited values and overriding with presentation attributes, then CSS
// classes, then an inline style.
func (w *svgWalker) resolveStyle(e xml.StartElement, pFill, pStroke string) (fill, stroke string) {
	fill, stroke = pFill, pStroke
	if v := attr(e, "fill"); v != "" {
		fill = v
	}
	if v := attr(e, "stroke"); v != "" {
		stroke = v
	}
	for _, cls := range strings.Fields(attr(e, "class")) {
		if props, ok := w.css[cls]; ok {
			if v, ok := props["fill"]; ok {
				fill = v
			}
			if v, ok := props["stroke"]; ok {
				stroke = v
			}
		}
	}
	if st := attr(e, "style"); st != "" {
		props := parseDecls(st)
		if v, ok := props["fill"]; ok {
			fill = v
		}
		if v, ok := props["stroke"]; ok {
			stroke = v
		}
	}
	return
}

func parseDecls(s string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}

// classifyRole maps an element's fill/stroke to a laser operation + hex color.
func classifyRole(fill, stroke string) (Role, string) {
	hasFill := fill != "" && !strings.EqualFold(fill, "none")
	hasStroke := stroke != "" && !strings.EqualFold(stroke, "none")
	switch {
	case hasStroke && isCutColor(stroke):
		return RoleCut, cutRed
	case hasFill:
		return RoleFillEngrave, normColor(fill)
	case hasStroke:
		return RoleEngrave, normColor(stroke)
	default:
		return RoleCut, cutRed
	}
}

var namedColors = map[string]string{
	"black": "#000000", "white": "#FFFFFF", "red": "#FF0000",
	"green": "#008000", "blue": "#0000FF", "yellow": "#FFFF00",
	"gray": "#808080", "grey": "#808080",
}

// normColor canonicalises a CSS color to "#RRGGBB" where possible.
func normColor(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	if h, ok := namedColors[strings.ToLower(c)]; ok {
		return h
	}
	if strings.HasPrefix(c, "#") {
		hex := strings.ToUpper(c[1:])
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) == 6 {
			return "#" + hex
		}
	}
	if h, ok := parseRGB(c); ok {
		return h
	}
	return c
}

// parseRGB handles CSS rgb()/rgba() with integer (0-255) or percentage channels,
// as emitted by pdftocairo (PDF→SVG), e.g. "rgb(100%, 0%, 0%)".
func parseRGB(c string) (string, bool) {
	lc := strings.ToLower(strings.TrimSpace(c))
	if !strings.HasPrefix(lc, "rgb(") && !strings.HasPrefix(lc, "rgba(") {
		return "", false
	}
	open := strings.IndexByte(lc, '(')
	inner := strings.TrimSuffix(strings.TrimSpace(lc[open+1:]), ")")
	parts := strings.Split(inner, ",")
	if len(parts) < 3 {
		return "", false
	}
	var ch [3]int
	for i := 0; i < 3; i++ {
		p := strings.TrimSpace(parts[i])
		if strings.HasSuffix(p, "%") {
			f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(p, "%")), 64)
			if err != nil {
				return "", false
			}
			ch[i] = int(math.Round(f / 100 * 255))
		} else {
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return "", false
			}
			ch[i] = int(math.Round(f))
		}
		if ch[i] < 0 {
			ch[i] = 0
		}
		if ch[i] > 255 {
			ch[i] = 255
		}
	}
	return fmt.Sprintf("#%02X%02X%02X", ch[0], ch[1], ch[2]), true
}

// isCutColor reports whether a stroke color denotes a cut (red).
func isCutColor(c string) bool {
	switch normColor(c) {
	case "#E61F19", "#FF0000":
		return true
	}
	return false
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

func (w *svgWalker) element(name string, e xml.StartElement, m, defMat Matrix, group int, fill, stroke string, inDefs bool, defID string) error {
	// <use> instantiates a previously-defined element at (x,y) under the current
	// transform — this is how PDF→SVG (pdftocairo) renders text glyphs.
	if name == "use" && !inDefs {
		href := strings.TrimSpace(attr(e, "href"))
		href = strings.TrimPrefix(href, "#")
		raw := w.defs[href]
		if len(raw) == 0 {
			return nil
		}
		um := m.Mul(Translate(fattr(e, "x", 0), fattr(e, "y", 0)))
		role, color := classifyRole(fill, stroke)
		elem := w.elem
		w.elem++
		for _, sp := range raw {
			t := sp.transform(um)
			t.Role, t.Color, t.Elem, t.Group = role, color, elem, group
			w.subs = append(w.subs, t)
		}
		return nil
	}

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
	if len(sps) == 0 {
		return nil
	}

	// Geometry inside <defs>/<symbol> is stored (in def-local coords) for later
	// <use>, never drawn directly.
	if inDefs {
		if defID != "" {
			for _, sp := range sps {
				w.defs[defID] = append(w.defs[defID], sp.transform(defMat))
			}
		}
		return nil
	}

	role, color := classifyRole(fill, stroke)
	elem := w.elem
	w.elem++
	for _, sp := range sps {
		sp.Role = role
		sp.Color = color
		sp.Elem = elem
		sp.Group = group
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
