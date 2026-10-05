#!/usr/bin/env bash
# Vendors the k9s backend packages (client, dao, watch, render, ...) into
# third_party/k9s with import paths rewritten so they become importable.
# UI-only and heavy packages (tview views, grype vuln scanner) are excluded
# or replaced by files from third_party/_k9s-overlay, then kubeon's fixes in
# third_party/_k9s-patches are applied strictly (no fuzz: a patch that no
# longer matches exactly fails instead of landing in the wrong place).
#
#   K9S_REF=v0.52.0 scripts/vendor-k9s.sh       # default: version in UPSTREAM
#   scripts/vendor-k9s.sh --dst DIR --no-patches  # staging copy (analysis)
#   scripts/vendor-k9s.sh --src CHECKOUT          # use an existing k9s checkout
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DST="$ROOT/third_party/k9s"
SRCREPO=""
PATCHES=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dst) DST="$2"; shift 2 ;;
    --src) SRCREPO="$2"; shift 2 ;;
    --no-patches) PATCHES=0; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
# default: re-vendor the version recorded in third_party/k9s/UPSTREAM
K9S_REF="${K9S_REF:-$(cut -d' ' -f1 "$ROOT/third_party/k9s/UPSTREAM")}"
OVERLAY="$ROOT/third_party/_k9s-overlay" # "_" prefix: ignored by go build
MOD="github.com/giovanni/kubeon/third_party/k9s"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [[ -z "$SRCREPO" ]]; then
  git -c advice.detachedHead=false clone -q --depth 1 --branch "$K9S_REF" https://github.com/derailed/k9s.git "$TMP/k9s"
  SRCREPO="$TMP/k9s"
fi
SRC="$SRCREPO/internal"

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

# kubeon fixes (paths relative to third_party/k9s, imports already rewritten)
if [[ $PATCHES == 1 ]]; then
  for p in "$ROOT"/third_party/_k9s-patches/*.patch; do
    if ! patch -d "$DST" -p1 --forward --fuzz=0 --batch --no-backup-if-mismatch --reject-file=- < "$p"; then
      echo "vendor-k9s: $(basename "$p") no longer applies to k9s $K9S_REF (see scripts/k9s-upstream-report.sh)" >&2
      exit 1
    fi
  done
fi

cp "$SRCREPO/LICENSE" "$DST/LICENSE"
echo "$K9S_REF $(git -C "$SRCREPO" rev-parse HEAD)" > "$DST/UPSTREAM"
echo "vendored k9s $K9S_REF into $DST"
