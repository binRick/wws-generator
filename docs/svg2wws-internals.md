# `svg2wws` internals (architecture & implementation)

Maintainer-level detail on how the converter works inside. For *usage* see
[`svg2wws.md`](svg2wws.md); for *calling it from another repo* see
[`svg2wws-agent.md`](svg2wws-agent.md).

The tool is a single Go binary, standard library only. CLI in `cmd/svg2wws`,
all logic in package `internal/conv`.

## File map (`internal/conv`)

| File | Responsibility |
| --- | --- |
| `geom.go` | `Point`, `Rect`, affine `Matrix`, `Cmd`/`Subpath`, curve flattening, exact Bézier-extrema bbox, polygon area / point-in-polygon |
| `svgpath.go` | Path `d` tokenizer; converts every command to absolute `M/L/C/Q/Z`; arc→cubic |
| `svgdoc.go` | XML walk, `<g>` transform stack, root unit scale, shape→subpath conversion, `transform` parsing |
| `piece.go` | Group subpaths into pieces by even/odd containment (outer + holes) |
| `raster.go` | Rasterise a piece footprint (even-odd fill), dilate by spacing, bitset occupancy primitives |
| `nest.go` | Multi-sheet first-fit packer; rotation choice; placement→matrix |
| `wws.go` | `.wws` JSON model + `Build`; Fabric path emission; `processList`/`layerDataList`; UUIDs |
| `cover.go` | Render sheet-1 thumbnail PNG → `data:` URI |
| `convert.go` | `Convert()` orchestration (parse → pieces → nest → build → marshal) |
| `cmd/svg2wws/main.go` | Flag parsing, material/rotation parsing, file I/O, summary output |

Data flow: `Convert(io.Reader, Options)` → `ParseSVG` → `buildPieces` → `Nest` →
`Build` (+ `coverDataURI`) → JSON bytes + `Summary`.

## Core types

- `Matrix [6]float64` = SVG `matrix(a,b,c,d,e,f)`; `Apply` does
  `x'=a·x+c·y+e, y'=b·x+d·y+f`. `Mul` composes so `m.Mul(n).Apply(p) ==
  m.Apply(n.Apply(p))`.
- `Cmd{Op, P []Point}` with `Op ∈ {M,L,C,Q,Z}`; point counts M/L=1, Q=2, C=3,
  Z=0. `Subpath{Cmds, Closed}`.
- `Piece{Subpaths, Loops, Area, BBox}` — `Subpaths` is exact output geometry
  (outer first, then holes); `Loops` are the matching flattened polylines used
  for nesting; `Area` is the outer loop's |area| (placement order).
- `mask` — rasterised footprint at one rotation: undilated `rows` (mark
  occupancy) + dilated `dRows` (collision), plus `ox,oy` (the rotated footprint's
  min corner, needed to recover the placement transform).
- `Placement{Piece, Sheet, M}` — `M` maps piece-local mm → sheet mm.

## Stage 1 — SVG → absolute-mm subpaths

**XML walk** (`ParseSVG`): a manual stack of transform matrices. On each
`StartElement`, the current matrix is `parent · local`, where `local` comes from
the element's `transform` attribute (`parseTransform` supports
`matrix/translate/scale/rotate/skewX/skewY`, composed left-to-right). The root
`<svg>` prepends a uniform `scale(s)` so all downstream coordinates land in mm.

**Unit scale** (`rootScale`): if the root has a physical `width` (mm/cm/in/pt/pc/
px via `lengthToMM`) **and** a `viewBox`, `s = width_mm / viewBox_width`.
Otherwise `s = 1` (1 user unit = 1 mm). `--scale` overrides entirely. Only a
uniform factor is applied (non-uniform viewBox aspect is not corrected).

