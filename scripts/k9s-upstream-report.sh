#!/usr/bin/env bash
# Markdown report on moving the vendored k9s (third_party/k9s/UPSTREAM) to
# TO (a tag or branch; default: the latest release):
#
#   1. verdict: ✅ safe · ⚠️ needs attention · ❌ will break
#   2. kubeon's patches, file by file, dry-run against TO exactly as
#      vendor-k9s.sh applies them (no fuzz): clean / already upstream (drop
#      it) / only with fuzz (review) / conflict
#   3. incompatible API changes (apidiff) in the vendored packages, split
#      into those touching what kubeon uses (scripts/k9suse) and the rest
#   4. release notes, commits and diffstat of the vendored packages, files
#      kubeon overlays that changed upstream
#
# The last line is a machine-readable marker: <!-- verdict: ok|warn|fail -->
#
#   scripts/k9s-upstream-report.sh            # vs latest release
#   scripts/k9s-upstream-report.sh master     # vs upstream master
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO=derailed/k9s
MOD=github.com/giovanni/kubeon/third_party/k9s
APIDIFF=golang.org/x/exp/cmd/apidiff@v0.0.0-20260410095643-746e56fc9e2f
FROM_TAG="$(cut -d' ' -f1 "$ROOT/third_party/k9s/UPSTREAM")"
FROM_SHA="$(cut -d' ' -f2 "$ROOT/third_party/k9s/UPSTREAM")"
TO="${1:-$(gh api "repos/$REPO/releases/latest" -q .tag_name)}"

# vendored packages, from the vendoring script ("." = the root package)
eval "$(grep '^PKGS=' "$ROOT/scripts/vendor-k9s.sh")"
PATHS=()
# shellcheck disable=SC2153 # PKGS comes from the eval above
for p in "${PKGS[@]}"; do PATHS+=("internal/$p"); done

FENCE='```'
TMP="$(mktemp -d)"
WT="$TMP/new"
cleanup() {
  git -C "$ROOT" worktree remove --force "$WT" >/dev/null 2>&1 || true
  git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

# ---- upstream checkout and staging copy (vendored layout, no patches) ----
# Test seams (scripts/k9s-upstream-report-test.sh): K9S_REPORT_SRC reuses a
# full k9s clone; K9S_REPORT_STAGE_HOOK alters the staged copy (run in it).
if [[ -n "${K9S_REPORT_SRC:-}" ]]; then
  git clone -q "$K9S_REPORT_SRC" "$TMP/k9s"
else
  git clone -q --filter=blob:none "https://github.com/$REPO.git" "$TMP/k9s"
  git -C "$TMP/k9s" fetch -q --tags origin
fi
# a branch (origin/<TO>) or a tag/sha; --verify prints nothing on a miss
TO_SHA="$(git -C "$TMP/k9s" rev-parse --verify -q "origin/$TO^{commit}" ||
  git -C "$TMP/k9s" rev-parse --verify -q "$TO^{commit}")" || {
  echo "k9s-upstream-report: unknown k9s ref $TO" >&2
  exit 1
}
git -C "$TMP/k9s" -c advice.detachedHead=false checkout -q "$TO_SHA"
K9S_REF="$TO" "$ROOT/scripts/vendor-k9s.sh" --src "$TMP/k9s" --dst "$TMP/stage" --no-patches >/dev/null
if [[ -n "${K9S_REPORT_STAGE_HOOK:-}" ]]; then
  (cd "$TMP/stage" && bash -euc "$K9S_REPORT_STAGE_HOOK")
fi
TOTAL="$(git -C "$TMP/k9s" rev-list --count "$FROM_SHA..$TO_SHA")"

