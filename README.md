# wws-generator

Read and generate **WeCreat MakeIt!** `.wws` project files for a WeCreat 60W laser
(WeCreat Vision) — programmatically, instead of only by hand in the closed MakeIt! GUI.

## Why this is possible

WeCreat markets `.wws` as a proprietary, no-export format. But under the hood a
`.wws` file is just **plain JSON**, and its canvas/object model is
**[Fabric.js](http://fabricjs.com/)**. That makes it fully readable *and writable*.

**Confirmed:** a `.wws` file built by this repo opens correctly in MakeIt! as a
100×100 mm cut square — see [`samples/square-100.known-good.wws`](samples/square-100.known-good.wws).

## Quick start

```bash
# Build the converter (single self-contained binary, stdlib only)
go build -o svg2wws ./cmd/svg2wws

# Convert an SVG, nesting its pieces onto 300×200 mm sheets
./svg2wws --in design.svg --material 300x200 --spacing 3
# → writes design.wws; open it in MakeIt! to verify

# Pieces that don't all fit spill onto extra sheets (each becomes a canvas)
./svg2wws --in parts.svg --material 120x120 --out parts.wws
```

Or just generate a plain cut square (the original proof-of-concept):

```bash
node src/generate-square.js 100 ~/Desktop/cups/WWS/my-square.wws
```

### `svg2wws` options

| Flag | Default | Meaning |
| --- | --- | --- |
| `--in FILE` | — | input SVG (required) |
| `--material WxH` | — | sheet size in mm, e.g. `300x200` (required) |
| `--out FILE` | `<input>.wws` | output file |
| `--name NAME` | output base name | project name shown in MakeIt! |
| `--spacing MM` | `3` | space around items — the gap between pieces and the border around the whole layout |
| `--grid MM` | `1.0` | nesting resolution — smaller packs tighter but is slower |
| `--rotations N\|list` | `8` | rotation candidates: a count for N evenly-spaced angles, or a comma list of degrees (`1` = no rotation, `4` = 90° steps) |
| `--scale F` | auto | force user-unit → mm factor (default 1 unit = 1 mm) |
| `--power N` | `0` | cut power 0–100 (usually set per material in MakeIt!) |
| `--speed N` | `5` | cut speed |

The layout is anchored at the canvas **top-left** (every item gets `--spacing` of
clear space around it); reposition it on the bed inside MakeIt!. See
[`docs/svg2wws.md`](docs/svg2wws.md) for how nesting works and current limits.

## Layout

| Path | What |
| --- | --- |
| [`docs/wws-format.md`](docs/wws-format.md) | Full reverse-engineered `.wws` format spec |
| [`docs/svg2wws.md`](docs/svg2wws.md) | SVG → `.wws` converter: pipeline, nesting, limits |
| [`docs/svg2wws-agent.md`](docs/svg2wws-agent.md) | Using `svg2wws` from another repo (CLI contract for AI agents) |
| [`docs/svg2wws-internals.md`](docs/svg2wws-internals.md) | Converter architecture & implementation deep-dive |
| [`CLAUDE.md`](CLAUDE.md) | Orientation for AI sessions (read first) |
| `cmd/svg2wws/` | CLI entry point for the converter |
| `internal/conv/` | Converter: SVG parse, piece detection, nesting, `.wws` emit |
| `src/generate-square.js` | Proven write recipe / proof-of-concept generator |
| `samples/square-100.known-good.wws` | Generated file **confirmed to open in MakeIt!** |
| `samples/square-test.original.wws` | Hand-made reference file (generation template) |
| `samples/square-test.annotated.json` | Pretty-printed sample (cover truncated) for reading |
| `samples/test-parts.svg` | Multi-piece SVG for exercising the converter |

## Format in one paragraph

A `.wws` is one UTF-8 JSON object: `canvasList[]` holds canvases, each with Fabric
`objects[]` (units = mm; effective size = `width*scaleX` × `height*scaleY`). Geometry
is stored as Fabric `path` command arrays (`[["M",x,y],["L",x,y],["C",...]]`). Laser
settings live in `processList`, keyed by object `id`; layers are grouped by stroke
color in `layerDataList`. Red `#E61F19` = cut. Target `version: "3.0.4"`. See
[`docs/wws-format.md`](docs/wws-format.md) for everything.

## Status

- [x] Confirmed `.wws` is writable; round-trip a generated file into MakeIt!
- [x] Reverse-engineered format reference
- [x] Proof-of-concept square generator
- [x] SVG → `.wws` converter with true-polygon nesting onto multiple sheets (`svg2wws`)
- [ ] Confirm a converter output opens in MakeIt! (pending hardware test)
- [ ] Multi-layer cut + engrave, raster image engrave, text
- [ ] Verify the exact SVG-unit → mm import rule

> Unofficial and reverse-engineered. No affiliation with WeCreat. Format details are
> inferred and may change between MakeIt! versions — always verify generated files in
> the app.
