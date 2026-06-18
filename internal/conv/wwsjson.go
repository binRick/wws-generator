package conv

import (
	"encoding/json"
	"math"
	"strings"
)

// This file decodes a .wws into a fully-typed, renderable JSON model. The intent
// is that a web front-end can draw the design from this JSON alone: every object
// carries its local geometry plus a transform matrix to canvas millimetres, its
// style, and its decoded laser settings.

// Project is the root of the JSON model.
type Project struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	ProjectID string   `json:"projectId,omitempty"`
	Units     string   `json:"units"` // always "mm"
	Canvases  []Canvas `json:"canvases"`
}

// Canvas is one work area; a design may have several.
type Canvas struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Index    int       `json:"index"`
	Material *Material `json:"material,omitempty"`
	BBox     *Box      `json:"bbox,omitempty"` // content bounds in mm
	Items    []Item    `json:"items"`
}

// Material is the per-canvas stock settings, when the file records them.
type Material struct {
	ThicknessMM       *float64 `json:"thicknessMm,omitempty"`
	VariableThickness bool     `json:"variableThickness,omitempty"`
}

// Box is an axis-aligned rectangle in millimetres.
type Box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Item is one drawable object.
type Item struct {
	ID string `json:"id,omitempty"`
	// Type is the SVG-equivalent element kind: path, rect, circle, ellipse,
	// line, polygon, polyline, image, text.
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	// Transform maps the local Geometry coordinates to canvas millimetres as an
	// SVG matrix [a b c d e f]: x' = a*x + c*y + e, y' = b*x + d*y + f.
	Transform [6]float64     `json:"transform"`
	Geometry  map[string]any `json:"geometry"`
	BBox      *Box           `json:"bbox,omitempty"` // canvas-space AABB in mm
	Style     Style          `json:"style"`
	Laser     *Laser         `json:"laser,omitempty"`
	// GroupPath lists ancestor group ids, outermost first (empty if top-level).
	GroupPath []string `json:"groupPath,omitempty"`
}

// Style mirrors the object's visual attributes.
type Style struct {
	Stroke      string  `json:"stroke,omitempty"`
	Fill        string  `json:"fill,omitempty"`
	StrokeWidth float64 `json:"strokeWidth"`
	Opacity     float64 `json:"opacity"`
	Visible     bool    `json:"visible"`
}

// Laser is the decoded cut/engrave configuration for an object.
type Laser struct {
	Operation   string   `json:"operation,omitempty"` // cut | engrave | fillEngrave
	Ignored     bool     `json:"ignored"`
	Cut         *LaserOp `json:"cut,omitempty"`
	Engrave     *LaserOp `json:"engrave,omitempty"`
	FillEngrave *LaserOp `json:"fillEngrave,omitempty"`
	LineDensity *float64 `json:"lineDensity,omitempty"`
	DPI         *float64 `json:"dpi,omitempty"`
}

// LaserOp is one operation's power/speed/passes.
type LaserOp struct {
	Power  float64 `json:"power"`  // 0-100 %
	Speed  float64 `json:"speed"`  // machine units
	Passes float64 `json:"passes"` // "repeat" count
}

// DescribeOptions controls JSON generation.
type DescribeOptions struct {
	StripImages bool // replace embedded image data with a placeholder
}

// Describe decodes a .wws file into the renderable JSON model.
func Describe(data []byte, opt DescribeOptions) (*Project, error) {
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	p := &Project{
		Name:      jstr(top, "name", ""),
		Version:   jstr(top, "version", ""),
		ProjectID: jstr(top, "projectId", ""),
		Units:     "mm",
	}
	pl := asMap(top["processList"])
	mats := materialsByCanvas(top)

	for i, cv := range jarr(top, "canvasList") {
		c := asMap(cv)
		canvas := Canvas{
			ID:       jstr(c, "id", ""),
			Name:     jstr(c, "name", ""),
			Index:    i,
			Material: mats[jstr(c, "id", "")],
		}
		bb := emptyRect()
		walkItems(jarr(c, "objects"), Identity(), nil, pl, opt, &canvas.Items, &bb)
		if !math.IsInf(bb.MinX, 1) {
			canvas.BBox = &Box{X: r3(bb.MinX), Y: r3(bb.MinY), W: r3(bb.W()), H: r3(bb.H())}
		}
		if canvas.Items == nil {
			canvas.Items = []Item{}
		}
		p.Canvases = append(p.Canvases, canvas)
	}
	return p, nil
}

