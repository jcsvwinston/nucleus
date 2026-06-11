#!/usr/bin/env bash
#
# scripts/website/check-coverage.sh — drift guard for the PUBLIC docs site.
#
# The public Docusaurus site lives under website/docs/** and is a curated
# reflection of Nucleus as it ships. Its deploy workflow (docs.yml) only
# fires on `paths: website/**`, so a change to pkg/* / internal/cli / config
# that is NOT mirrored into website/docs silently rots the site. This script
# is the heuristic guard that surfaces that drift.
#
# It is intentionally a HEURISTIC, not a proof: faithful documentation cannot
# be machine-verified without an oracle. It optimises for high-signal,
# low-false-positive checks and errs toward false-negatives over noise.
#
# Checks:
#   1. Legacy / removed-API tokens in website/docs (e.g. `GoFrame`, the
#      removed `.SQLite()/.Postgres()/.MySQL()` fluent chain, `RouterGroup`).
#   2. Dangling `covers:` frontmatter entries — a page claims to document a
#      stable symbol that is no longer in contracts/baseline/api_exported_symbols.txt.
#   3. Coverage hygiene — pages with no `covers:` manifest at all (informational).
#
# Usage:
#   scripts/website/check-coverage.sh            # WARN mode: report, exit 0
#   scripts/website/check-coverage.sh --strict   # FAIL on legacy tokens or dangling refs
#
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DOCS="$REPO_ROOT/website/docs"
BASELINE="$REPO_ROOT/contracts/baseline/api_exported_symbols.txt"
MODULE_PREFIX="github.com/jcsvwinston/nucleus"

STRICT=0
[ "${1:-}" = "--strict" ] && STRICT=1

if [ ! -d "$WEB_DOCS" ]; then
  echo "check-coverage: website/docs not found at $WEB_DOCS" >&2
  exit 2
fi

# Portable array fill (avoid bash-4 `mapfile`; macOS ships bash 3.2).
PAGES=()
while IFS= read -r _f; do
  [ -n "$_f" ] && PAGES+=("$_f")
done < <(find "$WEB_DOCS" -type f \( -name '*.md' -o -name '*.mdx' \) | sort)

legacy_hits=0
dangling_hits=0
missing_manifest=0

note()  { printf '%s\n' "$*"; }
rel()   { printf '%s' "${1#"$REPO_ROOT"/}"; }

# ---------------------------------------------------------------------------
# 1. Legacy / removed-API token scan
# ---------------------------------------------------------------------------
note "== 1. legacy / removed-API tokens =="
# High-confidence removed tokens only. The removed legacy fluent chain used
# .SQLite()/.Postgres()/.MySQL() builder methods and a RouterGroup type;
# GoFrame/goframe is the pre-rename name. These should never appear in the
# current public docs.
LEGACY_RE='GoFrame|goframe|RouterGroup|\.SQLite\(|\.Postgres\(|\.MySQL\('
if matches="$(grep -rnE "$LEGACY_RE" "$WEB_DOCS" --include='*.md' --include='*.mdx' 2>/dev/null)"; then
  while IFS= read -r line; do
    [ -n "$line" ] && note "  STALE  $(rel "$line")"
  done <<< "$matches"
  legacy_hits="$(printf '%s\n' "$matches" | grep -c . )"
else
  note "  none"
fi

# ---------------------------------------------------------------------------
# 2. Dangling covers: references vs the freeze baseline
# ---------------------------------------------------------------------------
note ""
note "== 2. dangling covers: references =="
if [ ! -f "$BASELINE" ]; then
  note "  (skipped — freeze baseline not found at $(rel "$BASELINE"))"
