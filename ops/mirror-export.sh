#!/usr/bin/env bash
# Keeps the ops host's copy of the code host (docs/4-choices.md, code host
# fallback): `mirror` runs `git clone --mirror` / `remote update` for every repo
# of the org; `export` writes issues (labels, milestones, comments included) as
# JSON under export/<date>/<repo>/ and fails unless each issue count matches the
# code host's. Run by briolink-mirror.timer and briolink-export.timer as root.
# Per M15 it parses MIRROR_GH_TOKEN (read-only) from the repo-root .env (path
# in MIRROR_ENV_FILE) and never sources it; the token is never put on a command
# line or in a git config.
set -euo pipefail

ORG=${MIRROR_ORG:-briolink}
DATA=${MIRROR_DATA:-/var/lib/briolink-mirror}
KEEP_DAYS=${EXPORT_KEEP_DAYS:-14}
API=https://api.github.com
TOKEN=$(grep -m1 '^MIRROR_GH_TOKEN=' "$MIRROR_ENV_FILE" | cut -d= -f2- | tr -d "\"'[:space:]")
[ -n "$TOKEN" ] || { echo "MIRROR_GH_TOKEN missing from $MIRROR_ENV_FILE" >&2; exit 1; }

# GET a path (or a full URL) with pagination; prints one JSON array of all items.
api() {
  local url=$1 out='[]' page
  [[ $url == http* ]] || url=$API$url
  while [ -n "$url" ]; do
    page=$(mktemp); hdr=$(mktemp)
    curl -fsS --max-time 60 -D "$hdr" -o "$page" \
      -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" "$url"
    out=$(jq -s '.[0] + .[1]' <(echo "$out") "$page")
    url=$(grep -i '^link:' "$hdr" | grep -o '<[^>]*>; rel="next"' | sed 's/^<//;s/>.*//' || true)
    rm -f "$page" "$hdr"
  done
  echo "$out"
}

repos() { api "/orgs/$ORG/repos?per_page=100&type=all" | jq -r '.[].name'; }

# Git gets the token as a header from the environment, not argv.
git_auth() {
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.extraHeader \
  GIT_CONFIG_VALUE_0="Authorization: Basic $(printf 'x-access-token:%s' "$TOKEN" | base64 -w0)" \
  GIT_TERMINAL_PROMPT=0 "$@"
}

mirror() {
  mkdir -p "$DATA/repos"
  local r d
  for r in $(repos); do
    d=$DATA/repos/$r.git
    if [ -d "$d" ]; then
      git_auth git -C "$d" remote update --prune
    else
      git_auth git clone --mirror -q "https://github.com/$ORG/$r.git" "$d"
    fi
    echo "$(date -u +%FT%TZ) mirrored $r"
  done
}

export_issues() {
  local day dir r n want
  day=$(date -u +%F); dir=$DATA/export/$day
  rm -rf "$dir.tmp"; mkdir -p "$dir.tmp"
  for r in $(repos); do
    mkdir -p "$dir.tmp/$r"
    api "/repos/$ORG/$r/issues?state=all&per_page=100" | jq '[.[] | select(has("pull_request")|not)]' > "$dir.tmp/$r/issues.json"
    api "/repos/$ORG/$r/issues/comments?per_page=100" > "$dir.tmp/$r/comments.json"
    api "/repos/$ORG/$r/labels?per_page=100" > "$dir.tmp/$r/labels.json"
    api "/repos/$ORG/$r/milestones?state=all&per_page=100" > "$dir.tmp/$r/milestones.json"
    n=$(jq length "$dir.tmp/$r/issues.json")
    # The code host's own count, from a separate endpoint (the highest issue-or-PR
    # number is not usable): search counts issues only.
    want=$(curl -fsS --max-time 30 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
      "$API/search/issues?q=repo:$ORG/$r+is:issue&per_page=1" | jq .total_count)
    [ "$n" = "$want" ] || { echo "$r: exported $n issues, code host reports $want" >&2; exit 1; }
    echo "$(date -u +%FT%TZ) exported $r: $n issues"
  done
  rm -rf "$dir"; mv "$dir.tmp" "$dir"
  find "$DATA/export" -mindepth 1 -maxdepth 1 -type d -mtime "+$KEEP_DAYS" -exec rm -rf {} +
}

case ${1:-} in
  mirror) mirror ;;
  export) export_issues ;;
  *) echo "usage: $0 mirror|export" >&2; exit 2 ;;
esac
