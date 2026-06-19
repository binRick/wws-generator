// Command svg2wws converts an SVG into a WeCreat MakeIt! .wws file, nesting the
// SVG's pieces onto one or more material sheets (each sheet becomes a canvas).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/binRick/wws-generator/internal/conv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "svg2wws: "+err.Error())
		os.Exit(1)
	}
}

type flags struct {
	in           string
	out          string
	material     string
	spacing      float64
	margin       float64
	grid         float64
	rotations    string
	scale        float64
	power        int
	speed        int
	passes       int
	name         string
	engraveAlign bool
	groupEngrave bool
}

func run(args []string) error {
	f := flags{
		spacing:      3,
		margin:       10,
		grid:         1.0,
		rotations:    "8",
		power:        0,
		speed:        5,
		passes:       1,
		engraveAlign: true,
		groupEngrave: true,
	}
	fs := newFlagSet(&f)
	if err := fs.parse(args); err != nil {
		return err
	}
	if f.in == "" {
		return fmt.Errorf("missing --in <file.svg> (try --help)")
	}
	if f.material == "" {
		return fmt.Errorf("missing --material WxH in mm, e.g. --material 300x200 (try --help)")
	}

	mw, mh, err := parseMaterial(f.material)
	if err != nil {
		return err
	}
	rots, err := parseRotations(f.rotations)
	if err != nil {
		return err
	}

	out := f.out
	if out == "" {
		base := strings.TrimSuffix(filepath.Base(f.in), filepath.Ext(f.in))
		out = base + ".wws"
	}
	name := f.name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(out), filepath.Ext(out))
	}

	opt := conv.Options{
		Name:         name,
		MaterialW:    mw,
		MaterialH:    mh,
		Spacing:      f.spacing,
		Margin:       f.margin,
		Grid:         f.grid,
		Rotations:    rots,
		Scale:        f.scale,
		CutPower:     f.power,
		CutSpeed:     f.speed,
		CutPasses:    f.passes,
		EngraveAlign: f.engraveAlign,
		GroupEngrave: f.groupEngrave,
		Time:         time.Now().UnixMilli(),
	}

	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.in), "."))
	var data []byte
	var sum conv.Summary
	switch format {
	case "", "svg":
		raw, err := os.ReadFile(f.in)
		if err != nil {
			return err
		}
		data, sum, err = conv.Convert(bytes.NewReader(raw), opt)
		if err != nil {
			return err
		}
	case "pdf", "ai":
		raw, err := os.ReadFile(f.in)
		if err != nil {
			return err
		}
		data, sum, err = conv.ConvertPDF(raw, opt)
		if err != nil {
			return err
		}
	case "dxf":
		raw, err := os.ReadFile(f.in)
		if err != nil {
			return err
		}
		data, sum, err = conv.ConvertDXF(raw, opt)
		if err != nil {
			return err
		}
	case "png", "jpg", "jpeg", "gif", "bmp":
		raw, err := os.ReadFile(f.in)
		if err != nil {
			return err
		}
		data, sum, err = conv.ConvertRaster(raw, format, opt)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported input format %q (supported: svg, pdf, ai, dxf, png, jpg)", format)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}

	fmt.Printf("Wrote %s\n", out)
	fmt.Printf("  %d piece(s) nested onto %d sheet(s) of %.0fx%.0f mm (%d bytes)\n",
		sum.Pieces, sum.Sheets, mw, mh, sum.Bytes)
	align := "engrave-align on"
	if !f.engraveAlign {
		align = "engrave-align off"
	}
	fmt.Printf("  %.1f mm margin, %.1f mm between items, grid %.2f mm, %d rotations, %s; layout anchored top-left\n",
		f.margin, f.spacing, f.grid, len(rots), align)
	if sum.EngraveSheets > 0 && sum.EngraveSheets < sum.Sheets {
		fmt.Printf("  engraving consolidated onto the last %d of %d sheet(s); the rest are cut-only\n",
			sum.EngraveSheets, sum.Sheets)
	}
	fmt.Printf("  Open in MakeIt! to verify dimensions and cut (reposition on the bed as needed).\n")
	return nil
}

func parseMaterial(s string) (float64, float64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	sep := strings.IndexAny(s, "x*")
	if sep < 0 {
		return 0, 0, fmt.Errorf("--material must be WxH, e.g. 300x200")
	}
	w, err1 := strconv.ParseFloat(strings.TrimSpace(s[:sep]), 64)
	h, err2 := strconv.ParseFloat(strings.TrimSpace(s[sep+1:]), 64)
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("--material must be positive WxH, e.g. 300x200")
	}
	return w, h, nil
}