else
  for page in "${PAGES[@]}"; do
    # Extract `covers:` entries: bullet lines of the form `  - pkg/<pkg>.<symbol>`
    # that appear in the page (the manifest lives in YAML frontmatter).
    while IFS= read -r entry; do
      [ -z "$entry" ] && continue
      # entry e.g. "pkg/nucleus.AppBuilder.FromConfigFile"
      pkg="$(printf '%s' "$entry" | sed -E 's#^pkg/([a-z0-9_]+)\..*#\1#')"
      sym="$(printf '%s' "$entry" | sed -E 's#^pkg/[a-z0-9_]+\.##')"
      [ -z "$pkg" ] && continue
      [ -z "$sym" ] && continue
      import="$MODULE_PREFIX/pkg/$pkg"
      # A live symbol appears in the baseline as: "<import> <kind>:<sym>".
      # No `-q` on the right-hand grep: under `set -o pipefail`, -q exits at
      # the first match and SIGPIPEs the left grep mid-write (exit 141), which
      # `if !` then misreads as "symbol absent" — a timing-dependent spurious
      # DANGLING that bites on large baseline blocks (seen on pkg/storage).
      # Without -q the right grep drains its stdin, so neither side can fail.
      if ! grep -F "$import " "$BASELINE" | grep -F ":$sym" >/dev/null; then
        note "  DANGLING  $(rel "$page"): covers '$entry' not in freeze baseline"
        dangling_hits=$((dangling_hits + 1))
      fi
    done < <(grep -oE 'pkg/[a-z0-9_]+\.[A-Za-z0-9_.]+' "$page" 2>/dev/null \
               | awk -v RS='[^A-Za-z0-9_./]+' '/^pkg\/[a-z0-9_]+\.[A-Z]/' \
               | sort -u)
  done
  [ "$dangling_hits" -eq 0 ] && note "  none"
fi

# ---------------------------------------------------------------------------
# 3. Coverage hygiene — pages with no covers: manifest (informational)
# ---------------------------------------------------------------------------
note ""
note "== 3. pages without a covers: manifest (informational) =="
for page in "${PAGES[@]}"; do
  # _category_ files and pure index pages are exempt.
  case "$(basename "$page")" in
    _category_*.json) continue ;;
  esac
  if ! grep -qE '^covers:' "$page" 2>/dev/null; then
    note "  no-manifest  $(rel "$page")"
    missing_manifest=$((missing_manifest + 1))
  fi
done
[ "$missing_manifest" -eq 0 ] && note "  none"

# ---------------------------------------------------------------------------
# 4. Body-content fact-check (§9 anti-falsehood discipline)
# ---------------------------------------------------------------------------
# The frontmatter/token checks above cannot see the BODY of a page — where
# the 2026-05-24 P0 falsehoods hid (wrong Go version, non-existent function,
# inexistent YAML key). scripts/website/bodycheck validates, in page bodies:
# Go-version claims vs go.mod, and Nucleus `pkg.Symbol` refs in fenced go
# blocks vs the freeze baseline (both hard), plus YAML keys vs the config
# registry (advisory). It is the automated complement to the
# docs-content-verifier subagent (CLAUDE.md §9).
note ""
note "== 4. body-content fact-check (§9) =="
body_hits=0
if command -v go >/dev/null 2>&1; then
  # Always run the tool in -strict mode so body_hits reflects hard findings
  # accurately in the summary; the OUTER --strict flag (below) decides whether
  # those findings fail this script.
  if ! ( cd "$REPO_ROOT" && go run ./scripts/website/bodycheck -root "$REPO_ROOT" -docs website/docs -strict ); then
    body_hits=1
  fi
else
  note "  (skipped — Go toolchain not found; this check runs in CI and locally with Go installed)"
fi

# ---------------------------------------------------------------------------
# Summary + exit policy
# ---------------------------------------------------------------------------
note ""
note "== summary =="
note "  legacy/removed tokens : $legacy_hits"
note "  dangling covers refs  : $dangling_hits"
note "  pages w/o manifest    : $missing_manifest (informational)"
note "  body-content (§9)     : $([ "$body_hits" -gt 0 ] && echo 'FAIL' || echo 'ok')"

if [ "$STRICT" -eq 1 ] && { [ "$legacy_hits" -gt 0 ] || [ "$dangling_hits" -gt 0 ] || [ "$body_hits" -gt 0 ]; }; then
  note ""
  note "FAIL (--strict): website/docs drift or body-content falsehood detected. Reconcile via the website-curator / docs-content-verifier subagents."
  exit 1
fi

note ""
note "OK (warn mode). Drift findings above are advisory; reconcile with the website-curator subagent."
exit 0
