package conv

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CanvasSVG is one canvas rendered to SVG.
type CanvasSVG struct {
	Index    int    // 0-based canvas index
	Name     string // canvas name from the file
	SVG      string // full SVG document
	Objects  int    // number of geometry elements emitted
	WidthMM  float64
	HeightMM float64
	Empty    bool // no drawable geometry
}

// ProjectName returns the project name from a .wws file (for output naming).
func ProjectName(data []byte) string {
	var top map[string]any
	if json.Unmarshal(data, &top) == nil {
		if s := jstr(top, "name", ""); s != "" {
			return s
		}
	}
	return ""
}

// WWSToSVGs converts a .wws file into one SVG per canvas. Coordinates are in
// millimetres; each object is emitted with an SVG transform replicating Fabric's
// own object-to-canvas matrix (origin, scale, rotation, flip, skew, and nested
// group transforms).
func WWSToSVGs(data []byte) ([]CanvasSVG, error) {
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse .wws: %w", err)
	}
	canvases := jarr(top, "canvasList")
	if len(canvases) == 0 {
		return nil, fmt.Errorf("no canvasList in file")
	}

	out := make([]CanvasSVG, 0, len(canvases))
	for i, cv := range canvases {
		c := asMap(cv)
		var body strings.Builder
		bb := emptyRect()
		n := emitChildren(jarr(c, "objects"), Identity(), &body, &bb)

		cs := CanvasSVG{Index: i, Name: jstr(c, "name", fmt.Sprintf("canvas%02d", i+1)), Objects: n}
		if n == 0 || math.IsInf(bb.MinX, 1) {
			cs.Empty = true
			cs.SVG = emptySVG()
			out = append(out, cs)
			continue
		}
		// Pad the viewBox slightly so hairline strokes aren't clipped.
		const pad = 1.0
		minX, minY := bb.MinX-pad, bb.MinY-pad
		w, h := bb.W()+2*pad, bb.H()+2*pad
		cs.WidthMM, cs.HeightMM = w, h

		var doc strings.Builder
		doc.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
		fmt.Fprintf(&doc,
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" `+
				`width="%smm" height="%smm" viewBox="%s %s %s %s">`+"\n",
			fmtNum(w), fmtNum(h), fmtNum(minX), fmtNum(minY), fmtNum(w), fmtNum(h))
		doc.WriteString(body.String())
		doc.WriteString("</svg>\n")
		cs.SVG = doc.String()
		out = append(out, cs)
	}
	return out, nil
}