# ---- 1. patches, file by file ----
clean=0 upstream=0 fuzz=0 conflict=0
PATCH_ROWS="$TMP/patch-rows.md"
PATCH_LOGS="$TMP/patch-logs.md"
: >"$PATCH_ROWS"
: >"$PATCH_LOGS"
mkdir -p "$TMP/sec"
for p in "$ROOT"/third_party/_k9s-patches/*.patch; do
  base="$(basename "$p" .patch)"
  awk -v dir="$TMP/sec" -v base="$base" '
    /^diff --git / { n++; f = sprintf("%s/%s.%03d.patch", dir, base, n) }
    f { print > f }' "$p"
done
dry=(patch -d "$TMP/stage" -p1 --dry-run --batch --no-backup-if-mismatch --reject-file=-)
for s in "$TMP"/sec/*.patch; do
  [[ -e "$s" ]] || continue
  name="$(basename "$s" | sed 's/\.[0-9]*\.patch$/.patch/')"
  target="$(sed -n 's#^+++ b/\([^[:space:]]*\).*#\1#p' "$s" | head -1)"
  if out="$("${dry[@]}" --forward --fuzz=0 <"$s" 2>&1)"; then
    status="✅ applies cleanly"
    clean=$((clean + 1))
  elif "${dry[@]}" --reverse --fuzz=0 <"$s" >/dev/null 2>&1; then
    status="⚠️ **already upstream**: drop this hunk from the patch"
    upstream=$((upstream + 1))
  elif out="$("${dry[@]}" --forward <"$s" 2>&1)"; then
    status="⚠️ applies only with fuzz: the context moved, review and regenerate"
    fuzz=$((fuzz + 1))
    printf '\n<details><summary>%s · %s</summary>\n\n%s\n%s\n%s\n\n</details>\n' "$name" "$target" "$FENCE" "$out" "$FENCE" >>"$PATCH_LOGS"
  else
    status="❌ **conflict**: rework the fix for the new code"
    conflict=$((conflict + 1))
    printf '\n<details><summary>%s · %s</summary>\n\n%s\n%s\n%s\n\n</details>\n' "$name" "$target" "$FENCE" "$out" "$FENCE" >>"$PATCH_LOGS"
  fi
  echo "| \`$name\` | \`$target\` | $status |" >>"$PATCH_ROWS"
done

# overlays replace whole upstream files
OVERLAY_ROWS="$TMP/overlay-rows.md"
: >"$OVERLAY_ROWS"
overlay_changed=0
while read -r f; do
  f="${f#./}"
  if ! git -C "$TMP/k9s" cat-file -e "$TO_SHA:internal/$f" 2>/dev/null; then
    echo "- ⚠️ \`$f\`: removed upstream (overlay may be obsolete)" >>"$OVERLAY_ROWS"
    overlay_changed=$((overlay_changed + 1))
  elif ! git -C "$TMP/k9s" diff --quiet "$FROM_SHA" "$TO_SHA" -- "internal/$f"; then
    echo "- ⚠️ \`$f\`: changed upstream, the overlay replaces it: port the changes if needed" >>"$OVERLAY_ROWS"
    overlay_changed=$((overlay_changed + 1))
  fi
done < <(cd "$ROOT/third_party/_k9s-overlay" && find . -type f -name '*.go' | sort)

# used PKG PREFIX: kubeon uses a symbol of PKG equal to PREFIX, or starting
# with it when PREFIX ends with "." (any member); "" = anything in PKG
used() {
  awk -F'\t' -v p="$1" -v n="$2" '
    $1 == p && (n == "" || $2 == n || (substr(n, length(n)) == "." && index($2, n) == 1)) { f = 1 }
    END { exit !f }' "$TMP/used.txt"
}

# ---- 2. API compatibility ----
# every Go package of the vendored tree, old and new (render/helm, …)
gopkgs() { (cd "$1" && find . -name '*.go' -printf '%h\n' | sed 's#^\./##; s#^\.$#.#' | sort -u); }
mapfile -t APKGS < <(sort -u <(gopkgs "$ROOT/third_party/k9s") <(gopkgs "$TMP/stage"))
mkdir -p "$TMP/bin" "$TMP/old"
GOBIN="$TMP/bin" go install "$APIDIFF" 2>/dev/null
(cd "$ROOT" && go run ./scripts/k9suse) >"$TMP/used.txt"
for p in "${APKGS[@]}"; do
  ip="$MOD"
  [[ $p == . ]] || ip="$MOD/$p"
  (cd "$ROOT" && "$TMP/bin/apidiff" -w "$TMP/old/${p//\//_}.api" "$ip" 2>/dev/null) || true
done
git -C "$ROOT" worktree add -q --detach "$WT" HEAD
rm -rf "$WT/third_party/k9s"
cp -r "$TMP/stage" "$WT/third_party/k9s"
tidy_err=""
# -e: keep going past missing packages, so apidiff can say which went away
if ! tidy_out="$(cd "$WT" && go mod tidy -e 2>&1)"; then
  tidy_err="$tidy_out"
fi
affected=0 other=0
API_HIT="$TMP/api-hit.md"
API_OTHER="$TMP/api-other.md"
: >"$API_HIT"
: >"$API_OTHER"
if [[ -z "$tidy_err" ]]; then
  for p in "${APKGS[@]}"; do
    old="$TMP/old/${p//\//_}.api"
    [[ -s "$old" ]] || continue # new upstream package: nothing to break
    ip="$MOD"
    [[ $p == . ]] || ip="$MOD/$p"
    if ! compgen -G "$WT/third_party/k9s/$p/*.go" >/dev/null; then
      lines="- (package): removed"
    elif ! lines="$(cd "$WT" && "$TMP/bin/apidiff" -incompatible "$old" "$ip" 2>&1)"; then
      lines="- (package): does not build: ${lines//$'\n'/ }"
    fi
    while IFS= read -r l; do
      [[ "$l" == "- "* ]] || continue
      subj="${l#- }"
      subj="${subj%%: *}"
      hit=0
      if [[ "$subj" == "(package)" ]]; then # removed or no longer builds
        if used "$p" ""; then hit=1; fi
      else
        norm="$(sed -E 's/^\(\*?([^)]*)\)/\1/' <<<"$subj")" # (*T).M → T.M
        if used "$p" "$norm" ||
          { [[ "$norm" != *.* ]] && used "$p" "$norm."; }; then # T changed: kubeon uses T or T.member
          hit=1
        fi
      fi
      row="| \`$p\` | \`$subj\` | ${l#*: } |"
      if [[ $hit == 1 ]]; then
        echo "$row" >>"$API_HIT"
        affected=$((affected + 1))
      else
        echo "$row" >>"$API_OTHER"
        other=$((other + 1))
      fi
    done <<<"$lines"
  done
fi

# ---- verdict ----
if ((conflict > 0 || affected > 0)) || [[ -n "$tidy_err" ]]; then
  verdict=fail
  headline="❌ **Will break**: kubeon's patches or code need changes before this update."
elif ((upstream > 0 || fuzz > 0 || overlay_changed > 0)); then
  verdict=warn
  headline="⚠️ **Needs attention**: no breakage expected, but patches or overlays need a look."
else
  verdict=ok
  headline="✅ **Looks safe**: patches apply cleanly and no API kubeon uses changed incompatibly."
fi

# ---- report ----
echo "# k9s upstream: $FROM_TAG → $TO"
echo
echo "Vendored \`$FROM_TAG\` (\`${FROM_SHA:0:12}\`) · target \`$TO\` (\`${TO_SHA:0:12}\`) · $TOTAL upstream commits."
echo
echo "$headline"
echo
echo "| check | result |"
echo "|---|---|"
echo "| kubeon patches (file by file, no fuzz) | $clean clean · $upstream already upstream · $fuzz fuzzy · $conflict conflicts |"
if [[ -n "$tidy_err" ]]; then
  echo "| dependencies (\`go mod tidy\`) | ❌ failed |"
else
  echo "| incompatible API changes | **$affected affecting kubeon** · $other elsewhere |"
fi
echo "| overlay files changed upstream | $overlay_changed |"
echo
echo "## kubeon patches"
echo
echo "| patch | file | status |"
echo "|---|---|---|"
cat "$PATCH_ROWS"
cat "$PATCH_LOGS"
if [[ -s "$OVERLAY_ROWS" ]]; then
  echo
  echo "Overlays (\`third_party/_k9s-overlay\`):"
  echo
  cat "$OVERLAY_ROWS"
fi
echo
echo "## API changes"
echo
if [[ -n "$tidy_err" ]]; then
  echo "\`go mod tidy\` failed with the new k9s, so the API could not be compared:"
  printf '\n%s\n%s\n%s\n' "$FENCE" "$(head -c 3000 <<<"$tidy_err")" "$FENCE"
else
  echo "Incompatible changes reported by [apidiff](https://pkg.go.dev/golang.org/x/exp/cmd/apidiff) between the vendored and the target packages."
  echo
  echo "### Affecting kubeon ($affected)"
  echo
  if [[ -s "$API_HIT" ]]; then
    echo "Symbols \`internal/backend\` uses (see \`go run ./scripts/k9suse\`): fix these call sites when updating."
    echo
    echo "| package | symbol | change |"
    echo "|---|---|---|"
    cat "$API_HIT"
  else
    echo "_None._"
  fi
  echo
  echo "<details><summary>Other incompatible changes ($other): not used by kubeon</summary>"
  echo
  if [[ -s "$API_OTHER" ]]; then
    echo "| package | symbol | change |"
    echo "|---|---|---|"
    cat "$API_OTHER"
  else
    echo "_None._"
  fi
  echo
  echo "</details>"
fi
echo

cd "$TMP/k9s"
echo "## Releases"
echo
from_date="$(git log -1 --format=%cI "$FROM_SHA")"
rel="$(gh api "repos/$REPO/releases?per_page=30" | jq -r --arg d "$from_date" --arg from "$FROM_TAG" \
  '[.[] | select(.draft|not) | select(.tag_name != $from and .published_at > $d)] | sort_by(.published_at) | .[] |
   "### [\(.tag_name)](\(.html_url)) — \(.published_at[:10])\n\n<details><summary>release notes</summary>\n\n\(.body // "" | .[0:6000])\n\n</details>\n"')"
if [[ -n "$rel" ]]; then echo "$rel"; else echo "_No release after $FROM_TAG._"; fi
echo

echo "## Commits touching vendored packages"
echo
fmt="- [\`%h\`](https://github.com/$REPO/commit/%H) %s (%an, %as)"
commits="$(git log --format="$fmt" "$FROM_SHA..$TO_SHA" -- "${PATHS[@]}")"
if [[ -n "$commits" ]]; then
  echo "$(wc -l <<<"$commits") of $TOTAL commits:"
  echo
  echo "$commits"
else
  echo "_None: the vendored packages are unchanged._"
fi
echo

echo "## Changes per package"
echo
echo '| package | files | + | − |'
echo '|---|---:|---:|---:|'
any=0
for p in "${PATHS[@]}"; do
  read -r files add del < <(git diff --numstat "$FROM_SHA" "$TO_SHA" -- "$p" |
    awk '$3 !~ /_test\.go$/ {f++; a+=$1; d+=$2} END {print f+0, a+0, d+0}')
  if [[ "$files" != 0 ]]; then
    echo "| \`${p#internal/}\` | $files | $add | $del |"
    any=1
  fi
done
[[ $any == 1 ]] || echo '| — | 0 | 0 | 0 |'
echo
echo "<!-- verdict: $verdict -->"
