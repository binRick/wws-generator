# WeCreat MakeIt! `.wws` file format — reverse-engineered reference

> Status: reverse-engineered by inspecting real files. **Undocumented and unofficial.**
> WeCreat publishes no spec and markets `.wws` as a closed/proprietary format with
> "no export." In reality the file is **plain JSON** and the geometry model is
> **Fabric.js**, so it is fully readable *and writable*.

## TL;DR

- A `.wws` file is a single **UTF-8 JSON object** (no zip, no binary, no encryption).
  `file` reports it as `JSON data`.
- The canvas/object model is **[Fabric.js](http://fabricjs.com/)** — objects carry
  `"version":"5.3.0"` and Fabric's exact serialization fields (`originX`,
  `strokeUniform`, `paintFirst`, `globalCompositeOperation`, `path` arrays, …).
- **We have confirmed write works**: a hand-built JSON file opened correctly in
  MakeIt! and showed a 100×100 mm cut square. See
  `samples/square-100.known-good.wws`.
- App/format `version` seen in the wild: `2.0.4`, `3.0.0`, `3.0.4`. **Target `3.0.4`**
  for generation (that's what a current MakeIt! writes and what we verified opens).

## Top-level structure

```jsonc
{
  "name": "square-test",                 // project name (shown in library)
  "version": "3.0.4",                     // MakeIt! app/format version
  "projectId": "project-<uuid>",
  "canvasList": [ Canvas, ... ],          // one entry per "canvas"/work area
  "processList": { "<objectId>": Process, ... },  // laser settings keyed by object id
  "time": 1781744744637,                  // epoch ms (Date.now())
  "currentCanvasId": "canvas-<uuid>",
  "cover": "data:image/png;base64,...",   // library thumbnail (PNG data URI)
  "layerDataList": [ LayerData, ... ],    // per-canvas layer grouping by color
  // seen in some files (v2/v3 of real designs), optional in minimal files:
  "remark": "...",
  "canvasParamsListArray": [...]
}
```

A minimal, known-working file only needs: `name`, `version`, `projectId`,
`canvasList` (1 canvas, 1 object), `processList` (1 entry), `time`,
`currentCanvasId`, `cover`, `layerDataList`. See `samples/square-test.annotated.json`.

## Canvas object (`canvasList[]`)

```jsonc
{
  "id": "canvas-<uuid>",
  "name": "canvas01",
  "color": "#ffff00",
  "objects": [ FabricObject, ... ],       // the shapes on this canvas
  "workModeData": {
    "canvasID": "canvas-<uuid>",          // must match this canvas's id
    "perimeter": 314.16,                  // informational; not load-critical
    "diameter": 100,                      // informational (rotary/default field)
    "workMode": "plane",                  // "plane" = flat bed (vs rotary)
    "pathPlanning": "AUTO_MERGE",         // also seen: "AUTO"
    "backgroundImage": null,
    "baseplateDistance": 0,               // (v3)
    "scanMode": "SCAN_ONE_WAY"            // (v3)
  }
}
```

## Fabric objects (`canvasList[].objects[]`)

These are standard Fabric.js serialized objects. Types seen: `rect`, `path`,
`group`, `image`, plus text types. **All units are millimetres** at the canvas
level (see "Units & scaling").

### Common fields (every object)
`type, version ("5.3.0"), originX ("left"), originY ("top"), left, top, width,
height, fill, stroke, strokeWidth, strokeDashArray, strokeLineCap,
strokeDashOffset, strokeLineJoin, strokeUniform, strokeMiterLimit, scaleX, scaleY,
angle, flipX, flipY, opacity, shadow, visible, backgroundColor, fillRule,
paintFirst, globalCompositeOperation, skewX, skewY, id, name, hasControls, isLock`

### MakeIt!-specific fields added onto Fabric objects
- `id` — `"el-<uuid>"`. **This is the join key into `processList` and `layerDataList`.**
- `originStroke` — default stroke, e.g. `"#FFA500"` (orange).
- `originStrokeForCut` — cut stroke, `"#E61F19"` (red).
- `sequence` — cut/process order (integer).
- `processMode` — `"cut"` | `"engrave"` | `"fillEngrave"`.
- `fixed`, `selectable`, and (version-dependent) `isVariableThickness`,
  `isIgnoreWork`.

### `rect`
Adds `rx`, `ry`. Effective size = `width*scaleX` × `height*scaleY` (mm).

### `path` — how real designs store geometry
The `path` field is a **Fabric path-command array** — nested arrays of SVG path
commands with absolute coordinates:

```jsonc
"path": [
  ["M", 16.1, 106.2],
  ["L", 29, 106.2],
  ["C", 29.1, 106.2, 29, 106.1, 29, 106.2],
  ["L", 29, 112.1],
  ...
]
```

Observed commands: `M`, `L`, `C` (Fabric normalizes to `M/L/C/Q/Z`; expect to
emit only these — convert `H/V/S/T/A`, relative commands, etc. before writing).
`width`/`height` are the **path bounding box** in path-coordinate space; `left`/
`top` position the bbox on the bed; `scaleX`/`scaleY` map path units → mm.

**Two real-world storage styles seen (both valid):**
- `6mm-box.wws`: coords already in mm, `scaleX≈1` → effective 290×102 mm.
- `6MM-5X5-DICE-HOLDER.wws`: coords in a large internal space (e.g. `11003.49`),
  `scaleX≈0.02568` → `3919.02 × 0.02568 ≈ 100.6 mm`.

So: **effective_mm = coord × scale**. A generator can pick either style; the
cleanest is coords in mm with `scaleX = scaleY = 1`.

### `image` (raster engraving)
Has a `src` data URI (base64 PNG/JPG) instead of `path`.

### `group`
Has nested `objects`. Note: `processList` contains entries for the **child**
object ids inside groups too (that's why a file can have more `processList`
entries than top-level objects). For generation, prefer flat objects (no groups).

## `processList` — laser settings (keyed by object id)

```jsonc
"processList": {
  "el-<uuid>": {
    "cut":         { "power": 100, "speed": 3,  "repeat": 1 },
    "engrave":     { "power": 0,   "speed": 2,  "repeat": 1 },
    "fillEngrave": { "power": 0,   "speed": 2,  "repeat": 1 },
    "processMode": "cut",          // which of the above is active
    "isLock": false,
    "strengthCutting": false,
    "oneWayScan": true,
    "lineDesity": 100,             // [sic] spelling in the format
    "dpi": 254,                    // (v3)
    "dotDuration": 230,            // NOTE: also seen misspelled "dotDruation" in v3 files
    "connectPointEnable": ...,     // (some files)
    "breakPointObj": {
      "breakPointNum": 2, "breakPointSize": 0.4,
      "isBreakPoint": false, "isAutoBreakPoint": true
    },
    "processAngle": 0,
    "radius": 0
  }
}
```

- `power`: 0–100 (%). `power: 0` = unset; the user usually dials it in per material.
- `speed`: machine units (mm/s-ish); real cuts seen at 3–5 for 6 mm wood at power 100.
- **Field set varies by `version`** — copy the set from a same-version known-good
  file rather than inventing fields.

## `layerDataList` — color-grouped layers

```jsonc
"layerDataList": [{
  "id": "canvas-<uuid>",                 // matches the canvas id
  "data": [
    { "type": "color", "id": "#E61F19", "color": "#E61F19", "selected": false, "show": true, "fixed": false },
    { "id": "el-<uuid>", "parentGroupID": "", "base64": "", "type": "shape",
      "name": "editor.color_layer.rect", "color": "#E61F19", "selected": false, "show": true, "fixed": false }
  ]
}]
```

Layers are organized by **stroke color**. There's a `type:"color"` header row per
color, then `type:"shape"` rows referencing each object `id`.

## Color & operation conventions

- **`#E61F19` (red) = CUT.** This is the canonical cut color (`originStrokeForCut`).
- `#FFA500` (orange) = default/unassigned stroke (`originStroke`).
- `#000000` (black) appears as an imported stroke color in some designs.
- The operation actually performed is driven by `processMode` (on both the object
  and its `processList` entry), not purely by color — but keep color and
  `processMode` consistent (red ↔ cut).

## Units & scaling (the thing converters get wrong)

- Canvas units are **millimetres**. `left`/`top` are mm offsets from the canvas
  origin (top-left of the work area).
- Effective rendered size = `width * scaleX` by `height * scaleY` (mm).
- Verified: square-test stored `width 97.2758 * scaleX 1.028005 = 100.0 mm`,
  `height 94.7046 * scaleY 1.055915 = 100.0 mm`.
- For SVG → .wws: map SVG user units → mm. Evidence (`6mm-box.wws` stored a
  290 mm box at scale 1) suggests MakeIt!'s import treats **1 SVG user unit ≈ 1 mm**
  for unitless/viewBox SVGs; honor explicit `mm/cm/in` on the SVG `width`/`height`
  when present. **Always let the user verify the dimension in MakeIt! after import,
  and provide a scale/width override.**

## The `cover` thumbnail

`cover` is a PNG `data:` URI used as the library tile. A real one isn't strictly
required for the geometry, but include a valid PNG data URI to be safe. The known-
good sample reuses a real square thumbnail. (Future: render an actual preview.)

## How to generate a known-good file (proven recipe)

The lowest-risk generator **clones a same-version known-good file as a template**
and mutates geometry + ids, rather than building JSON from nothing:

1. Read a confirmed-openable v3.0.4 template (`samples/square-test.original.wws`).
2. Regenerate `projectId`, the canvas `id`, and each object `id` (use UUIDs).
3. Set object geometry (`width`/`height` in mm, `scaleX=scaleY=1`, `left`/`top`).
4. Re-key `processList` to the new object id; set `cut.power`/`cut.speed`.
5. Re-point `layerDataList` ids to the new canvas/object ids.
6. Set `name`, `time = Date.now()`, `currentCanvasId`.
7. `JSON.stringify` (no pretty-print needed) and write `*.wws`.

See `src/generate-square.js` for the working implementation that produced
`samples/square-100.known-good.wws` (confirmed to open in MakeIt!).

## Open questions / TODO for future sessions

- Exact SVG-unit → mm rule MakeIt! uses on import (verify with a known-size SVG).
- Whether MakeIt! re-parses the `path` array through Fabric on load (and thus
  tolerates `H/V/S/T/A`/relative commands) or requires pre-simplified `M/L/C/Q/Z`.
  **Assume pre-simplified is required** until proven otherwise.
- Multi-color / engrave + cut in one file (map several stroke colors → multiple
  layers + matching `processMode`).
- Minimum required field set (we've only trimmed down to the annotated sample; a
  true minimal file hasn't been bisected).
- `group`, `image` (raster engrave), and text object generation.
- Rotary/`workMode` other than `plane`.
