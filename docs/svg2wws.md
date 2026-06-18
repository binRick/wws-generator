# `svg2wws` — SVG → `.wws` converter

Turns an SVG into a WeCreat MakeIt! `.wws` file, **nesting** the SVG's separate
pieces onto one or more material sheets. Each sheet becomes its own MakeIt!
canvas, so a job that's too big for one piece of wood spills onto the next.

Single self-contained Go binary (standard library only):

```bash
go build -o svg2wws ./cmd/svg2wws
./svg2wws --in design.svg --material 300x200
```

## Pipeline

1. **Parse SVG** (`svgdoc.go`, `svgpath.go`). Walks the XML, accumulating
   `transform` matrices down the `<g>` tree. Handles `<path>`, `<rect>` (incl.
   `rx`/`ry`), `<circle>`, `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`.
   Every path command is reduced to **absolute `M`/`L`/`C`/`Q`/`Z`** — relative
   commands, `H`/`V`, smooth `S`/`T`, and elliptical arcs `A` are all converted
   (arcs → cubic Béziers). Coordinates come out in **millimetres**.

2. **Units → mm** (`--scale`, else auto). With an explicit physical
   `width`/`height` **and** a `viewBox`, the factor is `width_mm / viewBox_w`.
   Otherwise **1 user unit = 1 mm** (matches MakeIt!'s observed import). Per-axis
   non-uniform `viewBox` scaling is not applied (uniform factor only).

3. **Detect pieces** (`piece.go`). Subpaths are grouped by **even/odd
   containment**: a loop nested inside an even number of others is an *outer*
   boundary that starts a new piece; an odd-depth loop is a *hole* of its
   immediate parent. So a ring (outer circle + inner circle) is one piece with a
   hole, and a small shape drawn inside a hole becomes its own piece.

4. **Nest** (`nest.go`, `raster.go`). The sheet size comes from `--material`;
   `--spacing` (default 3 mm) is the single "space around items" value — it's both
   the gap between pieces and the border inset, so the usable nesting area is the
   sheet shrunk by `--spacing` on every side and the layout is **anchored at the
   canvas top-left**. Each piece is **rasterised at its true filled footprint**
   (even-odd fill, so holes are empty space), then placed with a top-left
   first-fit heuristic, largest piece first. Pieces are tried at several
   **rotations** and the lowest-resting orientation wins. The footprint is dilated
   by `--spacing` for collision tests, so placed parts keep their gap; occupancy
   is marked with the undilated footprint. Because nesting uses the real polygon
   (not the bounding box), concave parts **interlock** and a small part can drop
   into a larger part's hole.

5. **Emit `.wws`** (`wws.go`, `cover.go`). One canvas per sheet. Each piece's
   exact geometry is transformed into absolute-mm Fabric `path` command arrays
   with `scaleX = scaleY = 1`, `angle = 0`, and `left`/`top` = the path's exact
   bounding-box min (including Bézier extrema, to match Fabric's own bbox math).
   Cut stroke is `#E61F19`, `processMode: "cut"`. `processList` is keyed by each
   object id; `layerDataList` gets one entry per canvas. A PNG thumbnail of sheet
   1 is rendered for the `cover`. Target format `version: "3.0.4"`.

## Why raster nesting

True analytic No-Fit-Polygon nesting with free rotation is fragile to implement
(degenerate geometry, robust Minkowski sums). Rasterising the footprint gives the
same *behaviour* — real-shape interlocking, free rotation (just rotate the mask),
hole-filling — with far fewer failure modes. Placement is quantised to the
`--grid` cell (default 1 mm); the **emitted cut geometry stays exact vector** —
only the chosen position/rotation is grid-aligned.

Trade-offs you can tune:
- `--grid 0.5` packs tighter (¼-cell positions) but is slower.
- `--rotations` is a count (N evenly-spaced angles) or an explicit degree list.
  `1` disables rotation; `4` restricts to 90° steps (good for grain direction);
  higher values let parts interlock at finer angles.

## Behaviour notes

- **Position is top-left only.** The tool does not place the material on the bed
  — it anchors the cut layout at the canvas top-left with `--spacing` of border.
  Move it to wherever you want on the bed inside MakeIt!.
- **Oversized parts error out.** If a single piece can't fit on an empty sheet
  (after the `--spacing` border), conversion fails with the piece size vs sheet
  size — enlarge `--material` or reduce `--spacing`. (Splitting one part across
  sheets with joinery is intentionally out of scope.)
- **Spacing is a floor, not exact.** Square dilation on the grid guarantees at
  least `--spacing` of clearance (often a hair more at coarse grids).
- **Each closed outline is an independent part.** If a single physical part is
  drawn as several disconnected strokes, they'll be nested separately. Group such
  geometry into one closed outline (with holes) if it must stay together.

## Current limits / TODO

- `<use>`, `<defs>` instantiation, `<text>`, and embedded `<image>` are ignored
  (only the listed vector shapes are read).
- Only cut output (red stroke). No engrave / fill-engrave layer mapping yet.
- No kerf compensation (cut lines are placed on the SVG geometry exactly).
- Nesting is a greedy heuristic, not globally optimal; very dense jobs may leave
  some waste a genetic-algorithm nester would recover.
- **Not yet verified on hardware.** Outputs pass structural validation and a
  no-overlap/spacing raster check, but a converter file has not been confirmed to
  open in MakeIt! — open one and check dimensions before cutting.