// MarshalJSON serialises the project (pretty unless compact).
func (p *Project) ToJSON(compact bool) ([]byte, error) {
	if compact {
		return json.Marshal(p)
	}
	return json.MarshalIndent(p, "", "  ")
}

func walkItems(objs []any, parent Matrix, groupPath []string, pl map[string]any, opt DescribeOptions, out *[]Item, bb *Rect) {
	for _, ov := range objs {
		o := asMap(ov)
		if o == nil {
			continue
		}
		t := strings.ToLower(jstr(o, "type", ""))
		full := parent.Mul(ownMatrix(o))
		if t == "group" {
			gp := append(append([]string{}, groupPath...), jstr(o, "id", "group"))
			walkItems(jarr(o, "objects"), full, gp, pl, opt, out, bb)
			continue
		}
		geom, pts, ok := geometryOf(o, opt)
		if !ok {
			continue
		}
		em := leafTransform(o, full)
		ib := emptyRect()
		for _, pt := range pts {
			tp := em.Apply(pt)
			bb.add(tp)
			ib.add(tp)
		}
		item := Item{
			ID:        jstr(o, "id", ""),
			Type:      t,
			Name:      jstr(o, "name", ""),
			Transform: [6]float64{r6(em[0]), r6(em[1]), r6(em[2]), r6(em[3]), r6(em[4]), r6(em[5])},
			Geometry:  geom,
			Style: Style{
				Stroke:      jstr(o, "stroke", ""),
				Fill:        jstr(o, "fill", ""),
				StrokeWidth: jnum(o, "strokeWidth", 0),
				Opacity:     jnum(o, "opacity", 1),
				Visible:     jbool(o, "visible", true),
			},
			Laser:     laserOf(pl, o),
			GroupPath: append([]string{}, groupPath...),
		}
		if !math.IsInf(ib.MinX, 1) {
			item.BBox = &Box{X: r3(ib.MinX), Y: r3(ib.MinY), W: r3(ib.W()), H: r3(ib.H())}
		}
		if len(item.GroupPath) == 0 {
			item.GroupPath = nil
		}
		*out = append(*out, item)
	}
}

