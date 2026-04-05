#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Configure GitHub branch protection with GoFrame required merge checks.

Usage:
  bash scripts/ci/configure_branch_protection.sh [options]

Options:
  --repo <owner/name>     GitHub repository (default: inferred from origin remote)
  --branch <name>         Branch name (default: main)
  --required-check <name> Required status check context (default: CI Required Gate)
  --approvals <n>         Required approving reviews (default: 1)
  --dry-run               Print payload and API target without applying changes
  -h, --help              Show help

Examples:
  bash scripts/ci/configure_branch_protection.sh --dry-run
  bash scripts/ci/configure_branch_protection.sh --repo jcsvwinston/GoFrame --branch main
EOF
}

infer_repo() {
  local remote
  remote="$(git config --get remote.origin.url || true)"
  remote="${remote%.git}"

  case "$remote" in
    https://github.com/*/*)
      printf '%s\n' "${remote#https://github.com/}"
      ;;
    git@github.com:*/*)
      printf '%s\n' "${remote#git@github.com:}"
      ;;
    *)
      printf '%s\n' ""
      ;;
  esac
}

repo=""
branch="main"
required_check="CI Required Gate"
approvals="1"
dry_run="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --branch)
      branch="${2:-}"
      shift 2
      ;;
    --required-check)
      required_check="${2:-}"
      shift 2
      ;;
    --approvals)
      approvals="${2:-}"
      shift 2
      ;;
    --dry-run)
      dry_run="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$repo" ]]; then
  repo="$(infer_repo)"
fi

if [[ -z "$repo" ]]; then
  echo "Unable to infer --repo from origin remote. Provide --repo <owner/name>." >&2
  exit 1
fi

if ! [[ "$approvals" =~ ^[0-9]+$ ]]; then
  echo "--approvals must be a non-negative integer" >&2
  exit 1
fi

payload=$(
  cat <<EOF
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["$required_check"]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": $approvals
  },
  "restrictions": null,
  "required_conversation_resolution": true
}
EOF
)

api_target="repos/${repo}/branches/${branch}/protection"

if [[ "$dry_run" == "true" ]]; then
  echo "Dry run mode."
  echo "Target: $api_target"
  echo "Payload:"
  printf '%s\n' "$payload"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required. Install gh and run again." >&2
  exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "Not authenticated in gh. Run 'gh auth login' and retry." >&2
  exit 1
fi

gh api \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  "$api_target" \
  --input - <<<"$payload" >/dev/null

echo "Branch protection updated for ${repo}:${branch}"
echo "Required check: ${required_check}"