// parseRotations accepts either an integer count (N evenly-spaced angles over
// 0..360) or a comma-separated list of degrees.
func parseRotations(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []float64{0}, nil
	}
	if !strings.Contains(s, ",") {
		if n, err := strconv.Atoi(s); err == nil {
			if n < 1 {
				return nil, fmt.Errorf("--rotations count must be >= 1")
			}
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				out[i] = 360 * float64(i) / float64(n)
			}
			return out, nil
		}
	}
	var out []float64
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("--rotations: bad angle %q", part)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []float64{0}, nil
	}
	return out, nil
}

// ---- minimal flag handling with a helpful usage message ----

type flagSet struct{ f *flags }

func newFlagSet(f *flags) *flagSet { return &flagSet{f: f} }

func (fs *flagSet) parse(args []string) error {
	f := fs.f
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-h" || a == "--help" {
			printUsage()
			os.Exit(0)
		}
		key, val, hasEq := strings.Cut(a, "=")
		next := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", key)
			}
			i++
			return args[i], nil
		}
		var err error
		switch key {
		case "--in":
			f.in, err = next()
		case "--out":
			f.out, err = next()
		case "--material":
			f.material, err = next()
		case "--spacing":
			err = setFloat(next, &f.spacing)
		case "--margin":
			err = setFloat(next, &f.margin)
		case "--grid":
			err = setFloat(next, &f.grid)
		case "--rotations":
			f.rotations, err = next()
		case "--scale":
			err = setFloat(next, &f.scale)
		case "--power":
			err = setInt(next, &f.power)
		case "--speed":
			err = setInt(next, &f.speed)
		case "--passes":
			err = setInt(next, &f.passes)
		case "--name":
			f.name, err = next()
		case "--engrave-align":
			f.engraveAlign = true
		case "--no-engrave-align":
			f.engraveAlign = false
		case "--group-engrave":
			f.groupEngrave = true
		case "--no-group-engrave":
			f.groupEngrave = false
		default:
			return fmt.Errorf("unknown flag %q (try --help)", a)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func setFloat(next func() (string, error), dst *float64) error {
	s, err := next()
	if err != nil {
		return err
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %q", s)
	}
	*dst = v
	return nil
}

func setInt(next func() (string, error), dst *int) error {
	s, err := next()
	if err != nil {
		return err
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("expected an integer, got %q", s)
	}
	*dst = v
	return nil
}

func printUsage() {
	fmt.Print(`svg2wws — convert a design (SVG/PDF/AI/DXF/raster) into a WeCreat MakeIt! .wws file

Usage:
  svg2wws --in design.svg --material 300x200 [options]

Required:
  --in FILE          input design: .svg, .pdf, .ai, .dxf, or a raster
                     .png/.jpg/.gif (embedded as fill-engrave). All parsed
                     natively — no external tools.
  --material WxH     sheet size in mm (e.g. 300x200)

Options:
  --out FILE         output .wws (default: <input>.wws)
  --name NAME        project name shown in MakeIt! (default: output base name)
  --spacing MM       gap between pieces (default 3)
  --margin MM        border between the layout and the material edge, for leeway
                     against the sheet edge (default 10)
  --grid MM          nesting resolution; smaller = tighter but slower (default 1.0)
  --rotations N|list rotation candidates for cut pieces: a count for N evenly-
                     spaced angles, or a comma list of degrees (default 8)
  --no-engrave-align keep engrave pieces in their drawn orientation. By default,
                     a piece carrying engraving may rotate to lay its engraving
                     flat (horizontal), so the laser raster-engraves it faster.
  --no-group-engrave allow engrave pieces to share sheets with cut-only pieces.
                     By default, engrave-bearing pieces are consolidated onto
                     their own sheet(s) so the cut-only sheets carry no engraving.
  --scale F          force user-unit -> mm factor (default: auto; 1 unit = 1 mm)
  --power N          cut power 0-100 (default 0; set per material in MakeIt!)
  --speed N          cut speed (default 5)
  --passes N         cut passes / repeat (default 1)

Pieces are nested with true polygon footprints; parts that don't fit spill onto
additional sheets, each emitted as its own canvas. The layout is anchored at the
canvas top-left — reposition it on the bed in MakeIt!. A piece larger than one
sheet is an error.
`)
}