**Shapes** (`svgdoc.go`): `<rect>` (sharp or `rx/ry` rounded via 4 cubic corners,
constant `k=0.5522847498307936`), `<circle>`/`<ellipse>` (4-cubic approximation),
`<line>`, `<polyline>`/`<polygon>`. Each yields subpaths in element-local units,
then `sp.transform(m)` bakes the accumulated matrix.

**Path data** (`svgpath.go`): a tokenizer splits the `d` string into command
letters and numbers (handles signs, decimals, exponents, implicit repeats,
comma/space). `pathState` walks tokens to absolute `M/L/C/Q/Z`:
- `H/V` → `L`; relatives → absolute (tracking the pen).
- `S`/`T` → `C`/`Q` by reflecting the previous control point (only when the prior
  command was the matching curve type).
- `A` (elliptic arc) → cubics via `arcToCubics`: endpoint→center parameterisation
  (out-of-range radius correction, center/angle computation), split into ≤90°
  segments, each turned into a cubic using the `t = 4/3·tan(Δ/4)` tangent rule.
- Extra coordinate pairs after `M` are implicit `L` (per SVG spec).

## Stage 2 — pieces by containment (`buildPieces`)

1. Flatten every subpath to a polyline (`Subpath.Flatten`, tol `flattenTol = 0.1
   mm`, recursive de-Casteljau on flatness).
2. For each loop compute |area|, bbox, and a guaranteed-interior point
   (`interiorPoint`: cast a horizontal ray through the bbox mid-Y, take the
   midpoint of the widest interior span — robust for concave shapes where a
   centroid could fall outside).
3. `parent(i)` = the **smallest-area** loop that bbox-contains *i* and whose
   polygon contains *i*'s interior point. `depth` = ancestor count.
4. **Even depth → piece outer; odd depth → hole** of its immediate parent. So a
   ring (outer+inner circle) is one piece with one hole; a shape inside a hole
   (depth 2) starts a fresh piece.
5. Pieces are sorted largest-area-first (first-fit-decreasing packs better).

## Stage 3 — nesting (`Nest`, `raster.go`)

**Why raster.** Analytic No-Fit-Polygon nesting with free rotation is fragile
(robust Minkowski sums, degenerate edges). Rasterising the footprint reproduces
the desired behaviour — real-shape interlocking, free rotation (rotate the mask),
hole-filling — with far fewer failure modes. Only *placement* is quantised; the
emitted cut path stays exact vector.

**Mask build** (`buildMask`): rotate the piece's flattened loops by `deg`,
translate so the footprint min corner is `(0,0)`, then **even-odd scanline fill**
(`fillEvenOdd`) through cell centres at resolution `res = --grid`. Even-odd over
*all* loops yields outer-minus-holes automatically, so holes are free space.
`rows` are the run-length spans of filled cells; `dRows` is the footprint
**dilated** by `d = round(spacing/res)` cells (square structuring element, so
clearance is Chebyshev ≥ Euclidean ⇒ at least `spacing`).

**Occupancy** is a per-sheet bitset (`[]uint64` rows). `anySet`/`setRange` test
and mark half-open `[x0,x1)` ranges with word masks.

**Placement test** (`sheetGrid.fits`): the *undilated* footprint must lie inside
the usable grid (the sheet inset by `--spacing` on every side); the *dilated*
footprint must not collide with occupancy. The single `--spacing` value thus
serves as both the layout border (via the inset) and the inter-piece gap (via the
dilation). After placing, occupancy is marked with the *undilated* spans only — so
the next piece's dilation creates the gap.

**Search** (`findSpot` + `bestOnSheet`): scan `py` ascending, `px` ascending,
return the first fit (top-left first-fit). Across all `--rotations` masks, pick
the one whose best spot has the lowest `py` (tie: lowest `px`). Pieces are tried
on existing sheets in order; if none fits, a new sheet (canvas) is opened. If a
piece doesn't fit on an **empty** sheet → oversized **error** (exit 1).

