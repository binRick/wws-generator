package conv

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"rsc.io/pdf"
)

// ConvertPDF converts a PDF (or PDF-compatible AI) into a .wws file. It reads
// the first page's vector geometry via rsc.io/pdf and the in-package content
// interpreter; stroke/fill color decides cut vs engrave. Text (fonts) is not
// rasterised, so text-only annotations are dropped.
func ConvertPDF(data []byte, opt Options) ([]byte, Summary, error) {
	subs, err := ParsePDF(data)
	if err != nil {
		return nil, Summary{}, err
	}
	if len(subs) == 0 {
		return nil, Summary{}, fmt.Errorf("no vector geometry found in PDF (it may be image- or text-only)")
	}
	return convertSubpaths(subs, opt)
}

// ParsePDF returns the first page's vector geometry as subpaths in millimetres
// (PDF points -> mm, y-up -> y-down).
func ParsePDF(data []byte) (subs []Subpath, err error) {
	defer func() {
		if r := recover(); r != nil {
			subs, err = nil, fmt.Errorf("pdf: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	if r.NumPage() < 1 {
		return nil, fmt.Errorf("pdf: no pages")
	}
	page := r.Page(1)

	// Page box -> the base transform: PDF points (y-up, origin lower-left) into
	// screen millimetres (y-down).
	mb := page.V.Key("MediaBox")
	minX := mb.Index(0).Float64()
	maxY := mb.Index(3).Float64()
	const f = 25.4 / 72.0 // points -> mm
	base := Matrix{f, 0, 0, -f, -f * minX, f * maxY}

	csN := colorSpaceComponents(page.V.Key("Resources").Key("ColorSpace"))

	var content []byte
	c := page.V.Key("Contents")
	if c.Kind() == pdf.Array {
		for i := 0; i < c.Len(); i++ {
			b, _ := io.ReadAll(c.Index(i).Reader())
			content = append(content, b...)
			content = append(content, '\n')
		}
	} else {
		content, _ = io.ReadAll(c.Reader())
	}

	in := &pdfInterp{base: base, csN: csN}
	return in.run(content), nil
}

// colorSpaceComponents maps each named colorspace to its component count
// (1=gray, 3=rgb, 4=cmyk), reading /N for ICCBased.
func colorSpaceComponents(cs pdf.Value) map[string]int {
	out := map[string]int{}
	if cs.Kind() != pdf.Dict {
		return out
	}
	for _, k := range cs.Keys() {
		v := cs.Key(k)
		switch {
		case v.Kind() == pdf.Array && v.Len() > 0:
			switch v.Index(0).Name() {
			case "ICCBased":
				if n := int(v.Index(1).Key("N").Int64()); n > 0 {
					out[k] = n
				}
			case "CalRGB", "Lab":
				out[k] = 3
			case "CalGray":
				out[k] = 1
			case "DeviceN":
				out[k] = v.Index(1).Len()
			}
		case v.Kind() == pdf.Name:
			out[k] = deviceComponents(v.Name())
		}
	}
	return out
}

func deviceComponents(name string) int {
	switch name {
	case "DeviceRGB", "RGB":
		return 3
	case "DeviceCMYK", "CMYK":
		return 4
	case "DeviceGray", "G":
		return 1
	}
	return 0
}

// ---- content-stream interpreter ----

type gstate struct {
	ctm            Matrix
	fill, stroke   string // resolved hex
	fillN, strokeN int    // current colorspace component counts
}

type pdfInterp struct {
	base Matrix
	csN  map[string]int

	gs    gstate
	stack []gstate

	cur   []Subpath // path under construction (already in mm)
	sp    *Subpath  // open subpath
	pen   Point     // current point in user space (pre-CTM)
	start Point

	groupStack []int
	groupSeq   int
	elem       int
	subs       []Subpath
}

func (in *pdfInterp) run(content []byte) []Subpath {
	in.gs = gstate{ctm: in.base, fill: "#000000", stroke: "#000000", fillN: 1, strokeN: 1}
	in.groupStack = []int{-1}

	toks := tokenizePDF(content)
	var args []float64
	var name string // most recent /Name operand
	for _, t := range toks {
		switch t.kind {
		case pdfNum:
			args = append(args, t.num)
		case pdfName:
			name = t.str
		case pdfOp:
			in.op(t.str, args, name)
			args = args[:0]
			name = ""
		}
	}
	return in.subs
}

func (in *pdfInterp) group() int { return in.groupStack[len(in.groupStack)-1] }

func (in *pdfInterp) op(op string, a []float64, name string) {
	g := &in.gs
	switch op {
	case "q":
		in.stack = append(in.stack, in.gs)
	case "Q":
		if len(in.stack) > 0 {
			in.gs = in.stack[len(in.stack)-1]
			in.stack = in.stack[:len(in.stack)-1]
		}
	case "cm":
		if len(a) == 6 {
			g.ctm = g.ctm.Mul(Matrix{a[0], a[1], a[2], a[3], a[4], a[5]})
		}

	// marked content (optional-content layers) -> grouping for orphan marks
	case "BDC", "BMC":
		in.groupSeq++
		in.groupStack = append(in.groupStack, in.groupSeq)
	case "EMC":
		if len(in.groupStack) > 1 {
			in.groupStack = in.groupStack[:len(in.groupStack)-1]
		}

	// path construction
	case "m":
		if len(a) == 2 {
			in.moveTo(Point{a[0], a[1]})
		}
	case "l":
		if len(a) == 2 {
			in.lineTo(Point{a[0], a[1]})
		}
	case "c":
		if len(a) == 6 {
			in.curveTo(Point{a[0], a[1]}, Point{a[2], a[3]}, Point{a[4], a[5]})
		}
	case "v":
		if len(a) == 4 {
			in.curveTo(in.pen, Point{a[0], a[1]}, Point{a[2], a[3]})
		}
	case "y":
		if len(a) == 4 {
			in.curveTo(Point{a[0], a[1]}, Point{a[2], a[3]}, Point{a[2], a[3]})
		}
	case "re":
		if len(a) == 4 {
			in.rect(a[0], a[1], a[2], a[3])
		}
	case "h":
		in.closeSub()

	// color
	case "g":
		g.fill, g.fillN = grayHex(a), 1
	case "G":
		g.stroke, g.strokeN = grayHex(a), 1
	case "rg":
		g.fill, g.fillN = rgbHex(a), 3
	case "RG":
		g.stroke, g.strokeN = rgbHex(a), 3
	case "k":
		g.fill, g.fillN = cmykHex(a), 4
	case "K":
		g.stroke, g.strokeN = cmykHex(a), 4
	case "cs":
		g.fillN = in.csN[name]
	case "CS":
		g.strokeN = in.csN[name]
	case "sc", "scn":
		if c := compsHex(a, g.fillN); c != "" {
			g.fill = c
		}
	case "SC", "SCN":
		if c := compsHex(a, g.strokeN); c != "" {
			g.stroke = c
		}

	// path painting
	case "S", "s":
		if op == "s" {
			in.closeSub()
		}
		in.paint("", g.stroke)
	case "f", "F", "f*":
		in.paint(g.fill, "")
	case "b", "b*":
		in.closeSub()
		in.paint(g.fill, g.stroke)
	case "B", "B*":
		in.paint(g.fill, g.stroke)
	case "n":
		in.clear()
	}
}

func (in *pdfInterp) moveTo(p Point) {
	in.flushSub()
	in.pen, in.start = p, p
	in.sp = &Subpath{Cmds: []Cmd{{Op: OpM, P: []Point{in.gs.ctm.Apply(p)}}}}
}

func (in *pdfInterp) lineTo(p Point) {
	if in.sp == nil {
		in.moveTo(p)
		return
	}
	in.pen = p
	in.sp.Cmds = append(in.sp.Cmds, Cmd{Op: OpL, P: []Point{in.gs.ctm.Apply(p)}})
}

func (in *pdfInterp) curveTo(c1, c2, end Point) {
	if in.sp == nil {
		in.moveTo(c1)
	}
	m := in.gs.ctm
	in.sp.Cmds = append(in.sp.Cmds, Cmd{Op: OpC, P: []Point{m.Apply(c1), m.Apply(c2), m.Apply(end)}})
	in.pen = end
}

func (in *pdfInterp) rect(x, y, w, h float64) {
	in.moveTo(Point{x, y})
	in.lineTo(Point{x + w, y})
	in.lineTo(Point{x + w, y + h})
	in.lineTo(Point{x, y + h})
	in.closeSub()
}

func (in *pdfInterp) closeSub() {
	if in.sp != nil {
		in.sp.Closed = true
		in.sp.Cmds = append(in.sp.Cmds, Cmd{Op: OpZ})
		in.pen = in.start
	}
}

func (in *pdfInterp) flushSub() {
	if in.sp != nil && len(in.sp.Cmds) > 0 {
		in.cur = append(in.cur, *in.sp)
	}
	in.sp = nil
}

func (in *pdfInterp) clear() {
	in.sp = nil
	in.cur = nil
}

func (in *pdfInterp) paint(fill, stroke string) {
	in.flushSub()
	if len(in.cur) == 0 {
		return
	}
	role, color := classifyRole(fill, stroke)
	grp := in.group()
	// One paint op == one element: all its subpaths share an Elem so a filled
	// shape with holes stays a single object (and isn't scattered).
	e := in.elem
	in.elem++
	for _, sp := range in.cur {
		sp.Role, sp.Color, sp.Elem, sp.Group = role, color, e, grp
		in.subs = append(in.subs, sp)
	}
	in.cur = nil
}

// ---- color helpers (components are 0..1) ----

func clamp01(v float64) int {
	n := int(math.Round(v * 255))
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func grayHex(a []float64) string {
	if len(a) < 1 {
		return ""
	}
	v := clamp01(a[0])
	return fmt.Sprintf("#%02X%02X%02X", v, v, v)
}

func rgbHex(a []float64) string {
	if len(a) < 3 {
		return ""
	}
	return fmt.Sprintf("#%02X%02X%02X", clamp01(a[0]), clamp01(a[1]), clamp01(a[2]))
}

func cmykHex(a []float64) string {
	if len(a) < 4 {
		return ""
	}
	c, m, y, k := a[0], a[1], a[2], a[3]
	return fmt.Sprintf("#%02X%02X%02X", clamp01((1-c)*(1-k)), clamp01((1-m)*(1-k)), clamp01((1-y)*(1-k)))
}

// compsHex interprets n color components (drops a trailing pattern name, which
// would leave fewer numbers than n).
func compsHex(a []float64, n int) string {
	switch n {
	case 1:
		return grayHex(a)
	case 3:
		return rgbHex(a)
	case 4:
		return cmykHex(a)
	}
	switch len(a) {
	case 1:
		return grayHex(a)
	case 3:
		return rgbHex(a)
	case 4:
		return cmykHex(a)
	}
	return ""
}

// ---- minimal content-stream tokenizer ----

type pdfTokKind int

const (
	pdfNum pdfTokKind = iota
	pdfName
	pdfOp
)

type pdfTok struct {
	kind pdfTokKind
	num  float64
	str  string
}

func tokenizePDF(b []byte) []pdfTok {
	var toks []pdfTok
	i, n := 0, len(b)
	isDelim := func(c byte) bool {
		switch c {
		case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
			return true
		}
		return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0
	}
	for i < n {
		c := b[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0:
			i++
		case c == '%': // comment to end of line
			for i < n && b[i] != '\n' && b[i] != '\r' {
				i++
			}
		case c == '/': // name
			i++
			start := i
			for i < n && !isDelim(b[i]) {
				i++
			}
			toks = append(toks, pdfTok{kind: pdfName, str: string(b[start:i])})
		case c == '(': // literal string — skip balanced
			depth, esc := 1, false
			i++
			for i < n && depth > 0 {
				ch := b[i]
				if esc {
					esc = false
				} else if ch == '\\' {
					esc = true
				} else if ch == '(' {
					depth++
				} else if ch == ')' {
					depth--
				}
				i++
			}
		case c == '<':
			if i+1 < n && b[i+1] == '<' { // dict — skip balanced
				depth := 1
				i += 2
				for i < n && depth > 0 {
					if b[i] == '<' && i+1 < n && b[i+1] == '<' {
						depth++
						i += 2
					} else if b[i] == '>' && i+1 < n && b[i+1] == '>' {
						depth--
						i += 2
					} else {
						i++
					}
				}
			} else { // hex string — skip
				for i < n && b[i] != '>' {
					i++
				}
				i++
			}
		case c == '[' || c == ']' || c == '{' || c == '}':
			i++ // array/proc delimiters: ignored (operands within are numbers)
		case c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9'):
			start := i
			i++
			for i < n && (b[i] == '.' || b[i] == '-' || b[i] == '+' || b[i] == 'e' || b[i] == 'E' || (b[i] >= '0' && b[i] <= '9')) {
				i++
			}
			toks = append(toks, pdfTok{kind: pdfNum, num: atof(string(b[start:i]))})
		default: // operator keyword
			start := i
			for i < n && !isDelim(b[i]) {
				i++
			}
			word := string(b[start:i])
			if word == "BI" { // inline image: skip through EI
				j := bytes.Index(b[i:], []byte("EI"))
				if j < 0 {
					i = n
				} else {
					i += j + 2
				}
				continue
			}
			if word != "" {
				toks = append(toks, pdfTok{kind: pdfOp, str: word})
			}
		}
	}
	return toks
}
