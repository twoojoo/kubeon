#!/usr/bin/env bash
# Markdown report of what changed in k9s between the vendored version
# (third_party/k9s/UPSTREAM) and TO (a tag or branch, default: latest
# release), limited to the packages kubeon vendors: release notes, commits,
# per-package diffstat and the files kubeon patches or overlays (merge risk).
#
#   scripts/k9s-upstream-report.sh            # vs latest release
#   scripts/k9s-upstream-report.sh master     # vs upstream master
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO=derailed/k9s
FROM_TAG="$(cut -d' ' -f1 "$ROOT/third_party/k9s/UPSTREAM")"
FROM_SHA="$(cut -d' ' -f2 "$ROOT/third_party/k9s/UPSTREAM")"
TO="${1:-$(gh api "repos/$REPO/releases/latest" -q .tag_name)}"

# vendored packages, from the vendoring script
eval "$(grep '^PKGS=' "$ROOT/scripts/vendor-k9s.sh")"
PATHS=()
for p in "${PKGS[@]}"; do PATHS+=("internal/$p"); done
# kubeon patches and overlays, as upstream paths
RISKY=()
while read -r f; do RISKY+=("internal/${f#b/}"); done < <(sed -n 's/^+++ \(b\/[^\t ]*\).*/\1/p' "$ROOT"/third_party/_k9s-patches/*.patch)
while read -r f; do RISKY+=("internal/${f#./}"); done < <(cd "$ROOT/third_party/_k9s-overlay" && find . -type f)

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git clone -q --filter=blob:none --no-checkout "https://github.com/$REPO.git" "$TMP/k9s"
cd "$TMP/k9s"
git fetch -q --tags origin
TO_SHA="$(git rev-parse "origin/$TO" 2>/dev/null || git rev-parse "$TO^{commit}")"

echo "# k9s upstream: $FROM_TAG → $TO"
echo
echo "Vendored: \`$FROM_TAG\` (\`${FROM_SHA:0:12}\`) · target: \`$TO\` (\`${TO_SHA:0:12}\`) · $(git rev-list --count "$FROM_SHA..$TO_SHA") upstream commits in total."
echo

# releases published after the vendored one
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
  echo "$(wc -l <<<"$commits") of $(git rev-list --count "$FROM_SHA..$TO_SHA") commits:"
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

echo "## Files kubeon patches or overlays"
echo
risk=0
for f in "${RISKY[@]}"; do
  if ! git diff --quiet "$FROM_SHA" "$TO_SHA" -- "$f"; then
    echo "- ⚠️ \`$f\` changed upstream: check \`third_party/_k9s-patches\` / \`_k9s-overlay\` still fit"
    risk=1
  fi
done
[[ $risk == 1 ]] || echo "_Unchanged upstream: patches and overlays should apply cleanly._"
