#!/usr/bin/env node
/*
 * generate-square.js — proof-of-concept WeCreat MakeIt! .wws generator.
 *
 * Produces a clean N×N mm cut-square .wws by cloning a known-good v3.0.4 template
 * and mutating geometry + ids. The output of this script (a 100x100 square) was
 * confirmed to open correctly in MakeIt! — see samples/square-100.known-good.wws.
 *
 * This is the *write* proof. The general SVG -> .wws converter builds on this
 * envelope; the only added work there is turning SVG geometry into Fabric path
 * command arrays. See docs/wws-format.md.
 *
 * Usage:
 *   node src/generate-square.js [sizeMM] [outFile]
 *   node src/generate-square.js 100 ~/Desktop/cups/WWS/gen-square-100.wws
 */
const fs = require('fs');
const path = require('path');
const { randomUUID } = require('crypto');

const SIZE = Number(process.argv[2] || 100);
const OUT = process.argv[3] || path.join(process.cwd(), `square-${SIZE}.wws`);
const TEMPLATE = path.join(__dirname, '..', 'samples', 'square-test.original.wws');

const CUT_RED = '#E61F19';

const t = JSON.parse(fs.readFileSync(TEMPLATE, 'utf8'));

const projectId = 'project-' + randomUUID();
const canvasId  = 'canvas-'  + randomUUID();
const objId     = 'el-'      + randomUUID();

const canvas = t.canvasList[0];
const obj    = canvas.objects[0];
const proc   = t.processList[obj.id];

t.name = path.basename(OUT).replace(/\.wws$/i, '');
t.projectId = projectId;
t.currentCanvasId = canvasId;
t.time = Date.now();

canvas.id = canvasId;
canvas.workModeData.canvasID = canvasId;

// Clean geometry: SIZE×SIZE mm at scale 1, positioned on the bed.
obj.id = objId;
obj.type = 'rect';
obj.width = SIZE;
obj.height = SIZE;
obj.scaleX = 1;
obj.scaleY = 1;
obj.left = 82.94;          // mm from canvas origin (top-left); from template
obj.top = 49.87;
obj.stroke = CUT_RED;
obj.processMode = 'cut';

// Re-key processList to the new object id (one entry per object).
for (const k of Object.keys(t.processList)) delete t.processList[k];
proc.processMode = 'cut';
// proc.cut.power / proc.cut.speed left as-is; user sets per material in MakeIt!.
t.processList[objId] = proc;

// Re-point layerDataList ids to the new canvas/object ids.
const ld = t.layerDataList[0];
ld.id = canvasId;
for (const d of ld.data) {
  if (d.type === 'shape') { d.id = objId; d.color = CUT_RED; }
  if (d.type === 'color') { d.id = CUT_RED; d.color = CUT_RED; }
}

fs.writeFileSync(OUT, JSON.stringify(t));
console.log(`Wrote ${OUT} (${fs.statSync(OUT).size} bytes)`);
console.log(`Effective: ${obj.width * obj.scaleX} x ${obj.height * obj.scaleY} mm, cut (${CUT_RED})`);
