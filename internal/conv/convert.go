package conv

import (
	"bytes"
	"fmt"
	"io"
)

// Options are the full set of conversion settings.
type Options struct {
	Name      string
	MaterialW float64
	MaterialH float64
	Spacing   float64 // gap between pieces
	Margin    float64 // border between the layout and the material edge (<=0 -> Spacing)
	Grid      float64

	// EngraveAlign lets engrave-bearing pieces rotate to lay their engraving in a
	// short horizontal band (faster raster engraving). Cut-only pieces are
	// unaffected. Has no effect on SVGs without engrave content.
	EngraveAlign bool

	// GroupEngrave consolidates all engrave-bearing pieces onto their own
	// sheet(s), separate from cut-only pieces. No effect without engrave content.
	GroupEngrave bool
	Rotations    []float64
	Scale        float64 // user-unit -> mm override; <=0 means auto
	CutPower     int
	CutSpeed     int
	CutPasses    int
	Time         int64
}

// Summary reports what the conversion produced.
type Summary struct {
	Pieces        int
	Sheets        int
	EngraveSheets int // how many sheets carry engraving
	Bytes         int
}

// flattenTol is the curve-flattening tolerance (mm) used for nesting and the
// cover. Fine enough that the polygon footprint closely matches the true curve.
const flattenTol = 0.1

// Convert reads an SVG and returns the bytes of a .wws file plus a summary.
func Convert(r io.Reader, opt Options) ([]byte, Summary, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, Summary{}, err
	}

	subs, err := ParseSVG(bytes.NewReader(data), opt.Scale)
	if err != nil {
		return nil, Summary{}, err
	}
	if len(subs) == 0 {
		return nil, Summary{}, fmt.Errorf("no drawable geometry found in SVG")
	}
	return convertSubpaths(subs, opt)
}

// convertSubpaths runs the shared pipeline (role split → pieces → nest → emit)
// for geometry from any front-end (SVG, DXF, …).
func convertSubpaths(subs []Subpath, opt Options) ([]byte, Summary, error) {
	// Split geometry by role: cut paths form pieces; engrave/fillEngrave shapes
	// become marks (grouped per source element so glyph holes survive).
	var cutSubs []Subpath
	marksByElem := map[int]*Mark{}
	var markOrder []int
	for _, sp := range subs {
		if sp.Role == RoleCut {
			cutSubs = append(cutSubs, sp)
			continue
		}
		mk, ok := marksByElem[sp.Elem]
		if !ok {
			mk = &Mark{Role: sp.Role, Color: sp.Color, Group: sp.Group}
			marksByElem[sp.Elem] = mk
			markOrder = append(markOrder, sp.Elem)
		}
		mk.Subpaths = append(mk.Subpaths, sp)
	}

	pieces := buildPieces(cutSubs, flattenTol)
	pieces = attachMarks(pieces, marksByElem, markOrder, flattenTol)
	if len(pieces) == 0 {
		return nil, Summary{}, fmt.Errorf("no closed pieces found")
	}

	margin := opt.Margin
	if margin <= 0 {
		margin = opt.Spacing
	}
	placements, sheets, err := Nest(pieces, NestOptions{
		MaterialW:    opt.MaterialW,
		MaterialH:    opt.MaterialH,
		Spacing:      opt.Spacing,
		Margin:       margin,
		Grid:         opt.Grid,
		Rotations:    opt.Rotations,
		EngraveAlign: opt.EngraveAlign,
		GroupEngrave: opt.GroupEngrave,
	})
	if err != nil {
		return nil, Summary{}, err
	}

	engraveSheet := map[int]bool{}
	for _, pl := range placements {
		if len(pl.Piece.Marks) > 0 {
			engraveSheet[pl.Sheet] = true
		}
	}

	bopt := BuildOptions{
		Name:      opt.Name,
		CutPower:  opt.CutPower,
		CutSpeed:  opt.CutSpeed,
		CutPasses: opt.CutPasses,
		MaterialW: opt.MaterialW,
		MaterialH: opt.MaterialH,
		Time:      opt.Time,
	}
	f, err := Build(placements, sheets, bopt)
	if err != nil {
		return nil, Summary{}, err
	}
	f.Cover = coverDataURI(placements, bopt, flattenTol)

	out, err := f.Marshal()
	if err != nil {
		return nil, Summary{}, err
	}
	return out, Summary{Pieces: len(pieces), Sheets: sheets, EngraveSheets: len(engraveSheet), Bytes: len(out)}, nil
}