func emptySVG() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<svg xmlns="http://www.w3.org/2000/svg" width="1mm" height="1mm" viewBox="0 0 1 1"></svg>` + "\n"
}

// emitChildren renders a list of objects under the given parent matrix
// (object-content-frame -> canvas) and returns the count of leaf elements.
func emitChildren(objs []any, parent Matrix, b *strings.Builder, bb *Rect) int {
	count := 0
	for _, ov := range objs {
		count += emitObject(asMap(ov), parent, b, bb)
	}
	return count
}

func emitObject(o map[string]any, parent Matrix, b *strings.Builder, bb *Rect) int {
	if o == nil || !jbool(o, "visible", true) {
		return 0
	}
	t := strings.ToLower(jstr(o, "type", ""))
	own := ownMatrix(o)
	full := parent.Mul(own) // content-center frame -> canvas

	switch t {
	case "group":
		return emitChildren(jarr(o, "objects"), full, b, bb)

	case "rect":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		em := full.Mul(Translate(-w/2, -h/2))
		rx, ry := jnum(o, "rx", 0), jnum(o, "ry", 0)
		extra := ""
		if rx > 0 || ry > 0 {
			extra = fmt.Sprintf(` rx="%s" ry="%s"`, fmtNum(rx), fmtNum(ry))
		}
		fmt.Fprintf(b, `  <rect x="0" y="0" width="%s" height="%s"%s %s %s/>`+"\n",
			fmtNum(w), fmtNum(h), extra, styleAttrs(o, false), matrixAttr(em))
		addCorners(bb, em, w, h)
		return 1

	case "circle":
		r := jnum(o, "radius", jnum(o, "width", 0)/2)
		fmt.Fprintf(b, `  <circle cx="0" cy="0" r="%s" %s %s/>`+"\n",
			fmtNum(r), styleAttrs(o, false), matrixAttr(full))
		addBox(bb, full, -r, -r, r, r)
		return 1

	case "ellipse":
		rx := jnum(o, "rx", jnum(o, "width", 0)/2)
		ry := jnum(o, "ry", jnum(o, "height", 0)/2)
		fmt.Fprintf(b, `  <ellipse cx="0" cy="0" rx="%s" ry="%s" %s %s/>`+"\n",
			fmtNum(rx), fmtNum(ry), styleAttrs(o, false), matrixAttr(full))
		addBox(bb, full, -rx, -ry, rx, ry)
		return 1

	case "line":
		x1, y1, x2, y2 := lineLocalPoints(o)
		fmt.Fprintf(b, `  <line x1="%s" y1="%s" x2="%s" y2="%s" %s %s/>`+"\n",
			fmtNum(x1), fmtNum(y1), fmtNum(x2), fmtNum(y2), styleAttrs(o, false), matrixAttr(full))
		bb.add(full.Apply(Point{x1, y1}))
		bb.add(full.Apply(Point{x2, y2}))
		return 1

	case "polygon", "polyline":
		pts := parsePointObjs(o["points"])
		if len(pts) == 0 {
			return 0
		}
		ox, oy := centerOf(pts)
		em := full.Mul(Translate(-ox, -oy))
		var ps strings.Builder
		for i, p := range pts {
			if i > 0 {
				ps.WriteByte(' ')
			}
			fmt.Fprintf(&ps, "%s,%s", fmtNum(p.X), fmtNum(p.Y))
			bb.add(em.Apply(p))
		}
		fmt.Fprintf(b, `  <%s points="%s" %s %s/>`+"\n",
			t, ps.String(), styleAttrs(o, false), matrixAttr(em))
		return 1

	case "path":
		sps := parseWWSPath(o["path"])
		if len(sps) == 0 {
			return 0
		}
		r := subpathsBBox(sps)
		ox, oy := r.MinX+r.W()/2, r.MinY+r.H()/2
		em := full.Mul(Translate(-ox, -oy))
		fmt.Fprintf(b, `  <path d="%s" %s %s/>`+"\n",
			subpathsToD(sps), styleAttrs(o, false), matrixAttr(em))
		for _, sp := range sps {
			for _, p := range sp.Flatten(0.25) {
				bb.add(em.Apply(p))
			}
		}
		return 1

	case "image":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		src := jstr(o, "src", "")
		if src == "" {
			return 0
		}
		em := full.Mul(Translate(-w/2, -h/2))
		fmt.Fprintf(b, `  <image x="0" y="0" width="%s" height="%s" xlink:href="%s" %s/>`+"\n",
			fmtNum(w), fmtNum(h), xmlEscape(src), matrixAttr(em))
		addCorners(bb, em, w, h)
		return 1

	case "i-text", "text", "textbox":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		txt := jstr(o, "text", "")
		fs := jnum(o, "fontSize", 16)
		fam := jstr(o, "fontFamily", "sans-serif")
		fill := jstr(o, "fill", "#000000")
		if fill == "" {
			fill = "#000000"
		}
		em := full.Mul(Translate(-w/2, -h/2))
		fmt.Fprintf(b, `  <text x="0" y="%s" font-family="%s" font-size="%s" fill="%s" %s>%s</text>`+"\n",
			fmtNum(fs), xmlEscape(fam), fmtNum(fs), fill, matrixAttr(em), xmlEscape(txt))
		addCorners(bb, em, w, h)
		return 1
	}
	return 0
}

// ownMatrix replicates Fabric's calcOwnMatrix: maps the object's centred content
// frame to its parent's coordinate plane.
func ownMatrix(o map[string]any) Matrix {
	left, top := jnum(o, "left", 0), jnum(o, "top", 0)
	sx, sy := jnum(o, "scaleX", 1), jnum(o, "scaleY", 1)
	angle := jnum(o, "angle", 0)
	skewX, skewY := jnum(o, "skewX", 0), jnum(o, "skewY", 0)
	flipX, flipY := jbool(o, "flipX", false), jbool(o, "flipY", false)
	w, h := jnum(o, "width", 0), jnum(o, "height", 0)
	originX := strings.ToLower(jstr(o, "originX", "left"))
	originY := strings.ToLower(jstr(o, "originY", "top"))

	// Transformed (scaled) dimensions; skew/stroke effects on the AABB are
	// ignored here (rare in laser files and negligible for placement).
	dimX, dimY := w*math.Abs(sx), h*math.Abs(sy)

	var cx, cy float64
	switch originX {
	case "center":
		cx = left
	case "right":
		cx = left - dimX/2
	default:
		cx = left + dimX/2
	}
	switch originY {
	case "center":
		cy = top
	case "bottom":
		cy = top - dimY/2
	default:
		cy = top + dimY/2
	}
	// Rotation is about the origin point (left,top), so rotate the centre too.
	if angle != 0 {
		p := Translate(left, top).Mul(RotateDeg(angle)).Mul(Translate(-left, -top)).Apply(Point{cx, cy})
		cx, cy = p.X, p.Y
	}
	return Translate(cx, cy).Mul(RotateDeg(angle)).Mul(scaleSkewMatrix(sx, sy, skewX, skewY, flipX, flipY))
}

func scaleSkewMatrix(sx, sy, skewX, skewY float64, flipX, flipY bool) Matrix {
	fx, fy := 1.0, 1.0
	if flipX {
		fx = -1
	}
	if flipY {
		fy = -1
	}
	m := Scale(sx*fx, sy*fy)
	if skewX != 0 {
		m = m.Mul(Matrix{1, 0, math.Tan(skewX * math.Pi / 180), 1, 0, 0})
	}
	if skewY != 0 {
		m = m.Mul(Matrix{1, math.Tan(skewY * math.Pi / 180), 0, 1, 0, 0})
	}
	return m
}

// lineLocalPoints reproduces Fabric Line.calcLinePoints (endpoints relative to
// the object centre).
func lineLocalPoints(o map[string]any) (x1, y1, x2, y2 float64) {
	ox1, oy1 := jnum(o, "x1", 0), jnum(o, "y1", 0)
	ox2, oy2 := jnum(o, "x2", 0), jnum(o, "y2", 0)
	w, h := jnum(o, "width", 0), jnum(o, "height", 0)
	xm, ym := -1.0, -1.0
	if ox1 > ox2 {
		xm = 1
	}
	if oy1 > oy2 {
		ym = 1
	}
	return xm * w / 2, ym * h / 2, xm * w * -0.5, ym * h * -0.5
}

func parseWWSPath(v any) []Subpath {
	if s, ok := v.(string); ok {
		sps, _ := parsePathData(s)
		return sps
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var subs []Subpath
	var cur *Subpath
	flush := func() {
		if cur != nil && len(cur.Cmds) > 0 {
			subs = append(subs, *cur)
		}
		cur = nil
	}
	emit := func(c Cmd) {
		if cur == nil {
			cur = &Subpath{}
		}
		cur.Cmds = append(cur.Cmds, c)
	}
	for _, cv := range arr {
		ca, _ := cv.([]any)
		if len(ca) == 0 {
			continue
		}
		op, _ := ca[0].(string)
		var n []float64
		for _, x := range ca[1:] {
			if f, ok := x.(float64); ok {
				n = append(n, f)
			}
		}
		switch strings.ToUpper(op) {
		case "M":
			if len(n) >= 2 {
				flush()
				emit(Cmd{Op: OpM, P: []Point{{n[0], n[1]}}})
			}
		case "L":
			if len(n) >= 2 {
				emit(Cmd{Op: OpL, P: []Point{{n[0], n[1]}}})
			}
		case "C":
			if len(n) >= 6 {
				emit(Cmd{Op: OpC, P: []Point{{n[0], n[1]}, {n[2], n[3]}, {n[4], n[5]}}})
			}
		case "Q":
			if len(n) >= 4 {
				emit(Cmd{Op: OpQ, P: []Point{{n[0], n[1]}, {n[2], n[3]}}})
			}
		case "Z":
			if cur != nil {
				cur.Closed = true
				emit(Cmd{Op: OpZ})
			}
		}
	}
	flush()
	return subs
}

func subpathsToD(sps []Subpath) string {
	var b strings.Builder
	for _, sp := range sps {
		for _, c := range sp.Cmds {
			switch c.Op {
			case OpM:
				fmt.Fprintf(&b, "M%s %s ", fmtNum(c.P[0].X), fmtNum(c.P[0].Y))
			case OpL:
				fmt.Fprintf(&b, "L%s %s ", fmtNum(c.P[0].X), fmtNum(c.P[0].Y))
			case OpQ:
				fmt.Fprintf(&b, "Q%s %s %s %s ", fmtNum(c.P[0].X), fmtNum(c.P[0].Y), fmtNum(c.P[1].X), fmtNum(c.P[1].Y))
			case OpC:
				fmt.Fprintf(&b, "C%s %s %s %s %s %s ",
					fmtNum(c.P[0].X), fmtNum(c.P[0].Y), fmtNum(c.P[1].X), fmtNum(c.P[1].Y), fmtNum(c.P[2].X), fmtNum(c.P[2].Y))
			case OpZ:
				b.WriteString("Z ")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func parsePointObjs(v any) []Point {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	pts := make([]Point, 0, len(arr))
	for _, e := range arr {
		m := asMap(e)
		if m == nil {
			continue
		}
		pts = append(pts, Point{jnum(m, "x", 0), jnum(m, "y", 0)})
	}
	return pts
}

func centerOf(pts []Point) (float64, float64) {
	r := polylineBBox(pts)
	return r.MinX + r.W()/2, r.MinY + r.H()/2
}

func styleAttrs(o map[string]any, isText bool) string {
	if isText {
		f := jstr(o, "fill", "#000000")
		if f == "" {
			f = "#000000"
		}
		return fmt.Sprintf(`fill="%s"`, f)
	}
	fill := jstr(o, "fill", "")
	stroke := jstr(o, "stroke", "")
	sw := jnum(o, "strokeWidth", 0)
	if fill == "" {
		fill = "none"
	}
	if stroke == "" {
		stroke = "#000000"
	}
	if sw <= 0 {
		sw = 0.1 // hairline strokes serialise as 0; give them a visible width
	}
	return fmt.Sprintf(`fill="%s" stroke="%s" stroke-width="%s"`, fill, stroke, fmtNum(sw))
}

func matrixAttr(m Matrix) string {
	return fmt.Sprintf(`transform="matrix(%s %s %s %s %s %s)"`,
		fmtNum(m[0]), fmtNum(m[1]), fmtNum(m[2]), fmtNum(m[3]), fmtNum(m[4]), fmtNum(m[5]))
}

func addCorners(bb *Rect, m Matrix, w, h float64) { addBox(bb, m, 0, 0, w, h) }

func addBox(bb *Rect, m Matrix, x0, y0, x1, y1 float64) {
	bb.add(m.Apply(Point{x0, y0}))
	bb.add(m.Apply(Point{x1, y0}))
	bb.add(m.Apply(Point{x1, y1}))
	bb.add(m.Apply(Point{x0, y1}))
}

func fmtNum(v float64) string {
	v = math.Round(v*1000) / 1000
	if v == 0 {
		v = 0 // normalise -0
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// ---- generic JSON map accessors ----

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func jnum(m map[string]any, k string, def float64) float64 {
	if m == nil {
		return def
	}
	if f, ok := m[k].(float64); ok {
		return f
	}
	return def
}

func jstr(m map[string]any, k, def string) string {
	if m == nil {
		return def
	}
	if s, ok := m[k].(string); ok {
		return s
	}
	return def
}

func jbool(m map[string]any, k string, def bool) bool {
	if m == nil {
		return def
	}
	if b, ok := m[k].(bool); ok {
		return b
	}
	return def
}

func jarr(m map[string]any, k string) []any {
	if m == nil {
		return nil
	}
	a, _ := m[k].([]any)
	return a
}
