#!/usr/bin/env bash
#
# Pre-push test runner for wws-generator. Exercises every feature of all three
# binaries across every input format, hammers them with malformed/edge inputs
# (which must error cleanly and NEVER panic), stress-tests the nester, and
# validates output integrity. Exits non-zero on the first failure.
#
#   ./scripts/test.sh
#
# Optional: WWS_LIB points at a folder of real .wws files for a full library
# sweep (defaults to ~/cups/WWS if present; skipped otherwise).

set -euo pipefail
cd "$(dirname "$0")/.."

GREEN=$'\033[32m'; RED=$'\033[31m'; DIM=$'\033[2m'; OFF=$'\033[0m'
step() { printf '\n%s==> %s%s\n' "$GREEN" "$1" "$OFF"; }
fail() { printf '%s!! %s%s\n' "$RED" "$1" "$OFF"; exit 1; }
ok()   { printf '  %s\n' "$1"; }

command -v python3 >/dev/null 2>&1 || fail "python3 is required for the test suite"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SVG2WWS="$TMP/svg2wws"; WWS2SVG="$TMP/wws2svg"; WWS2JSON="$TMP/wws2json"
FIX="$TMP/fix"; mkdir -p "$FIX"

# ---------------------------------------------------------------- static checks
step "gofmt (formatting)"
unformatted="$(gofmt -l internal cmd)"
[ -z "$unformatted" ] || fail "unformatted files:\n$unformatted"
ok "ok"

step "go vet"; go vet ./...; ok "ok"

step "go build (three binaries)"
go build -o "$SVG2WWS" ./cmd/svg2wws
go build -o "$WWS2SVG" ./cmd/wws2svg
go build -o "$WWS2JSON" ./cmd/wws2json
ok "ok"

step "go test ./... (unit + end-to-end)"
go test ./...

# --------------------------------------------------------------------- helpers
# expect_ok DESC -- CMD...   : command must exit 0 and not panic
# expect_fail DESC -- CMD... : command must exit non-zero and not panic
expect_ok() {
  local d="$1"; shift; [ "$1" = "--" ] && shift
  local out rc
  if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
  if printf '%s' "$out" | grep -qi 'panic:'; then fail "$d: PANICKED:\n$out"; fi
  [ "$rc" -eq 0 ] || fail "$d: expected success, got exit $rc:\n$out"
  ok "ok: $d"
}
expect_fail() {
  local d="$1"; shift; [ "$1" = "--" ] && shift
  local out rc
  if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
  if printf '%s' "$out" | grep -qi 'panic:'; then fail "$d: PANICKED (must error cleanly):\n$out"; fi
  [ "$rc" -ne 0 ] || fail "$d: expected failure but it succeeded:\n$out"
  ok "ok (rejected cleanly): $d"
}

b64() { python3 -c 'import base64,sys; sys.stdout.buffer.write(base64.b64decode(sys.stdin.buffer.read()))'; }

cat > "$TMP/validate.py" <<'PY'
import json, sys
f = sys.argv[1]
W = float(sys.argv[2]) if len(sys.argv) > 2 else 0.0
H = float(sys.argv[3]) if len(sys.argv) > 3 else 0.0
d = json.load(open(f))
assert d.get("version") == "3.0.4", "version=" + str(d.get("version"))
assert isinstance(d.get("canvasList"), list) and d["canvasList"], "no canvasList"
assert isinstance(d.get("processList"), dict), "no processList"
assert isinstance(d.get("layerDataList"), list), "no layerDataList"
objs = [o for c in d["canvasList"] for o in c.get("objects", [])]
assert objs, "no objects emitted"
ids = [o["id"] for o in objs]
assert len(set(ids)) == len(ids), "duplicate object ids"
for i in ids:
    assert i in d["processList"], "object missing processList entry: " + i
assert len(d["processList"]) == len(objs), \
    "processList %d != objects %d" % (len(d["processList"]), len(objs))
shape_ids = {r["id"] for ld in d["layerDataList"] for r in ld.get("data", []) if r.get("type") == "shape"}
assert shape_ids <= set(ids), "layer shape row references unknown object"
for o in objs:
    assert o.get("processMode") in ("cut", "engrave", "fillEngrave"), "bad processMode " + str(o.get("processMode"))
if W > 0:
    for o in objs:
        sx = o.get("scaleX", 1) or 1
        sy = o.get("scaleY", 1) or 1
        L, T = o["left"], o["top"]
        R, B = L + o["width"] * sx, T + o["height"] * sy
        assert L >= -0.6 and T >= -0.6 and R <= W + 0.6 and B <= H + 0.6, \
            "object out of material: [%.1f,%.1f,%.1f,%.1f] in %gx%g" % (L, T, R, B, W, H)