**Placement matrix** (`mkPlacement`). A point `p` maps to sheet mm as
`R(deg)·p + t`, where `t = (spacing + px·res − ox, spacing + py·res − oy)` and
`(ox,oy)` is the rotated footprint min the mask recorded. So `M = Translate(t) ·
RotateDeg(deg)`. The exact subpaths are later transformed by this same `M`, so
vector geometry coincides with the rasterised placement.

## Stage 4 — `.wws` emission (`wws.go`)

One **canvas per sheet**. For each placement: transform the exact subpaths by
`M`, build Fabric path command arrays (`toPathArray`, coords rounded to 3 dp),
and create a `path` object.

**Fabric positioning convention (the subtle part).** We bake the full transform
(rotation + translation) into absolute mm coordinates and set `scaleX = scaleY =
1`, `angle = 0`. `left/top` are set to the path's **exact** bbox min and
`width/height` to its size — where "exact" includes Bézier extrema
(`subpathsBBox` solves the cubic/quadratic derivative roots). This matches how
Fabric.js recomputes a path's bounds and `pathOffset` on load, so a point at mm
`(x,y)` renders at `(x,y)` with no offset. This mirrors the verified real-file
style (`6mm-box.wws`: coords in mm, scale 1, `left` = path bbox min).

**Wiring.** Cut stroke `#E61F19`, `processMode "cut"`, incrementing `sequence`.
`processList[objId]` carries cut/engrave/fillEngrave power-speed-repeat (field set
copied from observed v3.0.4 files). `layerDataList` has one entry per canvas: a
`type:"color"` header (`#E61F19`) plus a `type:"shape"` row per object id.
`projectId`/canvas ids/object ids are UUID v4 (`crypto/rand`). `cover` is a PNG of
sheet 1 (`cover.go`, Bresenham outlines, white bg). Output is compact JSON,
`version "3.0.4"`, objects `"5.3.0"`.

## Coordinate conventions

SVG is y-down, top-left origin; MakeIt!'s canvas is the same orientation
(verified samples store positive top-left mm). **No Y flip** is applied — pieces
keep their drawn orientation (modulo nesting rotation). The tool does not position
the material on the bed: the layout is anchored at the canvas top-left (the
top-left piece sits at `(spacing, spacing)`) and the user repositions it in
MakeIt!.

## Performance & tuning

- Per piece: `O(rotations · positions · spans)`. `findSpot` returns at the first
  fit, so early pieces are cheap; cost rises as sheets fill.
- `--grid` halves → ~4× positions and ~4× memory. `--rotations` scales linearly.
- Defaults (`--grid 1.0 --rotations 8`) convert a handful of pieces instantly.
  For dense jobs prefer `--grid 0.5`; for grain-sensitive stock use `--rotations
  4` (90°) or `1` (none).

## Tests (`conv_test.go`)

`go test ./...` covers: path simplification of `H/V`/relative commands; arc→cubic
radius accuracy; ring → one piece with a hole; viewBox unit scaling; and an
end-to-end **no-overlap + ≥spacing** check that rasterises the actual placed
material per sheet.

## Extension points

- **Engrave/fill-engrave layers:** map non-red stroke colors to `processMode` and
  emit multiple `layerDataList` color groups (parser currently discards stroke
  color and emits cut-only).
- **Kerf compensation:** offset each loop by half the beam width before emission
  (a polygon-offset pass on `Subpaths`).
- **Better nesting:** add a genetic-algorithm ordering / bottom-left-fill variant
  over the existing raster collision core; or NFP for exact placement.
- **More SVG:** `<use>`/`<defs>` instantiation, `<text>` (font→path), `<image>`
  (raster engrave via the Fabric `image` object).

## Known risks

- **Not hardware-verified:** outputs pass structural + no-overlap validation but
  no converter file is yet confirmed to open in MakeIt!. The two most likely
  adjustment points if something looks off in the app are the path `left/top`/bbox
  convention and the unit scale.
- Field sets drift by MakeIt! version; this targets `3.0.4`.
