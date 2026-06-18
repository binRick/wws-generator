# Rendering `.wws` on the web (guide for the website team)

This is everything you need to draw a WeCreat design in a browser from the JSON
produced by `wws2json`. The JSON is designed so each object renders with a single
SVG element + transform — no geometry math on your side.

- Schema reference: [`wws2json.md`](wws2json.md)
- TL;DR: **one SVG per canvas**, coordinates are **millimetres**, each item has a
  `transform` matrix you apply verbatim.

## 1. Produce the JSON

```bash
wws2json --in design.wws --out design.json          # one file
wws2json --in design.wws --strip-images --compact    # lean, for the wire
```

`--strip-images` drops embedded raster data (replaces `href` with `null` +
`"stripped": true`) — use it if you don't need to show engraved photos.

## 2. Mental model

- **Coordinate system:** millimetres, origin **top-left, y-down** — the same as
  SVG. No axis flipping needed.
- **One canvas = one drawing.** A file can have several canvases (separate work
  areas); render each into its own `<svg>` (e.g. tabs or a vertical stack).
- **`transform` is absolute to the canvas.** It already includes any parent group
  transforms, so you never compose matrices yourself. `groupPath` is *informational*
  (use it to group/select in your UI, not for positioning).
- **Set the viewBox to the canvas (or item) bounds.** `canvas.bbox` is the content
  extent in mm; use it as the `viewBox` so the design fills the SVG.

`transform` is an SVG matrix `[a, b, c, d, e, f]`:
`x' = a·x + c·y + e`, `y' = b·x + d·y + f`. Drop it straight into
`transform="matrix(a b c d e f)"`.

## 3. Reference renderer (vanilla JS, no dependencies)

`renderer.js` — builds an `<svg>` for one canvas:

```js
const SVGNS = "http://www.w3.org/2000/svg";
const XLINK = "http://www.w3.org/1999/xlink";

// Colour outlines by laser operation (or pass colorBy:"style" to keep file colours).
const OP_COLOR = { cut: "#E61F19", engrave: "#1F6FE6", fillEngrave: "#16A34A" };

function renderCanvas(project, canvasIndex = 0, opts = {}) {
  const { colorBy = "operation", pxPerMm = 2 } = opts;
  const canvas = project.canvases[canvasIndex];
  const bb = canvas.bbox || { x: 0, y: 0, w: 100, h: 100 };

  const svg = document.createElementNS(SVGNS, "svg");
  svg.setAttribute("viewBox", `${bb.x} ${bb.y} ${bb.w} ${bb.h}`);
  svg.setAttribute("width", bb.w * pxPerMm);
  svg.setAttribute("height", bb.h * pxPerMm);

  for (const item of canvas.items) {
    if (item.style && item.style.visible === false) continue;
    const el = elementFor(item);
    if (!el) continue;                                  // e.g. stripped image
    el.setAttribute("transform", `matrix(${item.transform.join(" ")})`);
    styleEl(el, item, colorBy);
    el.dataset.id = item.id || "";                      // handy for click/hover
    svg.appendChild(el);
  }
  return svg;
}

function elementFor(item) {
  const g = item.geometry || {};
  const E = (tag) => document.createElementNS(SVGNS, tag);
  switch (item.type) {
    case "path": { const e = E("path"); e.setAttribute("d", g.d || ""); return e; }
    case "rect": { const e = E("rect");
      e.setAttribute("x", g.x || 0); e.setAttribute("y", g.y || 0);
      e.setAttribute("width", g.width || 0); e.setAttribute("height", g.height || 0);
      if (g.rx) e.setAttribute("rx", g.rx);
      if (g.ry) e.setAttribute("ry", g.ry); return e; }
    case "circle": { const e = E("circle");
      e.setAttribute("cx", g.cx || 0); e.setAttribute("cy", g.cy || 0);
      e.setAttribute("r", g.r || 0); return e; }
    case "ellipse": { const e = E("ellipse");
      e.setAttribute("cx", g.cx || 0); e.setAttribute("cy", g.cy || 0);
      e.setAttribute("rx", g.rx || 0); e.setAttribute("ry", g.ry || 0); return e; }
    case "line": { const e = E("line");
      e.setAttribute("x1", g.x1 || 0); e.setAttribute("y1", g.y1 || 0);
      e.setAttribute("x2", g.x2 || 0); e.setAttribute("y2", g.y2 || 0); return e; }
    case "polygon":
    case "polyline": { const e = E(item.type);
      e.setAttribute("points", (g.points || []).map((p) => p.join(",")).join(" ")); return e; }
    case "image": { if (!g.href) return null;           // stripped with --strip-images
      const e = E("image");
      e.setAttribute("width", g.width || 0); e.setAttribute("height", g.height || 0);
      e.setAttribute("href", g.href); e.setAttributeNS(XLINK, "href", g.href); return e; }
    case "text": { const e = E("text");
      e.setAttribute("y", g.fontSize || 16);
      e.setAttribute("font-size", g.fontSize || 16);
      e.setAttribute("font-family", g.fontFamily || "sans-serif");
      e.textContent = g.text || ""; return e; }
    default: return null;
  }
}

function styleEl(el, item, colorBy) {
  const laser = item.laser || {};
  if (item.type === "text") {                           // text is filled, not stroked
    el.setAttribute("fill", (item.style && item.style.fill) || "#000");
    return;
  }
  if (item.type === "image") return;

  let stroke, fill = "none";
  if (colorBy === "operation") {
    stroke = OP_COLOR[laser.operation] || "#888";
  } else {                                              // preserve the file's colours
    stroke = (item.style && item.style.stroke) || "#000";
    fill = (item.style && item.style.fill) || "none";
  }
  if (laser.ignored) { stroke = "#BBB"; el.setAttribute("stroke-dasharray", "2 2"); }

  el.setAttribute("fill", fill);
  el.setAttribute("stroke", stroke);
  el.setAttribute("stroke-width", (item.style && item.style.strokeWidth) || 0.2);
  el.setAttribute("vector-effect", "non-scaling-stroke"); // crisp lines at any zoom
}
```

