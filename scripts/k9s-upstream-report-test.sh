#!/usr/bin/env bash
# End-to-end tests of scripts/k9s-upstream-report.sh: the vendored version
# is compared with itself while a stage hook simulates each kind of upstream
# change, and the verdict, patch statuses and API findings are checked.
# Needs network once (one k9s clone); takes a few minutes.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FROM_TAG="$(cut -d' ' -f1 "$ROOT/third_party/k9s/UPSTREAM")"
PATCH="$ROOT/third_party/_k9s-patches/kubeon-fixes.patch"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git clone -q https://github.com/derailed/k9s.git "$TMP/k9s"

fails=0
run() { # name hook expect... (each expect: a fixed string the report must contain)
  local name="$1" hook="$2"
  shift 2
  local out="$TMP/$name.md"
  if ! K9S_REPORT_SRC="$TMP/k9s" K9S_REPORT_STAGE_HOOK="$hook" \
    "$ROOT/scripts/k9s-upstream-report.sh" "$FROM_TAG" >"$out" 2>"$TMP/$name.err"; then
    echo "FAIL $name: report exited with an error"
    sed 's/^/    /' "$TMP/$name.err"
    fails=$((fails + 1))
    return
  fi
  local ok=1 e
  for e in "$@"; do
    if ! grep -qF -- "$e" "$out"; then
      echo "FAIL $name: missing «$e»"
      ok=0
    fi
  done
  if [[ $ok == 1 ]]; then
    echo "ok   $name"
  else
    fails=$((fails + 1))
    echo "    report: $out"
    cp "$out" "$ROOT/.dev/k9s-report-$name.md" 2>/dev/null || true
  fi
}

run baseline "" \
  "<!-- verdict: ok -->" "3 clean · 0 already upstream · 0 fuzzy · 0 conflicts" \
  "**0 affecting kubeon** · 0 elsewhere"

# the line right above kubeon's insertion changed: strict apply must refuse
run conflict "sed -i 's/^\t\topts.DefaultContainer = co$/\t\topts.DefaultContainer = strings.TrimSpace(co)/' dao/pod.go" \
  "<!-- verdict: fail -->" "| \`kubeon-fixes.patch\` | \`dao/pod.go\` | ❌ **conflict**" "Hunk #1 FAILED"

# an outer context line changed: only a fuzzy apply would succeed
run fuzzy "sed -i 's/^\touts := make(\[\]LogChan, 0, coCounts)$/\touts := make([]LogChan, 0, coCounts+0)/' dao/pod.go" \
  "<!-- verdict: warn -->" "| \`kubeon-fixes.patch\` | \`dao/pod.go\` | ⚠️ applies only with fuzz" "with fuzz 1"

# upstream fixed it the same way: the patch reverse-applies
run upstream "patch -s -p1 --fuzz=0 < '$PATCH'" \
  "<!-- verdict: warn -->" "0 clean · 3 already upstream" "| \`kubeon-fixes.patch\` | \`model/tree.go\` | ⚠️ **already upstream**"

# a function kubeon calls changes signature
run api-used "sed -i 's/^func IsCRD(r \*metav1.APIResource) bool {/func IsCRD(r *metav1.APIResource, strict bool) bool {/' dao/registry.go && sed -i 's/IsCRD(meta)/IsCRD(meta, false)/' dao/alias.go" \
  "<!-- verdict: fail -->" "**1 affecting kubeon**" "| \`dao\` | \`IsCRD\` | changed from"

# an interface kubeon uses gains a method: k9s' own implementations stop
# compiling, and the report says which method is missing
run iface "sed -i 's/^type Loggable interface {$/type Loggable interface {\n\tLogStats() int/' dao/types.go" \
  "<!-- verdict: fail -->" "| \`dao\` | \`(package)\` | does not build" "missing method LogStats"

# a change kubeon doesn't care about stays informational (an exported
# function nothing calls)
run api-unused "sed -i 's/^func NewTableDataFull(/func NewTableDataComplete(/' model1/table_data.go" \
  "<!-- verdict: ok -->" "**0 affecting kubeon** · 1 elsewhere" "| \`model1\` | \`NewTableDataFull\` | removed |"

# a package kubeon uses stops building (an internal caller broke)
run pkg-broken "sed -i 's/^func (r \*RowEvents) Delete(/func (r *RowEvents) DeleteRow(/' model1/row_event.go" \
  "<!-- verdict: fail -->" "| \`model1\` | \`(package)\` | does not build"

# a whole package kubeon uses disappears
run pkg-removed "rm -rf render/helm" \
  "<!-- verdict: fail -->" "| \`render/helm\` | \`(package)\` | removed |"

# a new dependency that can't be resolved
run bad-dep "printf 'package internal\n\nimport _ \"example.invalid/nope\"\n' > zz_bad.go" \
  "<!-- verdict: fail -->" "| \`.\` | \`(package)\` | does not build"

if [[ $fails -gt 0 ]]; then
  echo "$fails scenario(s) failed"
  exit 1
fi
echo "all scenarios passed"
