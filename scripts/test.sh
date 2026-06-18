#!/usr/bin/env bash
#
# Pre-push test runner for wws-generator. Exercises every feature of both
# converters and exits non-zero on the first failure. Run this before pushing.
#
#   ./scripts/test.sh
#
# Optional: point WWS_LIB at a folder of real .wws files for a full library
# sweep (defaults to ~/Desktop/cups/WWS if present; skipped otherwise).

set -euo pipefail
cd "$(dirname "$0")/.."

GREEN=$'\033[32m'; RED=$'\033[31m'; DIM=$'\033[2m'; OFF=$'\033[0m'
step() { printf '\n%s==> %s%s\n' "$GREEN" "$1" "$OFF"; }
fail() { printf '%s!! %s%s\n' "$RED" "$1" "$OFF"; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SVG2WWS="$TMP/svg2wws"
WWS2SVG="$TMP/wws2svg"
WWS2JSON="$TMP/wws2json"

# ---------------------------------------------------------------- static checks
step "gofmt (formatting)"
unformatted="$(gofmt -l internal cmd)"
[ -z "$unformatted" ] || fail "unformatted files:\n$unformatted"
echo "  ok"

step "go vet"
go vet ./...
echo "  ok"

step "go build (both binaries)"
go build -o "$SVG2WWS" ./cmd/svg2wws
go build -o "$WWS2SVG" ./cmd/wws2svg
go build -o "$WWS2JSON" ./cmd/wws2json
echo "  ok"

# --------------------------------------------------- unit + end-to-end go tests
step "go test ./... (parse, arc, nesting, transforms, round-trip)"
go test ./...

# ----------------------------------------------------- svg2wws CLI smoke checks
step "svg2wws: default convert"
"$SVG2WWS" --in samples/test-parts.svg --material 300x200 --out "$TMP/a.wws" >/dev/null
[ -s "$TMP/a.wws" ] || fail "no output written"
echo "  ok"

step "svg2wws: rotation + grid variants"
"$SVG2WWS" --in samples/test-parts.svg --material 300x200 --rotations 1 --out "$TMP/b.wws" >/dev/null
"$SVG2WWS" --in samples/test-parts.svg --material 300x200 --rotations 0,90 --grid 0.5 --out "$TMP/c.wws" >/dev/null
echo "  ok"

step "svg2wws: multi-sheet spill (small material)"
out="$("$SVG2WWS" --in samples/test-parts.svg --material 120x120 --out "$TMP/d.wws")"
echo "$out" | grep -qE 'onto [2-9][0-9]* sheet' || fail "expected spill onto >1 sheet:\n$out"
echo "  ok ($(echo "$out" | grep -oE 'onto [0-9]+ sheet'))"

step "svg2wws: oversized piece must error (exit 1)"
if "$SVG2WWS" --in samples/test-parts.svg --material 50x50 --out "$TMP/x.wws" >/dev/null 2>&1; then
  fail "oversized piece did not error"
fi
echo "  ok (errored as expected)"

# ----------------------------------------------------- wws2svg CLI smoke checks
step "wws2svg: convert a sample .wws"
"$WWS2SVG" --in samples/square-100.known-good.wws --out "$TMP/svg" >/dev/null
ls "$TMP/svg"/*.svg >/dev/null 2>&1 || fail "no SVG written"
grep -q "<svg" "$TMP/svg/"*.svg || fail "output is not an SVG"
echo "  ok"

step "wws2json: convert a sample .wws to detailed JSON"
"$WWS2JSON" --in samples/square-100.known-good.wws --out "$TMP/s.json" >/dev/null
grep -q '"units"' "$TMP/s.json" || fail "JSON missing expected fields"
if command -v python3 >/dev/null 2>&1; then
  python3 -c "import json,sys; json.load(open('$TMP/s.json'))" || fail "invalid JSON"
fi
echo "  ok"

# --------------------------------------------- optional: full real-library sweep
WWS_LIB="${WWS_LIB:-$HOME/Desktop/cups/WWS}"
if [ -d "$WWS_LIB" ]; then
  step "wws2svg: batch sweep of $WWS_LIB"
  sweep="$TMP/sweep"
  res="$("$WWS2SVG" --in "$WWS_LIB" --out "$sweep")"
  echo "  $res"
  echo "$res" | grep -q "failed" && fail "some files failed to convert"
  if command -v python3 >/dev/null 2>&1; then
    step "validate swept SVGs are well-formed XML"
    python3 - "$sweep" <<'PY'
import sys, glob, os, xml.etree.ElementTree as ET
d=sys.argv[1]; bad=0; n=0
for f in glob.glob(os.path.join(d,"*.svg")):
    n+=1
    try: ET.parse(f)
    except Exception as e: bad+=1; print("  malformed:", os.path.basename(f), e)
print(f"  parsed {n} SVGs, malformed: {bad}")
sys.exit(1 if bad else 0)
PY
  fi

  step "wws2json: batch sweep of $WWS_LIB"
  jsweep="$TMP/jsweep"
  jres="$("$WWS2JSON" --in "$WWS_LIB" --out "$jsweep" --strip-images)"
  echo "  $jres"
  echo "$jres" | grep -q "failed" && fail "some files failed to convert to JSON"
  if command -v python3 >/dev/null 2>&1; then
    step "validate swept JSON parses"
    python3 - "$jsweep" <<'PY'
import sys, glob, os, json
d=sys.argv[1]; bad=0; n=0
for f in glob.glob(os.path.join(d,"*.json")):
    n+=1
    try: json.load(open(f))
    except Exception as e: bad+=1; print("  invalid:", os.path.basename(f), e)
print(f"  parsed {n} JSON files, invalid: {bad}")
sys.exit(1 if bad else 0)
PY
  fi
else
  step "library sweep skipped"
  echo "  ${DIM}set WWS_LIB=/path/to/wws-folder to enable the full sweep${OFF}"
fi

printf '\n%sALL CHECKS PASSED%s\n' "$GREEN" "$OFF"
