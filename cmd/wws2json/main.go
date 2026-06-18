// Command wws2json converts a WeCreat MakeIt! .wws file into a detailed,
// renderable JSON model (geometry + transforms + style + decoded laser
// settings) — intended to drive a web renderer.
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
		fmt.Fprintln(os.Stderr, "wws2json: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	var in, out string
	var compact, stripImages bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			printUsage()
			return nil
		case a == "--compact":
			compact = true
		case a == "--strip-images":
			stripImages = true
		case strings.HasPrefix(a, "--in"):
			v, err := val(a, args, &i)
			if err != nil {
				return err
			}
			in = v
		case strings.HasPrefix(a, "--out"):
			v, err := val(a, args, &i)
			if err != nil {
				return err
			}
			out = v
		case in == "" && !strings.HasPrefix(a, "-"):
			in = a
		default:
			return fmt.Errorf("unknown argument %q (try --help)", a)
		}
	}
	if in == "" {
		return fmt.Errorf("missing --in <file.wws|dir> (try --help)")
	}

	files, isDir, err := collectInputs(in)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .wws files found at %s", in)
	}
	opt := conv.DescribeOptions{StripImages: stripImages}

	// Single file input: --out is a file path (default: stream to stdout).
	if !isDir && len(files) == 1 {
		b, err := convert(files[0], compact, opt)
		if err != nil {
			return err
		}
		if out == "" {
			os.Stdout.Write(b)
			fmt.Println()
			return nil
		}
		if d := filepath.Dir(out); d != "" {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return err
		}
		fmt.Printf("Wrote %s\n", out)
		return nil
	}

	// Directory input: --out is a directory (default: next to each input).
	written, failed := 0, 0
	for _, f := range files {
		b, err := convert(f, compact, opt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", filepath.Base(f), err)
			failed++
			continue
		}
		dir := out
		if dir == "" {
			dir = filepath.Dir(f)
		} else if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		if err := os.WriteFile(filepath.Join(dir, base+".json"), b, 0o644); err != nil {
			return err
		}
		written++
	}
	fmt.Printf("Wrote %d JSON file(s)", written)
	if failed > 0 {
		fmt.Printf(" (%d failed)", failed)
	}
	fmt.Println()
	return nil
}

func convert(path string, compact bool, opt conv.DescribeOptions) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	proj, err := conv.Describe(data, opt)
	if err != nil {
		return nil, err
	}
	return proj.ToJSON(compact)
}

func collectInputs(in string) ([]string, bool, error) {
	info, err := os.Stat(in)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return []string{in}, false, nil
	}
	entries, err := os.ReadDir(in)
	if err != nil {
		return nil, true, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".wws") {
			files = append(files, filepath.Join(in, e.Name()))
		}
	}
	sort.Strings(files)
	return files, true, nil
}

func val(a string, args []string, i *int) (string, error) {
	if _, v, ok := strings.Cut(a, "="); ok {
		return v, nil
	}
	if *i+1 >= len(args) {
		return "", fmt.Errorf("flag %s needs a value", a)
	}
	*i++
	return args[*i], nil
}

func printUsage() {
	fmt.Print(`wws2json — convert a WeCreat MakeIt! .wws into detailed, renderable JSON

Usage:
  wws2json --in design.wws                 # JSON to stdout
  wws2json --in design.wws --out design.json
  wws2json --in /path/to/wws-folder --out /path/to/json   # batch a directory

Options:
  --in FILE|DIR    a .wws file, or a directory of them (required)
  --out FILE|DIR   output file (single) or directory (batch); default: stdout for
                   a single file, else next to each input
  --strip-images   replace embedded image data with a placeholder (smaller JSON)
  --compact        minified JSON (default: pretty-printed)
  -h, --help       this help

The JSON gives, per canvas, every object's local geometry, a transform matrix to
canvas millimetres, its style, and decoded laser settings (operation, power,
speed, passes, ignored) — enough for a web front-end to render the design.
`)
}
