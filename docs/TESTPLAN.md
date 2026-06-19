# Test plan

Run before pushing any change:

```bash
./scripts/test.sh
```

It exits non-zero on the first failure and prints `ALL CHECKS PASSED` on success.
The script builds all three binaries into a temp dir, runs the Go suite, then
hammers the CLIs: it converts **every input format** (SVG/PDF/AI/DXF/raster) from
self-contained fixtures it generates at runtime, exercises every flag, fires a
battery of malformed/edge inputs that must error cleanly and **never panic**,
stress-tests the nester, and validates output integrity (valid v3.0.4 JSON, one
`processList` entry per object, everything within the material). It needs **no
external files** (`python3` is required). If a folder of real `.wws` files exists
at `~/cups/WWS` (or `WWS_LIB=/path`), it also sweeps the whole library.

## What runs

| Stage | Command | Checks |
| --- | --- | --- |
| Format | `gofmt -l` | no unformatted Go files |
| Vet | `go vet ./...` | no suspicious constructs |
| Build | `go build` | all three binaries compile |
| Unit + e2e | `go test ./...` | the test matrix below |
| Every format | CLI runs | SVG/PDF/AI/DXF/PNG each → a valid `.wws`; PDF→cut+engrave, PNG→fillEngrave image |
| Every flag | CLI runs | spacing/margin/grid/rotations(count+list)/scale/power/speed/passes/name/no-engrave-align/no-group-engrave; `--name`/`--power`/`--passes` propagate; engrave consolidated |
| Adversarial | CLI runs | ~23 malformed/edge inputs (bad files, missing/garbage flags, oversized, 0/negative material/grid/spacing/margin) all exit non-zero and **never panic** |
| Stress | CLI runs | 60-piece multi-sheet spill, 6×6 tiny sheet, 32 rotations — all valid + in-bounds |
| Integrity | CLI runs | round-trip svg→wws→svg keeps all 5 paths; wws→json valid; bad `.wws` errors non-zero |
| Library sweep (optional) | CLI batch | all real files convert (non-zero exit if any fails); every SVG well-formed, every JSON parses |

## Feature → test coverage

### `svg2wws` (SVG → `.wws`)

| Feature | Test |
| --- | --- |
| Path command simplification (`H/V`/relative → `M/L/C/Q/Z`) | `TestParsePathSimplifies` |
| Arc → cubic conversion | `TestArcToCubicSemicircle` |
| Unit scaling from `viewBox` + physical width | `TestUnitScaleFromViewBox` |
| Piece detection / hole grouping (ring = outer + hole) | `TestRingGroupsHole` |
| True-polygon nesting: no overlap + ≥ spacing | `TestNestNoOverlapAndSpacing` |
| End-to-end: valid v3.0.4 JSON, objects within material, top-left anchor, `processList` keyed to objects | `TestConvertEndToEnd` |
| Multi-sheet spill onto extra canvases | `TestMultiSheetSpill` |
| Oversized single piece → error | `TestOversizedErrors` |
| Color → operation mapping (red=cut, fill=fillEngrave, other stroke=engrave) + per-color layers | `TestColorRoleMapping` |
| Engrave-align rotates a tall engraving flat | `TestEngraveAlignFlattensEngraving` |
| Engrave grouping onto separate sheets | `TestGroupEngraveSeparatesSheets` |
| Floating label glyphs sharing a `<g>` stay one block | `TestLabelGroupStaysTogether` |
| `<use>`/`<defs>`/`<symbol>`, style inheritance, `rgb()` colors | `TestUseDefsRGBInheritance` |
| PDF/AI: content-stream interpreter (red stroke=cut, black fill=engrave) | `TestPDFInterpreter` |
| DXF: LWPOLYLINE + CIRCLE → geometry with hole | `TestDXFBasic` |
| Raster image → fit-to-sheet fillEngrave image object | `TestRasterImageEngrave` |
| Every input format, every flag, adversarial battery, stress, integrity | `scripts/test.sh` (CLI) |

### `wws2svg` (`.wws` → SVG)

| Feature | Test |
| --- | --- |
| Fabric transform, angle 0 + flip → correct AABB | `TestFabricAABBAngle0Flip` |
| Rotation about origin + scale preserves edge lengths | `TestFabricRotationOrigin` |
| Nested group transform composition | `TestFabricGroupCompose` |
| End-to-end: one canvas → one SVG, right element + object count | `TestWWSToSVGsSample` |
| Batch over a directory; well-formed XML for every file | `scripts/test.sh` (library sweep) |
| No geometry dropped (incl. inside groups) | library sweep + element-count parity (manual) |

### `wws2json` (`.wws` → detailed JSON)

| Feature | Test |
| --- | --- |
| Decode: types, transform, geometry, bbox, style, laser, material | `TestDescribeSample` |
| Generated file decodes to cut paths with geometry + valid laser | `TestDescribeGenerated` |
| CLI emits valid JSON with expected fields | `scripts/test.sh` (smoke) |
| Batch over a directory; every file produces valid JSON | `scripts/test.sh` (library sweep) |

### Round trip

| Property | Test |
| --- | --- |
| `svg → wws → svg` keeps every piece (5 pieces → 5 `<path>`) | `TestRoundTrip` |

## Manual fidelity check (not automated)

`go test` proves placement maths; for a visual gut-check, render an output SVG and
eyeball it (requires `rsvg-convert` / ImageMagick):

```bash
./wws2svg --in ~/Desktop/cups/WWS/pentagram.wws --out /tmp/svg
rsvg-convert -w 600 -b white /tmp/svg/pentagram.svg -o /tmp/pentagram.png
open /tmp/pentagram.png
```

Note: the `cover` PNG embedded in a `.wws` is MakeIt!'s **workspace** thumbnail
(bed texture/grid, sometimes stale), not a clean render of the file's geometry —
don't expect a pixel match.

## Not yet covered (known gaps)

- No automated AABB cross-check inside `go test` (done ad hoc in the library sweep
  notes; could be added with a synthetic fixture).
- `wws2svg` text is approximate (no font metrics) — not asserted.
- Neither converter is hardware-verified in MakeIt! yet.
