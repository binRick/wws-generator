# Test plan

Run before pushing any change:

```bash
./scripts/test.sh
```

It exits non-zero on the first failure and prints `ALL CHECKS PASSED` on success.
The script builds both binaries into a temp dir, runs the Go suite, and smoke-tests
the CLIs against the in-repo sample (`samples/test-parts.svg`,
`samples/square-100.known-good.wws`) so it needs **no external files**. If a folder
of real `.wws` files exists at `~/Desktop/cups/WWS` (or `WWS_LIB=/path`), it also
sweeps the whole library and checks every SVG is well-formed.

## What runs

| Stage | Command | Checks |
| --- | --- | --- |
| Format | `gofmt -l` | no unformatted Go files |
| Vet | `go vet ./...` | no suspicious constructs |
| Build | `go build` | both `svg2wws` and `wws2svg` compile |
| Unit + e2e | `go test ./...` | the test matrix below |
| svg2wws smoke | CLI runs | default, rotation/grid variants, spill, oversized-error |
| wws2svg smoke | CLI runs | sample converts to a valid SVG |
| Library sweep (optional) | CLI batch | all real files convert; every SVG is well-formed XML |

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
| Rotation / grid flag variants | `scripts/test.sh` (CLI smoke) |
| Spill summary line | `scripts/test.sh` (CLI smoke) |

### `wws2svg` (`.wws` → SVG)

| Feature | Test |
| --- | --- |
| Fabric transform, angle 0 + flip → correct AABB | `TestFabricAABBAngle0Flip` |
| Rotation about origin + scale preserves edge lengths | `TestFabricRotationOrigin` |
| Nested group transform composition | `TestFabricGroupCompose` |
| End-to-end: one canvas → one SVG, right element + object count | `TestWWSToSVGsSample` |
| Batch over a directory; well-formed XML for every file | `scripts/test.sh` (library sweep) |
| No geometry dropped (incl. inside groups) | library sweep + element-count parity (manual) |

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
