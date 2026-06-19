// Command wws2svg converts WeCreat MakeIt! .wws files into SVG — one SVG per
// canvas. It accepts a single file or a directory (batch mode).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/binRick/wws-generator/internal/conv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wws2svg: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	var in, out string
	skipEmpty := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			printUsage()
			return nil
		case a == "--all-canvases":
			skipEmpty = false
		case strings.HasPrefix(a, "--in"):
			var err error
			in, err = val(a, args, &i)
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--out"):
			var err error
			out, err = val(a, args, &i)
			if err != nil {
				return err
			}
		case in == "" && !strings.HasPrefix(a, "-"):
			in = a // allow positional input
		default:
			return fmt.Errorf("unknown argument %q (try --help)", a)
		}
	}
	if in == "" {
		return fmt.Errorf("missing --in <file.wws|dir> (try --help)")
	}

	files, err := collectInputs(in)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .wws files found at %s", in)
	}

	totalSVG, totalCanvas, failed := 0, 0, 0
	for _, f := range files {
		n, c, err := convertFile(f, out, skipEmpty)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", filepath.Base(f), err)
			failed++
			continue
		}
		totalSVG += n
		totalCanvas += c
	}

	fmt.Printf("Converted %d file(s): %d canvas(es) -> %d SVG file(s)",
		len(files)-failed, totalCanvas, totalSVG)
	if failed > 0 {
		fmt.Printf(" (%d file(s) failed)", failed)
	}
	fmt.Println()
	if failed > 0 {
		return fmt.Errorf("%d of %d file(s) failed to convert", failed, len(files))
	}
	return nil
}

func convertFile(path, outDir string, skipEmpty bool) (svgWritten, canvases int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	out, err := conv.WWSToSVGs(data)
	if err != nil {
		return 0, 0, err
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dir := outDir
	if dir == "" {
		dir = filepath.Dir(path)
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, 0, err
	}

	single := len(out) == 1
	for _, cs := range out {
		canvases++
		if cs.Empty && skipEmpty {
			continue
		}
		name := base + ".svg"
		if !single {
			name = fmt.Sprintf("%s-canvas%02d.svg", base, cs.Index+1)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(cs.SVG), 0o644); err != nil {
			return svgWritten, canvases, err
		}
		svgWritten++
	}
	return svgWritten, canvases, nil
}

func collectInputs(in string) ([]string, error) {
	info, err := os.Stat(in)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{in}, nil
	}
	entries, err := os.ReadDir(in)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".wws") {
			files = append(files, filepath.Join(in, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func val(a string, args []string, i *int) (string, error) {
	if k, v, ok := strings.Cut(a, "="); ok {
		_ = k
		return v, nil
	}
	if *i+1 >= len(args) {
		return "", fmt.Errorf("flag %s needs a value", a)
	}
	*i++
	return args[*i], nil
}

func printUsage() {
	fmt.Print(`wws2svg — convert WeCreat MakeIt! .wws files to SVG (one SVG per canvas)

Usage:
  wws2svg --in design.wws [--out DIR]
  wws2svg --in /path/to/wws-folder --out /path/to/svgs   # batch a directory

Options:
  --in FILE|DIR    a .wws file, or a directory of .wws files (required)
  --out DIR        output directory (default: next to each input file)
  --all-canvases   also write SVGs for canvases that have no geometry
  -h, --help       this help

Output naming:
  single-canvas file  -> <name>.svg
  multi-canvas file   -> <name>-canvas01.svg, <name>-canvas02.svg, ...

Coordinates are in millimetres (1 SVG unit = 1 mm). Object stroke/fill colours are
preserved; hairline (strokeWidth 0) lines are given a thin visible width.
`)
}
