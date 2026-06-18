# wws-generator

Read and generate **WeCreat MakeIt!** `.wws` project files for a WeCreat 60W laser
(WeCreat Vision) — programmatically, instead of only by hand in the closed MakeIt! GUI.

## Why this is possible

WeCreat markets `.wws` as a proprietary, no-export format. But under the hood a
`.wws` file is just **plain JSON**, and its canvas/object model is
**[Fabric.js](http://fabricjs.com/)**. That makes it fully readable *and writable*.

**Confirmed:** a `.wws` file built by this repo opens correctly in MakeIt! as a
100×100 mm cut square — see [`samples/square-100.known-good.wws`](samples/square-100.known-good.wws).

## Features

Two single-file Go binaries (standard library only — no dependencies), plus the
reverse-engineered format reference.

### `svg2wws` — SVG → `.wws` (with nesting)

- **True-polygon nesting.** Packs each part by its *real filled footprint* (not its
  bounding box), so concave/irregular parts interlock tightly.
- **Free rotation.** Tries multiple orientations per part; `--rotations` takes a
  count of evenly-spaced angles or an explicit degree list (`1` = none, `4` = 90°
  steps for grain direction).
- **Hole-aware.** A loop inside another becomes a *hole* that stays attached to its
  parent part, and small parts can nest **inside** a larger part's hole (free space).
- **Multi-sheet spill.** Parts that don't fit roll onto additional sheets — each
  sheet is emitted as its own MakeIt! **canvas**.
- **One "space around items" knob** (`--spacing`, default 3 mm): the gap between
  parts *and* the border around the whole layout. The layout is **anchored
  top-left** — reposition it on the bed in MakeIt!.
- **Full SVG geometry in.** `<path>`, `<rect>` (incl. rounded), `<circle>`,
  `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`, nested `<g transform>`; every
  path command (relative, `H/V`, smooth `S/T`, arcs `A`) is simplified to
  `M/L/C/Q/Z`. Units → millimetres (1 user unit = 1 mm, or honour explicit
  `mm/cm/in` + `viewBox`; override with `--scale`).
- **Faithful output.** v3.0.4 envelope, exact-vector cut paths (red `#E61F19`,
  `processMode: cut`), a rendered PNG thumbnail, and a hard error if a single part
  is larger than the sheet.

### `wws2svg` — `.wws` → SVG (reverse)

- **One SVG per canvas**, geometry in millimetres, stroke/fill colours preserved.
- **Faithful Fabric transforms.** Replicates Fabric's `calcOwnMatrix`
  (origin/scale/angle/flip/skew) and composes nested **group** transforms; each
  object is emitted as the matching SVG element with a `transform="matrix(...)"`.
- **Every object type:** path, rect, circle, ellipse, line, polyline, polygon,
  image (embedded), text (approximate), group.
- **Single file or batch a whole folder**; multi-canvas files become
  `<name>-canvas01.svg`, `<name>-canvas02.svg`, …
- **Validated** across a 339-file / 1604-canvas library: 1553 well-formed SVGs;
  un-rotated bounds match Fabric AABBs to < 0.03 mm.

### `wws2json` — `.wws` → detailed JSON (drive a web renderer)

- **Complete, renderable decode.** Per canvas, every object as: SVG-equivalent
  `type`, a `transform` matrix to canvas mm, local `geometry`, canvas-space `bbox`,
  `style`, decoded **laser** settings (operation, power %, speed, passes, ignored,
  line density, DPI), and `groupPath`.
- **Material thickness** surfaced where the file records it (sheet size isn't stored
  by MakeIt!). Single file → stdout, or batch a directory; `--strip-images` for
  lean output. Schema in [`docs/wws2json.md`](docs/wws2json.md).
- **Validated**: every file in the 339-file library produces valid JSON.

### Also

- **Format reference** — [`docs/wws-format.md`](docs/wws-format.md) documents the
  whole `.wws` schema (Fabric objects, `path` arrays, `processList`,
  `layerDataList`, units, colours, version differences).
- **Square generator** — `src/generate-square.js`, the original proof that `.wws`
  is writable.

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

Convert the other way — `.wws` → SVG (one SVG per canvas), single file or a whole
folder:

```bash
go build -o wws2svg ./cmd/wws2svg
./wws2svg --in design.wws --out ./svgs
./wws2svg --in ~/Desktop/cups/WWS --out ./svgs   # batch a directory
```

Or `.wws` → **detailed JSON** (geometry + transforms + style + decoded laser
settings) to drive a web renderer or inspect a design:

```bash
go build -o wws2json ./cmd/wws2json
./wws2json --in design.wws                       # JSON to stdout
./wws2json --in ~/Desktop/cups/WWS --out ./json  # batch a directory
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
| [`docs/wws2svg.md`](docs/wws2svg.md) | `.wws` → SVG converter (reverse): transforms, batch, limits |
| [`docs/wws2json.md`](docs/wws2json.md) | `.wws` → detailed JSON: schema for a web renderer |
| [`docs/wws2json-web-renderer.md`](docs/wws2json-web-renderer.md) | Website-team guide: render the JSON in a browser (+ reference renderer) |
| [`docs/svg2wws-agent.md`](docs/svg2wws-agent.md) | Using `svg2wws` from another repo (CLI contract for AI agents) |
| [`docs/svg2wws-internals.md`](docs/svg2wws-internals.md) | Converter architecture & implementation deep-dive |
| [`docs/TESTPLAN.md`](docs/TESTPLAN.md) | Test plan + feature→test coverage map |
| [`CLAUDE.md`](CLAUDE.md) | Orientation for AI sessions (read first) |
| `cmd/svg2wws/`, `cmd/wws2svg/`, `cmd/wws2json/` | CLI entry points |
| `internal/conv/` | Converters: SVG parse, nesting, `.wws` emit, `.wws`→SVG, `.wws`→JSON |
| `scripts/test.sh` | Pre-push test runner (build, unit/e2e, CLI smoke, library sweep) |
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

## Testing

Run the full suite before pushing — it builds both binaries, runs the Go unit +
end-to-end tests, smoke-tests both CLIs, and (if a `.wws` library is present)
sweeps it and checks every SVG is well-formed:

```bash
./scripts/test.sh        # prints ALL CHECKS PASSED on success
```

See [`docs/TESTPLAN.md`](docs/TESTPLAN.md) for the feature→test coverage map.

## Status

- [x] Confirmed `.wws` is writable; round-trip a generated file into MakeIt!
- [x] Reverse-engineered format reference
- [x] Proof-of-concept square generator
- [x] SVG → `.wws` converter with true-polygon nesting onto multiple sheets (`svg2wws`)
- [x] `.wws` → SVG converter, batch over a folder, one SVG per canvas (`wws2svg`)
- [x] `.wws` → detailed JSON (geometry + transforms + laser settings) for web rendering (`wws2json`)
- [ ] Confirm a converter output opens in MakeIt! (pending hardware test)
- [ ] Multi-layer cut + engrave, raster image engrave, text
- [ ] Verify the exact SVG-unit → mm import rule

> Unofficial and reverse-engineered. No affiliation with WeCreat. Format details are
> inferred and may change between MakeIt! versions — always verify generated files in
> the app.