print("    valid: %d object(s) / %d canvas(es)" % (len(objs), len(d["canvasList"])))
PY
valid_wws() { python3 "$TMP/validate.py" "$@" || fail "invalid .wws: $1"; }

# -------------------------------------------------------------------- fixtures
step "build fixtures (every format, self-contained)"
# Minimal PDF: red-stroked square (cut) + black-filled square (engrave).
echo "JVBERi0xLjcKMSAwIG9iago8PC9UeXBlL0NhdGFsb2cvUGFnZXMgMiAwIFI+PgplbmRvYmoKMiAwIG9iago8PC9UeXBlL1BhZ2VzL0tpZHNbMyAwIFJdL0NvdW50IDE+PgplbmRvYmoKMyAwIG9iago8PC9UeXBlL1BhZ2UvUGFyZW50IDIgMCBSL01lZGlhQm94WzAgMCAyMDAgMjAwXS9SZXNvdXJjZXM8PD4+L0NvbnRlbnRzIDQgMCBSPj4KZW5kb2JqCjQgMCBvYmoKPDwvTGVuZ3RoIDU4Pj4Kc3RyZWFtCjEgMCAwIFJHIDIgdyAyMCAyMCAxNjAgMTYwIHJlIFMgMCAwIDAgcmcgNjAgNjAgODAgODAgcmUgZgplbmRzdHJlYW0KZW5kb2JqCnhyZWYKMCA1CjAwMDAwMDAwMDAgNjU1MzUgZiAKMDAwMDAwMDAwOSAwMDAwMCBuIAowMDAwMDAwMDU0IDAwMDAwIG4gCjAwMDAwMDAxMDUgMDAwMDAgbiAKMDAwMDAwMDE5OSAwMDAwMCBuIAp0cmFpbGVyCjw8L1NpemUgNS9Sb290IDEgMCBSPj4Kc3RhcnR4cmVmCjMwNAolJUVPRgo=" | b64 > "$FIX/min.pdf"
cp "$FIX/min.pdf" "$FIX/min.ai" # PDF-compatible AI uses the same path
# Small PNG (16x12 gray gradient).
echo "iVBORw0KGgoAAAANSUhEUgAAABAAAAAMCAAAAABOjGJdAAAAF0lEQVR4nGJhEEAFTAxoYOgIAAIAAP//w5ABC9+NuMYAAAAASUVORK5CYII=" | b64 > "$FIX/min.png"
cp "$FIX/min.png" "$FIX/min.jpglike" 2>/dev/null || true

# Multi-role SVG: a decorated cut part, a plain cut-only part, and a grouped label.
cat > "$FIX/multi.svg" <<'SVG'
<svg width="200mm" height="200mm" viewBox="0 0 200 200">
  <style>.cut{fill:none;stroke:red}.fil{fill:black}</style>
  <rect class="cut" x="10" y="10" width="70" height="70"/>
  <circle class="fil" cx="45" cy="45" r="12"/>
  <rect class="cut" x="110" y="110" width="60" height="60"/>
  <g id="label"><rect class="fil" x="20" y="120" width="5" height="5"/><rect class="fil" x="28" y="120" width="5" height="5"/></g>
