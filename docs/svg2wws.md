# `svg2wws` — SVG → `.wws` converter

Turns an SVG into a WeCreat MakeIt! `.wws` file, **nesting** the SVG's separate
pieces onto one or more material sheets. Each sheet becomes its own MakeIt!
canvas, so a job that's too big for one piece of wood spills onto the next.

Single self-contained Go binary (standard library only):

```bash
go build -o svg2wws ./cmd/svg2wws
./svg2wws --in design.svg --material 300x200
```

## Input formats

Auto-detected by extension:

- **`.svg`** — parsed directly (the rich path; preserves the cut/engrave/label
  layer structure best).
- **`.pdf` / `.ai`** — parsed **natively** (`pdf.go`), no external tools. The
  page is read with the vendored `rsc.io/pdf` (object/xref/stream/FlateDecode
  plumbing) and an in-package content-stream interpreter walks the path operators
  (`m`/`l`/`c`/`re`, `cm`, `q`/`Q`), resolving color via device ops and
  `cs`/`scn` over the page's ICCBased/Device colorspaces — red stroke → cut,
  fills → engrave. PDF-compatible `.ai` works the same way. **Caveat:** text is
  font-based and not rasterised, so text-only annotations (e.g. the comment
  label) are dropped; the cut + vector engraving come through (cut geometry is
  identical to the sibling SVG). `Do` form/image XObjects aren't recursed yet.
- **`.dxf`** — native parser (`dxf.go`): LINE, LWPOLYLINE/POLYLINE (incl. bulge
  arcs), CIRCLE, ARC, ELLIPSE, and SPLINE (rational NURBS via de Boor). Reads
  `$INSUNITS` for the unit scale, flips CAD y-up to screen y-down, and maps
  layer name/color to an operation (layers hinting "engrav"/"fill"/"score" →
  engrave; **everything else cuts** — DXF rarely encodes cut-vs-engrave, so the
  safe laser default is cut).
- **raster `.png` / `.jpg` / `.gif`** — embedded as a single **fill-engrave**
  image object (`format.go`), grayscaled, scaled to fit the material minus
  margins, anchored top-left. Set engrave power/dither in MakeIt!.

## Pipeline

1. **Parse SVG** (`svgdoc.go`, `svgpath.go`). Walks the XML, accumulating
   `transform` matrices down the `<g>` tree. Handles `<path>`, `<rect>` (incl.
   `rx`/`ry`), `<circle>`, `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`.
   Every path command is reduced to **absolute `M`/`L`/`C`/`Q`/`Z`** — relative
   commands, `H`/`V`, smooth `S`/`T`, and elliptical arcs `A` are all converted
   (arcs → cubic Béziers). Coordinates come out in **millimetres**. Each element's
   effective `fill`/`stroke` is resolved (presentation attrs < `<style>` `.class`
   rules < inline `style`) and classified into a **role**: a red stroke
   (`#E61F19`/`#FF0000`/`red`) → **cut**; any non-`none` fill → **fillEngrave**;
   any other stroke → **engrave** (line). Unstyled geometry defaults to cut.

2. **Units → mm** (`--scale`, else auto). With an explicit physical
   `width`/`height` **and** a `viewBox`, the factor is `width_mm / viewBox_w`.
   Otherwise **1 user unit = 1 mm** (matches MakeIt!'s observed import). Per-axis
   non-uniform `viewBox` scaling is not applied (uniform factor only).

3. **Detect pieces** (`piece.go`). Only **cut** subpaths form pieces, grouped by
   **even/odd containment**: a loop nested inside an even number of others is an
   *outer* boundary that starts a new piece; an odd-depth loop is a *hole* of its
   immediate parent. So a ring (outer circle + inner circle) is one piece with a
   hole. Engrave/fillEngrave shapes become **marks**: each is attached (by
   point-in-polygon) to the cut piece whose outer loop contains it, so it travels
   with that piece through nesting and is placed by the same transform. A mark not
   inside any cut outline is an **orphan** (typically annotation/label text):
   orphans that share an SVG `<g>` group — e.g. all the glyphs of a comment line
   like `wooden_mug, thickness: 6mm` — are bundled into **one** piece so the text
   isn't scattered across the layout; ungrouped orphans stand alone. Marks keep
   their source-element grouping so glyph holes render via the element's fill rule.
   Marks do **not** enlarge a piece's nesting footprint (they're interior detail).

