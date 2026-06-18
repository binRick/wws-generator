# `wws2svg` — WeCreat `.wws` → SVG converter

The reverse of `svg2wws`: reads a MakeIt! `.wws` file and writes **one SVG per
canvas**, with geometry in millimetres and stroke/fill colours preserved. Useful
for editing purchased designs in a vector tool, extracting parts, or feeding
geometry back into `svg2wws`.

Single self-contained Go binary (standard library only):

```bash
go build -o wws2svg ./cmd/wws2svg

# one file
./wws2svg --in design.wws --out ./svgs

# batch a whole folder of .wws files
./wws2svg --in /Users/you/Desktop/cups/WWS --out ./svgs
```

## Options

| Flag | Default | Meaning |
| --- | --- | --- |
| `--in FILE\|DIR` | — (required) | a `.wws` file, or a directory of them (batch) |
| `--out DIR` | next to each input | output directory |
| `--all-canvases` | off | also emit SVGs for canvases with no geometry |
| `-h`, `--help` | | usage |

**Output naming:** a single-canvas file becomes `<name>.svg`; a multi-canvas file
becomes `<name>-canvas01.svg`, `<name>-canvas02.svg`, … (most `.wws` files have
several canvases).

## How it works

A `.wws` is a Fabric.js scene. Each object carries `left/top`, `scaleX/scaleY`,
`angle`, `flipX/flipY`, `skewX/skewY`, and an `originX/originY`. `wws2svg`
replicates Fabric's **`calcOwnMatrix`** to build, for every object, the affine
matrix from its centred content frame to the canvas, and composes those matrices
through nested **groups**. Each object is emitted as the matching SVG element
(`path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `image`,
`text`) carrying a `transform="matrix(...)"`, so the original element type and
exact geometry are preserved.

- **Units:** millimetres (1 SVG user unit = 1 mm); `width`/`height` are mm and the
  `viewBox` is the content bounds.
- **Colours:** each object's `stroke`/`fill` is kept (so cut vs engrave colours
  survive). Hairline strokes (`strokeWidth` 0) are given a thin 0.1 mm width so
  they're visible in editors.
- **Images:** the embedded base64 `src` is written straight into an `<image>`.
- **Groups:** flattened transform-wise but each child stays a separate element.

## Validation

- Across the reference library (339 files, 1604 canvases), all **1553** non-empty
  canvases produce **well-formed XML**, exercising every element type.
- For files without rotation/flip/skew/groups, the SVG content bounds match
  Fabric's reported object AABBs to **< 0.03 mm** (curve-flattening noise).
- Rotation-about-origin, flip, and group composition are covered by analytic unit
  tests (`conv_test.go`).

## Limits

- **Text is approximate.** `<text>` is emitted with the stored string, font family
  and size, but glyph metrics/layout are not reproduced exactly (no font
  embedding). Convert text to paths in MakeIt! first if you need exact shapes.
- **Geometry-faithful, not round-trip-exact.** MakeIt!-specific data
  (`processList` power/speed, layer grouping, sequence) is **not** carried into
  the SVG — it captures the drawing, not the laser job.
- No `clipPath`/gradient/filter handling (not used by these files); `skew`'s
  effect on the AABB is ignored (rare). Re-import via `svg2wws` re-nests and emits
  cut paths, so `wws → svg → wws` is a geometry round-trip, not a byte round-trip.
