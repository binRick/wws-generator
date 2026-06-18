# Using `svg2wws` from another repo (agent integration guide)

This guide is written for an AI coding agent (e.g. Claude Code) working in a
**different** repository that wants to convert SVGs into WeCreat MakeIt! `.wws`
laser files. It documents the tool as an external CLI you shell out to.

`svg2wws` reads an SVG and writes a `.wws` file, **nesting** the SVG's separate
closed shapes onto one or more material sheets (each sheet → one MakeIt! canvas).

## Where it lives

| | |
| --- | --- |
| Repo | `/Users/richardblundell/repos/wws-generator` |
| Go module | `github.com/rapidvps/wws-generator` |
| CLI source | `<repo>/cmd/svg2wws` |
| Built binary (gitignored) | `/Users/richardblundell/repos/wws-generator/svg2wws` |
| Format spec | `<repo>/docs/wws-format.md` |
| Converter internals | `<repo>/docs/svg2wws.md` |

> **You cannot import this as a Go package** from another module: the logic is in
> `internal/conv`, which Go restricts to this module. Integrate by **invoking the
> binary** (below). If you genuinely need a library API, the package must first be
> moved out of `internal/` in this repo.

## Setup (build once)

The binary may not exist yet (it's gitignored). Check, and build if missing —
this works from any current directory and needs only the Go toolchain:

```bash
BIN=/Users/richardblundell/repos/wws-generator/svg2wws
[ -x "$BIN" ] || go build -C /Users/richardblundell/repos/wws-generator -o "$BIN" ./cmd/svg2wws
```

Requires Go (built/tested with go1.26, `darwin/arm64`). No third-party deps.

## Invocation contract

```
svg2wws --in <FILE.svg> --material <WxH> [options]
```

- **Always pass absolute paths** for `--in` and `--out`. Relative paths resolve
  against the caller's working directory.
- `--material` is millimetres, `WIDTHxHEIGHT` (also accepts `*`), e.g. `300x200`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--in FILE` | — (required) | input SVG |
| `--material WxH` | — (required) | sheet size in mm |
| `--out FILE` | `<input>.wws` (cwd) | output `.wws` |
| `--name NAME` | output base name | project name shown in MakeIt! |
| `--margin MM` | `5` | clearance from sheet edge |
| `--spacing MM` | `3` | minimum gap between pieces |
| `--grid MM` | `1.0` | nesting resolution; smaller = tighter but slower |
| `--rotations N\|list` | `8` | count of evenly-spaced angles, or a comma list of degrees. `1` = no rotation, `4` = 90° steps |
| `--scale F` | auto | force user-unit → mm factor (default 1 unit = 1 mm) |
| `--power N` | `0` | cut power 0–100 (usually left 0; set per material in MakeIt!) |
| `--speed N` | `5` | cut speed |
| `--help` | | print usage and exit 0 |

### Output / exit codes

- **Success → exit `0`.** Writes the `.wws` file and prints a summary to
  **stdout**:
  ```
  Wrote /abs/out.wws
    5 piece(s) nested onto 2 sheet(s) of 120x120 mm (12294 bytes)
    spacing 3.0 mm, margin 5.0 mm, grid 1.00 mm, 8 rotations
    Open in MakeIt! to verify dimensions and cut.
  ```
  Parse `(\d+) piece.* onto (\d+) sheet` if you need the counts; otherwise just
  check exit code and that the output file exists.
- **Failure → exit `1`**, single line on **stderr** prefixed `svg2wws: `.

## Input contract (what the SVG should be)

- **Each closed outline becomes one independently-placed piece.** A loop fully
  inside another is treated as a **hole** of it (e.g. a ring = outer + inner
  circle = one piece). A shape drawn inside a hole becomes its own piece.
- Supported elements: `<path>`, `<rect>` (incl. `rx`/`ry`), `<circle>`,
  `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`, inside any nesting of `<g
  transform=...>`. All path commands (relative, `H/V`, `S/T`, arcs `A`) are
  handled.
- **Ignored:** `<use>`, `<defs>` instantiation, `<text>`, embedded `<image>`,
  styling/stroke colors. Only vector geometry is read; everything is emitted as a
  **cut** (red `#E61F19`).
- **Units:** with an explicit physical `width`/`height` **and** a `viewBox`, the
  scale is `width_mm / viewBox_width`. Otherwise **1 SVG user unit = 1 mm**.
  Override with `--scale` (mm per user unit) if a converted part comes out the
  wrong size.

## Guarantees & limits

- Placed pieces **never overlap** and keep **at least `--spacing`** mm apart
  (slightly more at coarse `--grid`). Concave parts interlock; small parts may
  nest inside larger parts' holes. Emitted cut geometry is **exact vector** — only
  the placement position/rotation is grid-quantised.
- A single piece **larger than one sheet is a hard error** (exit 1). React by
  raising `--material`, or lowering `--margin`/`--spacing`.
- Nesting is a greedy heuristic (not globally optimal). Higher `--rotations` and
  smaller `--grid` pack tighter at the cost of runtime.
- **Not yet hardware-verified:** outputs pass structural + no-overlap validation,
  but no converter file has been confirmed to open in MakeIt! yet. Tell the user
  to open the result and check dimensions before cutting.

## Recipes

```bash
BIN=/Users/richardblundell/repos/wws-generator/svg2wws

# Basic: nest onto 300x200 mm, default 3 mm spacing
"$BIN" --in /abs/design.svg --material 300x200 --out /abs/design.wws

# Small sheets → multiple canvases automatically
"$BIN" --in /abs/parts.svg --material 120x120 --out /abs/parts.wws

# Respect wood grain (axis-aligned only) / disable rotation entirely
"$BIN" --in /abs/parts.svg --material 300x200 --rotations 4   # 90° steps
"$BIN" --in /abs/parts.svg --material 300x200 --rotations 1   # no rotation

# Tighter packing (slower)
"$BIN" --in /abs/parts.svg --material 300x200 --grid 0.5 --rotations 16

# If a part imports at the wrong size, set mm-per-unit explicitly
"$BIN" --in /abs/parts.svg --material 300x200 --scale 0.2646   # treat units as 96-dpi px

# Batch-convert a directory of SVGs
for f in /abs/svgs/*.svg; do
  "$BIN" --in "$f" --material 300x200 --out "/abs/out/$(basename "${f%.svg}").wws" || echo "FAILED: $f"
done
```

## Paste-in pointer for the consumer repo's `CLAUDE.md`

```md
## Converting SVG → WeCreat `.wws` (laser cut files)

Use the `svg2wws` tool at `/Users/richardblundell/repos/wws-generator`. It nests an
SVG's shapes onto material sheets and writes a MakeIt! `.wws`. Full agent guide:
`/Users/richardblundell/repos/wws-generator/docs/svg2wws-agent.md`.

Quick use (build once if the binary is missing, then call with absolute paths):
    BIN=/Users/richardblundell/repos/wws-generator/svg2wws
    [ -x "$BIN" ] || go build -C /Users/richardblundell/repos/wws-generator -o "$BIN" ./cmd/svg2wws
    "$BIN" --in /abs/design.svg --material 300x200 --out /abs/design.wws

Each closed SVG outline = one nested piece (holes stay attached); pieces that don't
fit spill onto extra canvases. Exit 0 = success (summary on stdout); exit 1 = error
(message on stderr) — a piece larger than the sheet is the common failure, fix by
raising --material. Default spacing 3 mm, margin 5 mm. Not yet hardware-verified —
have the user open the result in MakeIt! to confirm size before cutting.
```
