# `wws2json` — `.wws` → detailed JSON

Decodes a WeCreat MakeIt! `.wws` into a fully-typed, **renderable** JSON model: a
web front-end can draw the design from this JSON alone, and it surfaces every
decoded property (geometry, transforms, style, and laser settings) — a complete
read of the format.

```bash
go build -o wws2json ./cmd/wws2json

wws2json --in design.wws                      # JSON to stdout
wws2json --in design.wws --out design.json
wws2json --in ~/Desktop/cups/WWS --out ./json # batch a directory
```

## Options

| Flag | Meaning |
| --- | --- |
| `--in FILE\|DIR` | a `.wws` file, or a directory of them (required) |
| `--out FILE\|DIR` | output file (single) or directory (batch); default: stdout for a single file, else next to each input |
| `--strip-images` | replace embedded image data with `{ "href": null, "stripped": true }` (much smaller JSON) |
| `--compact` | minified JSON (default is pretty-printed) |

## Schema

```jsonc
{
  "name": "leather-gravings",
  "version": "2.0.7",          // source MakeIt! format version
  "projectId": "project-…",
  "units": "mm",               // all coordinates are millimetres
  "canvases": [
    {
      "id": "canvas-…",
      "name": "canvas01",
      "index": 0,
      "material": { "thicknessMm": 5, "variableThickness": false },  // omitted if not set
      "bbox": { "x": 52.4, "y": 30.7, "w": 315.3, "h": 228.6 },      // content bounds, mm
      "items": [ Item, … ]
    }
  ]
}
```

### Item

```jsonc
{
  "id": "el-…",
  "type": "path",   // path | rect | circle | ellipse | line | polygon | polyline | image | text
  "name": "element",

  // Maps the local `geometry` coordinates to canvas millimetres, as an SVG matrix
  //   [a, b, c, d, e, f]  =>  x' = a*x + c*y + e,  y' = b*x + d*y + f
  // Render directly, e.g. <g transform="matrix(a b c d e f)"> … </g>.
  "transform": [1, 0, 0, 1.013381, 105.43109, -18.074337],

  // Local geometry (pre-transform). Keys depend on `type` — see below.
  "geometry": { "d": "M90.8 211.8 C…" },

  // Axis-aligned bounds in canvas millimetres (after transform).
  "bbox": { "x": 161.08, "y": 123.04, "w": 68.1, "h": 73.68 },

  "style": { "stroke": "#000000", "fill": "#000000", "strokeWidth": 0, "opacity": 1, "visible": true },

  // Decoded laser job for this object.
  "laser": {
    "operation": "fillEngrave",   // active mode: cut | engrave | fillEngrave
    "ignored": false,             // isIgnoreWork — excluded from the job
    "cut":         { "power": 100, "speed": 7,   "passes": 1 },
    "engrave":     { "power": 55,  "speed": 250, "passes": 1 },
    "fillEngrave": { "power": 55,  "speed": 250, "passes": 1 },
    "lineDensity": 100,
    "dpi": 256
  },

  // Ancestor group ids (outermost first); omitted when the item is top-level.
  "groupPath": ["el-group-…"]
}
```

`power` is 0–100 %, `speed` is the machine's units, `passes` is the repeat count.
The three operation blocks are all stored in the file; `operation` says which one
is active.

### `geometry` by type

| `type` | keys |
| --- | --- |
| `path` | `d` (SVG path data, local coords; only `M/L/C/Q/Z`) |
| `rect` | `x`, `y`, `width`, `height`, optional `rx`, `ry` |
| `circle` | `cx`, `cy`, `r` (centred at origin) |
| `ellipse` | `cx`, `cy`, `rx`, `ry` |
| `line` | `x1`, `y1`, `x2`, `y2` |
| `polygon` / `polyline` | `points`: `[[x,y], …]` |
| `image` | `width`, `height`, `href` (data URI, or `null` + `stripped:true` with `--strip-images`) |
| `text` | `text`, `fontSize`, `fontFamily` |

## Rendering tip

Each item is self-contained: apply its `transform` to the local `geometry`. In SVG
that's a one-liner; on a canvas, `ctx.setTransform(a,b,c,d,e,f)` then draw. Group
membership is informational (`groupPath`) — the transform is already absolute to the
canvas, so you don't need to compose group matrices yourself.

**Website team:** see [`wws2json-web-renderer.md`](wws2json-web-renderer.md) for a
complete, dependency-free reference renderer (HTML + JS) and a per-type cheat-sheet.

## Notes / limits

- Coordinates are millimetres; transform components are rounded to 6 dp, geometry to
  3 dp.
- `text` is described (string + font), not converted to glyph outlines.
- `material.thicknessMm` appears only for canvases where the file recorded it.
  Sheet **width/height** is **not** stored anywhere in a `.wws`.
- Validated across the 339-file reference library: every file produces valid JSON.