// geometryOf returns the local geometry map, the local points used to compute
// the canvas-space bounding box, and whether the object is drawable.
func geometryOf(o map[string]any, opt DescribeOptions) (map[string]any, []Point, bool) {
	switch strings.ToLower(jstr(o, "type", "")) {
	case "rect":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		g := map[string]any{"x": 0, "y": 0, "width": r3(w), "height": r3(h)}
		if rx, ry := jnum(o, "rx", 0), jnum(o, "ry", 0); rx > 0 || ry > 0 {
			g["rx"], g["ry"] = r3(rx), r3(ry)
		}
		return g, corners(0, 0, w, h), true
	case "circle":
		r := jnum(o, "radius", jnum(o, "width", 0)/2)
		return map[string]any{"cx": 0, "cy": 0, "r": r3(r)}, corners(-r, -r, r, r), true
	case "ellipse":
		rx := jnum(o, "rx", jnum(o, "width", 0)/2)
		ry := jnum(o, "ry", jnum(o, "height", 0)/2)
		return map[string]any{"cx": 0, "cy": 0, "rx": r3(rx), "ry": r3(ry)}, corners(-rx, -ry, rx, ry), true
	case "line":
		x1, y1, x2, y2 := lineLocalPoints(o)
		return map[string]any{"x1": r3(x1), "y1": r3(y1), "x2": r3(x2), "y2": r3(y2)},
			[]Point{{x1, y1}, {x2, y2}}, true
	case "polygon", "polyline":
		pts := parsePointObjs(o["points"])
		if len(pts) == 0 {
			return nil, nil, false
		}
		arr := make([][2]float64, len(pts))
		for i, p := range pts {
			arr[i] = [2]float64{r3(p.X), r3(p.Y)}
		}
		return map[string]any{"points": arr}, pts, true
	case "path":
		sps := parseWWSPath(o["path"])
		if len(sps) == 0 {
			return nil, nil, false
		}
		var pts []Point
		for _, sp := range sps {
			pts = append(pts, sp.Flatten(0.25)...)
		}
		return map[string]any{"d": subpathsToD(sps)}, pts, true
	case "image":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		src := jstr(o, "src", "")
		if src == "" {
			return nil, nil, false
		}
		g := map[string]any{"width": r3(w), "height": r3(h)}
		if opt.StripImages {
			g["href"] = nil
			g["stripped"] = true
		} else {
			g["href"] = src
		}
		return g, corners(0, 0, w, h), true
	case "i-text", "text", "textbox":
		w, h := jnum(o, "width", 0), jnum(o, "height", 0)
		g := map[string]any{
			"text":       jstr(o, "text", ""),
			"fontSize":   jnum(o, "fontSize", 16),
			"fontFamily": jstr(o, "fontFamily", "sans-serif"),
		}
		return g, corners(0, 0, w, h), true
	}
	return nil, nil, false
}

func laserOf(pl map[string]any, o map[string]any) *Laser {
	id := jstr(o, "id", "")
	var p map[string]any
	if id != "" {
		p = asMap(pl[id])
	}
	op := jstr(o, "processMode", "")
	if p != nil {
		if m := jstr(p, "processMode", ""); m != "" {
			op = m
		}
	}
	ignored := jbool(o, "isIgnoreWork", false) || (p != nil && jbool(p, "isIgnoreWork", false))
	if p == nil && op == "" && !ignored {
		return nil
	}
	l := &Laser{Operation: op, Ignored: ignored}
	if p != nil {
		l.Cut = opOf(p, "cut")
		l.Engrave = opOf(p, "engrave")
		l.FillEngrave = opOf(p, "fillEngrave")
		if v, ok := p["lineDesity"].(float64); ok {
			l.LineDensity = &v
		}
		if v, ok := p["dpi"].(float64); ok {
			l.DPI = &v
		}
	}
	return l
}

func opOf(p map[string]any, key string) *LaserOp {
	m := asMap(p[key])
	if m == nil {
		return nil
	}
	return &LaserOp{Power: jnum(m, "power", 0), Speed: jnum(m, "speed", 0), Passes: jnum(m, "repeat", 1)}
}

func materialsByCanvas(top map[string]any) map[string]*Material {
	out := map[string]*Material{}
	for _, e := range jarr(top, "canvasParamsListArray") {
		m := asMap(e)
		key := jstr(m, "key", "")
		if key == "" {
			continue
		}
		mat := &Material{}
		has := false
		if v, ok := m["materThickness"].(float64); ok {
			mat.ThicknessMM = &v
			has = true
		}
		if jbool(m, "isVariableThickness", false) {
			mat.VariableThickness = true
			has = true
		}
		if has {
			out[key] = mat
		}
	}
	return out
}

func corners(x0, y0, x1, y1 float64) []Point {
	return []Point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

func r3(v float64) float64 { return roundp(v, 1000) }
func r6(v float64) float64 { return roundp(v, 1e6) }
func roundp(v, scale float64) float64 {
	r := math.Round(v*scale) / scale
	if r == 0 {
		return 0
	}
	return r
}
