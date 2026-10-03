#!/usr/bin/env bash
# Vendors the k9s backend packages (client, dao, watch, render, ...) into
# third_party/k9s with import paths rewritten so they become importable.
# UI-only and heavy packages (tview views, grype vuln scanner) are excluded
# or replaced by files from third_party/k9s-overlay.
set -euo pipefail

K9S_REF="${K9S_REF:-v0.51.0}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DST="$ROOT/third_party/k9s"
OVERLAY="$ROOT/third_party/_k9s-overlay" # "_" prefix: ignored by go build
MOD="github.com/giovanni/k9desk/third_party/k9s"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

git -c advice.detachedHead=false clone -q --depth 1 --branch "$K9S_REF" https://github.com/derailed/k9s.git "$TMP/k9s"
SRC="$TMP/k9s/internal"

PKGS=(client config dao watch render model model1 xray slogs port health color vul view/cmd)

rm -rf "$DST"
mkdir -p "$DST"
# root package "internal" (helpers, keys, pool)
find "$SRC" -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cp {} "$DST/" \;
for p in "${PKGS[@]}"; do
  mkdir -p "$DST/$p"
  (cd "$SRC/$p" && find . -type f ! -name '*_test.go' ! -path '*/testdata/*' ! -path './mock/*' -print0 |
    xargs -0 -I{} cp --parents {} "$DST/$p/")
done

# overlays replace upstream files (e.g. grype-free vul scanner)
(cd "$OVERLAY" && find . -type f -print0 | xargs -0 -I{} cp --parents {} "$DST/")

# rewrite imports
grep -rl 'github.com/derailed/k9s/internal' "$DST" --include='*.go' |
  xargs sed -i "s#github.com/derailed/k9s/internal#$MOD#g"

cp "$TMP/k9s/LICENSE" "$DST/LICENSE"
echo "$K9S_REF $(git -C "$TMP/k9s" rev-parse HEAD)" > "$DST/UPSTREAM"
echo "vendored k9s $K9S_REF into $DST"