</svg>
SVG
# Cut-only and engrave-only edge SVGs.
printf '%s' '<svg viewBox="0 0 100 100"><rect x="10" y="10" width="50" height="50" style="fill:none;stroke:#e61f19"/></svg>' > "$FIX/cutonly.svg"
printf '%s' '<svg viewBox="0 0 100 100"><circle cx="50" cy="50" r="30" style="fill:black"/></svg>' > "$FIX/engraveonly.svg"
# Malformed / degenerate inputs.
: > "$FIX/empty.svg"
printf '%s' '<svg viewBox="0 0 10 10"></svg>' > "$FIX/nogeo.svg"
printf '%s' 'not xml at all <<<>>> &&&' > "$FIX/garbage.svg"
printf '%s' '<svg viewBox="0 0 10 10"><path d="M z q q q"/></svg>' > "$FIX/badpath.svg"
printf '%s' 'this is plain text' > "$FIX/notes.txt"
printf '%s' 'GARBAGE not a dxf' > "$FIX/garbage.dxf"
printf '%s' '%PDF-1.7 truncated junk' > "$FIX/broken.pdf"
printf '%s' 'not an image' > "$FIX/broken.png"
# DXF: closed square + circle + line + arc.
printf '0\nSECTION\n2\nENTITIES\n0\nLWPOLYLINE\n8\n0\n90\n4\n70\n1\n10\n0\n20\n0\n10\n60\n20\n0\n10\n60\n20\n60\n10\n0\n20\n60\n0\nCIRCLE\n8\n0\n10\n30\n20\n30\n40\n10\n0\nLINE\n8\n0\n10\n0\n20\n0\n11\n60\n21\n60\n0\nARC\n8\n0\n10\n80\n20\n30\n40\n20\n50\n0\n51\n180\n0\nENDSEC\n0\nEOF\n' > "$FIX/shapes.dxf"
# Stress SVG: many small squares forcing multi-sheet spill.
{ echo '<svg width="500mm" height="500mm" viewBox="0 0 500 500">'
  for i in $(seq 0 59); do x=$(( (i % 10) * 48 + 5 )); y=$(( (i / 10) * 48 + 5 ));
    echo "<rect x=\"$x\" y=\"$y\" width=\"40\" height=\"40\" style=\"fill:none;stroke:#e61f19\"/>"; done
  echo '</svg>'; } > "$FIX/many.svg"
ok "ok"

# ----------------------------------------------------- every input format -> wws
step "svg2wws: every input format converts to a valid .wws"
"$SVG2WWS" --in "$FIX/multi.svg"       --material 300x200 --out "$TMP/svg.wws"  >/dev/null; valid_wws "$TMP/svg.wws" 300 200
"$SVG2WWS" --in "$FIX/min.pdf"         --material 300x200 --out "$TMP/pdf.wws"  >/dev/null; valid_wws "$TMP/pdf.wws" 300 200
"$SVG2WWS" --in "$FIX/min.ai"          --material 300x200 --out "$TMP/ai.wws"   >/dev/null; valid_wws "$TMP/ai.wws"  300 200
"$SVG2WWS" --in "$FIX/shapes.dxf"      --material 300x200 --out "$TMP/dxf.wws"  >/dev/null; valid_wws "$TMP/dxf.wws" 300 200
"$SVG2WWS" --in "$FIX/min.png"         --material 300x200 --out "$TMP/png.wws"  >/dev/null; valid_wws "$TMP/png.wws" 300 200
"$SVG2WWS" --in "$FIX/cutonly.svg"     --material 300x200 --out "$TMP/co.wws"   >/dev/null; valid_wws "$TMP/co.wws"  300 200
"$SVG2WWS" --in "$FIX/engraveonly.svg" --material 300x200 --out "$TMP/eo.wws"   >/dev/null; valid_wws "$TMP/eo.wws"  300 200

step "svg2wws: format role assertions"
python3 -c 'import json,collections,sys
d=json.load(open(sys.argv[1])); m=collections.Counter(o["processMode"] for c in d["canvasList"] for o in c["objects"])
assert m["cut"]>=1 and m["fillEngrave"]>=1, "PDF must yield cut+engrave, got "+str(dict(m))' "$TMP/pdf.wws" || fail "PDF role mapping"
python3 -c 'import json,sys
d=json.load(open(sys.argv[1])); o=d["canvasList"][0]["objects"][0]
assert o["type"]=="image" and o["processMode"]=="fillEngrave", "PNG must be a fillEngrave image"' "$TMP/png.wws" || fail "PNG role"
ok "ok (PDF->cut+engrave, PNG->image fillEngrave)"

# ----------------------------------------------------------------- every flag
step "svg2wws: flags"
expect_ok "spacing+margin+grid"  -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --spacing 1 --margin 4 --grid 0.5 --out "$TMP/f1.wws"
expect_ok "rotations count"      -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --rotations 8 --out "$TMP/f2.wws"
expect_ok "rotations list"       -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --rotations 0,90,180 --out "$TMP/f3.wws"
expect_ok "no-rotate"            -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --rotations 1 --out "$TMP/f4.wws"
expect_ok "power/speed/passes"   -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --power 60 --speed 12 --passes 3 --out "$TMP/f5.wws"
expect_ok "scale override"       -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --scale 1 --out "$TMP/f6.wws"
expect_ok "material star sep"    -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300*200 --out "$TMP/f7.wws"
expect_ok "no-engrave-align"     -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --no-engrave-align --out "$TMP/f8.wws"
expect_ok "no-group-engrave"     -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --no-group-engrave --out "$TMP/f9.wws"
# --name lands in output; cut power/passes propagate to processList.
"$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --name ZZTOP --power 60 --passes 3 --out "$TMP/fn.wws" >/dev/null
python3 -c 'import json,sys
d=json.load(open(sys.argv[1])); assert d["name"]=="ZZTOP","name not applied"
cut=[p for p in d["processList"].values() if p["processMode"]=="cut"]
assert cut and cut[0]["cut"]["power"]==60 and cut[0]["cut"]["repeat"]==3,"cut power/passes not applied"' "$TMP/fn.wws" || fail "--name/--power/--passes"
ok "ok (--name, --power, --passes propagate)"
# engrave grouping puts engraving on its own sheet (multi.svg has a cut-only part).
out="$("$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --out "$TMP/fg.wws")"
echo "$out" | grep -q "engraving consolidated" || fail "expected engrave consolidation:\n$out"
ok "ok (engrave consolidated onto its own sheet)"