## 4. Minimal page

`index.html`:

```html
<!doctype html>
<meta charset="utf-8">
<title>WWS preview</title>
<style>
  body { font: 14px system-ui, sans-serif; margin: 24px; }
  svg  { border: 1px solid #ddd; background: #fff; max-width: 100%; }
  .op  { display: inline-flex; align-items: center; gap: 4px; margin-right: 12px; }
  .sw  { width: 12px; height: 12px; display: inline-block; }
</style>
<div id="legend"></div>
<div id="app"></div>
<script src="renderer.js"></script>
<script>
  const legend = { cut: "#E61F19", engrave: "#1F6FE6", fillEngrave: "#16A34A" };
  document.getElementById("legend").innerHTML = Object.entries(legend)
    .map(([k, c]) => `<span class="op"><span class="sw" style="background:${c}"></span>${k}</span>`)
    .join("");

  fetch("design.json").then((r) => r.json()).then((project) => {
    const app = document.getElementById("app");
    project.canvases.forEach((c, i) => {
      const h = document.createElement("h3");
      h.textContent = `Canvas ${i + 1}: ${c.name} — ${c.items.length} items` +
        (c.material ? ` — ${c.material.thicknessMm} mm stock` : "");
      app.append(h, renderCanvas(project, i, { colorBy: "operation", pxPerMm: 2 }));
    });
  });
</script>
```

That's a complete, working previewer: colours by operation, dashes ignored parts,
shows material thickness, and renders every canvas.

## 5. Per-type cheat-sheet

| `type` | element | attributes from `geometry` |
| --- | --- | --- |
| `path` | `<path>` | `d` |
| `rect` | `<rect>` | `x, y, width, height` (+ `rx, ry`) |
| `circle` | `<circle>` | `cx, cy, r` |
| `ellipse` | `<ellipse>` | `cx, cy, rx, ry` |
| `line` | `<line>` | `x1, y1, x2, y2` |
| `polygon` / `polyline` | same | `points` = `[[x,y], …]` → `"x,y x,y …"` |
| `image` | `<image>` | `width, height, href` (`null` if stripped) |
| `text` | `<text>` | `text, fontSize, fontFamily` |

Every element gets `transform="matrix(...)"` from `item.transform`. That's the
whole contract.

## 6. Using the laser settings in your UI

Each item's `laser` block is ready for a properties panel / tooltip:

```js
function describe(item) {
  const l = item.laser;
  if (!l) return "no laser settings";
  if (l.ignored) return `${l.operation || "—"} (ignored)`;
  const s = l[l.operation];                       // active operation's numbers
  return s ? `${l.operation}: ${s.power}% power, speed ${s.speed}, ${s.passes}× passes`
           : (l.operation || "—");
}
```

- `operation` ∈ `cut | engrave | fillEngrave`; the matching block (`laser.cut`,
  `laser.engrave`, `laser.fillEngrave`) holds `power` (0–100 %), `speed`, `passes`.
- `ignored: true` means the object is excluded from the laser job — render it muted.
- `lineDensity` / `dpi` apply to engraving.

## 7. Notes & gotchas

- **Scale:** `viewBox` is in mm; set `width/height = bbox * pxPerMm`, or omit them and
  size the `<svg>` with CSS. `vector-effect: non-scaling-stroke` keeps outlines crisp
  when zoomed; remove it if you want true-mm stroke widths.
- **Sheet size is not in the file.** There's no material width/height in a `.wws`;
  use `canvas.bbox` for the viewport. (Material *thickness* is in `canvas.material`
  when set.)
- **Text** is described (string + font), not converted to outlines — it won't match
  MakeIt!'s glyph rendering exactly. If you need pixel-accurate text, convert text to
  paths in MakeIt! before exporting.
- **Big designs:** some files have thousands of paths per canvas. Building one SVG is
  fine; if you hit perf limits, render to `<canvas>` instead (apply the same matrix via
  `ctx.setTransform(a,b,c,d,e,f)` then draw the geometry) or virtualize by `groupPath`.
- **Coordinates** are rounded (geometry 3 dp, transform 6 dp) — plenty for display.