4. **Nest** (`nest.go`, `raster.go`). The sheet size comes from `--material`.
   Two distances: `--spacing` (default 3 mm) is the **gap between pieces**, and
   `--margin` (default 10 mm) is the **border between the layout and the material
   edge** (leeway against the sheet edge). The usable nesting area is the sheet
   shrunk by `--margin` on every side and the layout is **anchored at that inset**
   (top-left). Each piece is **rasterised at its true filled footprint** (even-odd
   fill, so holes are empty space), then placed with a top-left first-fit
   heuristic, largest piece first. Pieces are tried at several **rotations** and
   the lowest-resting orientation wins. The footprint is dilated by `--spacing`
   for collision tests, so placed parts keep their gap; occupancy is marked with
   the undilated footprint. Because nesting uses the real polygon (not the
   bounding box), concave parts **interlock** and a small part can drop into a
   larger part's hole.

   **Engrave alignment** (on by default; `--no-engrave-align` to disable). Raster
   engraving sweeps the head horizontally, so engraving that spans a tall vertical
   band costs many short scan lines and lots of head travel. When a piece carries
   engrave/fillEngrave marks, the nester also considers the four axis-aligned
   orientations (even under `--rotations 1`) and **prefers the one whose engraving
   is shortest/flattest** — it only takes the rotation when it shortens the
   engrave band by ≥20 %, so near-square art and already-horizontal items stay
   put. Cut-only pieces are unaffected and never rotate beyond `--rotations`. The
   selection key is: smallest engrave height, then top-most, then left-most.

   **Engrave grouping** (on by default; `--no-group-engrave` to disable). When a
   design has both engrave-bearing and cut-only pieces, the cut-only pieces are
   nested onto their own sheets first and the engrave-bearing pieces onto separate
   sheets after them. So all engraving is **consolidated on the later canvas(es)**
   and the earlier sheets are cut-only — you set cut settings once on a clean
   sheet and do all the (slow) engraving on another. It trades some packing
   density (a cut-only sheet isn't backfilled with engrave parts) for that
   separation. No effect on designs that are all-cut or all-engrave.

5. **Emit `.wws`** (`wws.go`, `cover.go`). One canvas per sheet. Each piece's
   exact geometry is transformed into absolute-mm Fabric `path` command arrays
   with `scaleX = scaleY = 1`, `angle = 0`, and `left`/`top` = the path's exact
   bounding-box min (including Bézier extrema, to match Fabric's own bbox math).
   Each object's stroke/fill/`processMode` follow its role: **cut** → stroke
   `#E61F19`, `processMode "cut"`; **engrave** → stroke = the line color (e.g.
   `#0000FF`), `processMode "engrave"`; **fillEngrave** → fill+stroke = the fill
   color (e.g. `#000000`), `processMode "fillEngrave"`. `processList` is keyed by
   each object id; `layerDataList` gets one `type:"color"` header per distinct
   color on the canvas, followed by that color's shape rows. A PNG thumbnail of
   sheet 1 (cut in red, marks in gray) is rendered for the `cover`. Target format
   `version: "3.0.4"`.

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
  `1` disables rotation for cut-only pieces; `4` restricts to 90° steps (good for
  grain direction); higher values let parts interlock at finer angles.
- `--no-engrave-align` keeps engrave pieces in their drawn orientation. By default
  they may rotate to flatten the engraving (faster raster), independent of
  `--rotations`.
- `--no-group-engrave` lets engrave parts share sheets with cut-only parts. By
  default engraving is consolidated onto its own sheet(s), after the cut-only
  ones.

## Behaviour notes

- **Position is top-left only.** The tool does not place the material on the bed
  — it anchors the layout at the canvas top-left with `--margin` of border.
  Move it to wherever you want on the bed inside MakeIt!.
- **Oversized parts error out.** If a single piece can't fit on an empty sheet
  (after the `--margin` border), conversion fails with the piece size vs sheet
  size — enlarge `--material` or reduce `--margin`. (Splitting one part across
  sheets with joinery is intentionally out of scope.)
- **Spacing is a floor, not exact.** Square dilation on the grid guarantees at
  least `--spacing` of clearance between pieces (often a hair more at coarse grids).
- **Color drives the operation.** Red strokes cut; filled shapes fill-engrave;
  other strokes line-engrave. Override any layer's operation/power in MakeIt!.
- **Each closed outline is an independent part.** If a single physical part is
  drawn as several disconnected strokes, they'll be nested separately. Group such
  geometry into one closed outline (with holes) if it must stay together.

## Current limits / TODO

- `<use>`, `<defs>` instantiation, `<text>`, and embedded `<image>` are ignored
  (only the listed vector shapes are read).
- Cut + engrave + fill-engrave are mapped from stroke/fill color. CSS support is
  limited to `.class` and inline-`style` `fill`/`stroke` (no id/element selectors,
  specificity, `currentColor`, or gradients). Engrave/fill power & speed default
  low — set them per material in MakeIt!.
- No kerf compensation (cut lines are placed on the SVG geometry exactly).
- Nesting is a greedy heuristic, not globally optimal; very dense jobs may leave
  some waste a genetic-algorithm nester would recover.
- **Not yet verified on hardware.** Outputs pass structural validation and a
  no-overlap/spacing raster check, but a converter file has not been confirmed to
  open in MakeIt! — open one and check dimensions before cutting.