# ------------------------------------------------------ adversarial / punishment
step "svg2wws: malformed & edge inputs must error cleanly (no panic)"
expect_fail "missing --in"          -- "$SVG2WWS" --material 300x200
expect_fail "missing --material"    -- "$SVG2WWS" --in "$FIX/multi.svg"
expect_fail "nonexistent file"      -- "$SVG2WWS" --in "$FIX/nope.svg" --material 300x200 --out "$TMP/z.wws"
expect_fail "unknown flag"          -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --bogus 1 --out "$TMP/z.wws"
expect_fail "unsupported format"    -- "$SVG2WWS" --in "$FIX/notes.txt" --material 300x200 --out "$TMP/z.wws"
expect_fail "empty svg"             -- "$SVG2WWS" --in "$FIX/empty.svg" --material 300x200 --out "$TMP/z.wws"
expect_fail "svg no geometry"       -- "$SVG2WWS" --in "$FIX/nogeo.svg" --material 300x200 --out "$TMP/z.wws"
expect_fail "garbage svg"           -- "$SVG2WWS" --in "$FIX/garbage.svg" --material 300x200 --out "$TMP/z.wws"
expect_fail "bad path data"         -- "$SVG2WWS" --in "$FIX/badpath.svg" --material 300x200 --out "$TMP/z.wws"
expect_fail "garbage dxf"           -- "$SVG2WWS" --in "$FIX/garbage.dxf" --material 300x200 --out "$TMP/z.wws"
expect_fail "broken pdf"            -- "$SVG2WWS" --in "$FIX/broken.pdf" --material 300x200 --out "$TMP/z.wws"
expect_fail "broken png"            -- "$SVG2WWS" --in "$FIX/broken.png" --material 300x200 --out "$TMP/z.wws"
expect_fail "oversized piece"       -- "$SVG2WWS" --in "$FIX/multi.svg" --material 30x30 --out "$TMP/z.wws"
expect_fail "material 0x0"          -- "$SVG2WWS" --in "$FIX/multi.svg" --material 0x0 --out "$TMP/z.wws"
expect_fail "material negative"     -- "$SVG2WWS" --in "$FIX/multi.svg" --material -10x10 --out "$TMP/z.wws"
expect_fail "material garbage"      -- "$SVG2WWS" --in "$FIX/multi.svg" --material abc --out "$TMP/z.wws"
expect_fail "grid 0"                -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --grid 0 --out "$TMP/z.wws"
expect_fail "grid negative"         -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --grid -1 --out "$TMP/z.wws"
expect_fail "spacing negative"      -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --spacing -2 --out "$TMP/z.wws"
expect_fail "margin negative"       -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --margin -2 --out "$TMP/z.wws"
expect_fail "margin too big"        -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --margin 200 --out "$TMP/z.wws"
expect_fail "rotations 0"           -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --rotations 0 --out "$TMP/z.wws"
expect_fail "rotations garbage"     -- "$SVG2WWS" --in "$FIX/multi.svg" --material 300x200 --rotations xyz --out "$TMP/z.wws"

# ----------------------------------------------------------------------- stress
step "svg2wws: stress"
out="$("$SVG2WWS" --in "$FIX/many.svg" --material 150x150 --out "$TMP/s1.wws")"
echo "$out" | grep -qE 'onto [2-9][0-9]* sheet' || fail "60-piece job should spill onto >1 sheet:\n$out"
valid_wws "$TMP/s1.wws" 150 150
ok "ok (60 pieces -> $(echo "$out" | grep -oE 'onto [0-9]+ sheet'))"
expect_ok "6x6 tiny material"   -- "$SVG2WWS" --in "$FIX/many.svg" --material 152.4x152.4 --out "$TMP/s2.wws"
valid_wws "$TMP/s2.wws" 152.4 152.4
expect_ok "many rotations (32)" -- "$SVG2WWS" --in "$FIX/many.svg" --material 500x500 --rotations 32 --out "$TMP/s3.wws"
valid_wws "$TMP/s3.wws" 500 500

