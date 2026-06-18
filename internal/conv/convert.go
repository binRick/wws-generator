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
	Margin    float64
	Spacing   float64
	Grid      float64
	Rotations []float64
	Scale     float64 // user-unit -> mm override; <=0 means auto
	CutPower  int
	CutSpeed  int
	Time      int64
}

// Summary reports what the conversion produced.
type Summary struct {
	Pieces int
	Sheets int
	Bytes  int
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

	pieces := buildPieces(subs, flattenTol)
	if len(pieces) == 0 {
		return nil, Summary{}, fmt.Errorf("no closed pieces found in SVG")
	}

	placements, sheets, err := Nest(pieces, NestOptions{
		MaterialW: opt.MaterialW,
		MaterialH: opt.MaterialH,
		Margin:    opt.Margin,
		Spacing:   opt.Spacing,
		Grid:      opt.Grid,
		Rotations: opt.Rotations,
	})
	if err != nil {
		return nil, Summary{}, err
	}

	bopt := BuildOptions{
		Name:      opt.Name,
		CutPower:  opt.CutPower,
		CutSpeed:  opt.CutSpeed,
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
	return out, Summary{Pieces: len(pieces), Sheets: sheets, Bytes: len(out)}, nil
}