# ------------------------------------------------------------ output integrity
step "round-trip: svg2wws -> wws2svg -> wws2json"
"$SVG2WWS" --in samples/test-parts.svg --material 300x200 --rotations 1 --out "$TMP/rt.wws" >/dev/null
"$WWS2SVG" --in "$TMP/rt.wws" --out "$TMP/rtsvg" >/dev/null
ls "$TMP/rtsvg"/*.svg >/dev/null 2>&1 || fail "wws2svg produced no SVG"
grep -q "<svg" "$TMP/rtsvg/"*.svg || fail "wws2svg output is not an SVG"
n=$(grep -o "<path" "$TMP/rtsvg/"*.svg | wc -l | tr -d ' ')
[ "$n" -eq 5 ] || fail "round-trip path count = $n, want 5"
"$WWS2JSON" --in "$TMP/rt.wws" --out "$TMP/rt.json" >/dev/null
python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$TMP/rt.json" || fail "wws2json output invalid"
ok "ok (5 paths survive the round trip; JSON valid)"

step "wws2svg / wws2json: known-good sample"
"$WWS2SVG"  --in samples/square-100.known-good.wws --out "$TMP/k.svg"  >/dev/null
grep -q "<rect" "$TMP/k.svg/"*.svg || fail "expected a rect in the known-good SVG"
"$WWS2JSON" --in samples/square-100.known-good.wws --out "$TMP/k.json" >/dev/null
grep -q '"units"' "$TMP/k.json" || fail "wws2json missing fields"
ok "ok"

step "wws2svg / wws2json: bad input must error cleanly (no panic)"
printf '%s' 'not json' > "$FIX/bad.wws"
expect_fail "wws2svg nonexistent"  -- "$WWS2SVG"  --in "$FIX/nope.wws" --out "$TMP/q"
expect_fail "wws2json nonexistent" -- "$WWS2JSON" --in "$FIX/nope.wws" --out "$TMP/q.json"
expect_fail "wws2svg garbage"      -- "$WWS2SVG"  --in "$FIX/bad.wws"  --out "$TMP/q"
expect_fail "wws2json garbage"     -- "$WWS2JSON" --in "$FIX/bad.wws"  --out "$TMP/q.json"

# --------------------------------------------- optional: full real-library sweep
WWS_LIB="${WWS_LIB:-$HOME/cups/WWS}"
if [ -d "$WWS_LIB" ]; then
  step "wws2svg: batch sweep of $WWS_LIB"
  sweep="$TMP/sweep"
  if ! res="$("$WWS2SVG" --in "$WWS_LIB" --out "$sweep" 2>&1)"; then echo "$res"; fail "wws2svg sweep failed"; fi
  echo "  $res"
  step "validate swept SVGs are well-formed XML"
  python3 - "$sweep" <<'PY'
import sys, glob, os, xml.etree.ElementTree as ET
d=sys.argv[1]; bad=0; n=0
for f in glob.glob(os.path.join(d,"*.svg")):
    n+=1
    try: ET.parse(f)
    except Exception as e: bad+=1; print("  malformed:", os.path.basename(f), e)
print(f"  parsed {n} SVGs, malformed: {bad}"); sys.exit(1 if bad else 0)
PY
  step "wws2json: batch sweep of $WWS_LIB"
  jsweep="$TMP/jsweep"
  if ! jres="$("$WWS2JSON" --in "$WWS_LIB" --out "$jsweep" --strip-images 2>&1)"; then echo "$jres"; fail "wws2json sweep failed"; fi
  echo "  $jres"
  step "validate swept JSON parses"
  python3 - "$jsweep" <<'PY'
import sys, glob, os, json
d=sys.argv[1]; bad=0; n=0
for f in glob.glob(os.path.join(d,"*.json")):
    n+=1
    try: json.load(open(f))
    except Exception as e: bad+=1; print("  invalid:", os.path.basename(f), e)
print(f"  parsed {n} JSON files, invalid: {bad}"); sys.exit(1 if bad else 0)
PY
else
  step "library sweep skipped"
  echo "  ${DIM}set WWS_LIB=/path/to/wws-folder to enable the full sweep${OFF}"
fi

printf '\n%sALL CHECKS PASSED%s\n' "$GREEN" "$OFF"
