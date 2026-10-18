#!/usr/bin/env bash
# The deterministic forms of /briolink (M10): the board, list, exit, and the binding itself. No model is involved.
# Usage: scripts/briolink.sh [list|exit|bind]   (no argument: bind and show the board)
#        scripts/briolink.sh --hook             (UserPromptSubmit hook: reads the hook JSON on stdin)
# In hook mode, `/briolink`, `/briolink list` and `/briolink exit` are answered here and the prompt is blocked,
# so no model turn runs; any other `/briolink …` binds here and goes on to the model; any other prompt
# gets the routing reminder while bound.
set -euo pipefail

root=$(git -C "${CLAUDE_PROJECT_DIR:-.}" rev-parse --show-toplevel)
cd "$root"
# The founder is the owner of agents/ in CODEOWNERS: one place, already founder-owned, different in every project.
founder=$(awk '$1 == "/agents/" { sub(/^@/, "", $2); print $2 }' .github/CODEOWNERS)
flag=.briolink/bound
model_id=""   # the session's model, known only in hook mode

gh_account() {
  [ -n "$founder" ] || { echo "gh: no owner of /agents/ in .github/CODEOWNERS; stopped."; return 1; }
  [ "$(gh api user --jq .login 2>/dev/null || true)" = "$founder" ] && return
  if gh auth switch --user "$founder" >/dev/null 2>&1; then
    echo "gh: switched to $founder."
  else
    echo "gh: could not switch to $founder; stopped." && return 1
  fi
}

# Markdown links to their text, so the terminal shows "#58", not the URL.
plain() { sed -E 's/\[([^]]*)\]\([^)]*\)/\1/g'; }

roles() {
  echo "Roles (agents/master.md):"
  sed -n '/^| Role | Brief |/,/^$/p' agents/master.md | grep '^| `' | sed -E 's/^\| `([a-z]+)`: ([^|]*) \|.*/- \1: \2/'
}

# role and tier of an issue: its role:<name> label, else builder; the tier from spec/routing.json.
tier_of() { jq -r --arg r "$1" '.jobs["session." + $r] // "routine"' spec/routing.json; }
model_of() { jq -r --arg t "$1" '.tiers[$t]' spec/routing.json; }

board() {
  local stage last lead today issues
  today=$(date +%F)
  stage=$(grep -m1 '^- \*\*Stage:\*\*' docs/5-roadmap.md | sed -E 's/^- \*\*Stage:\*\* ([A-Z][0-9]+).*/\1/')
  last=$(grep -m1 '^- \*\*Last' docs/5-roadmap.md | sed -E 's/^- \*\*Last \(([^)]*)\):\*\* /(\1) /' | plain)
  lead=$(grep -m1 '^- \*\*Lead:\*\*' docs/5-roadmap.md | sed -E 's/^- \*\*Lead:\*\* //' | plain)

  echo "Session bound."
  echo
  echo "Last: $last"
  echo
  echo "Next:"
  echo "- Lead: $lead"
  issues=$(gh issue list --state open --label state:queued --limit 100 \
    --json number,title,labels,milestone)
  # Lead issues first (in the order the Lead names them), then the current stage's milestone, then the rest; five at most.
  local order
  order=$(grep -m1 '^- \*\*Lead:\*\*' docs/5-roadmap.md | grep -oE 'issues/[0-9]+' | cut -d/ -f2 | awk '!s[$0]++' | paste -sd, -)
  jq -r --arg stage "$stage" --arg lead "[${order}]" '
    ($lead | fromjson) as $l
    | map(.number as $n | . + {
        rank: ([$l | to_entries[] | select(.value == $n) | .key][0] // (if (.milestone.title // "") | startswith($stage + " ") then 100 else 200 end)),
        role: ([.labels[].name | select(startswith("role:")) | ltrimstr("role:")][0] // "")
      })
    | sort_by(.rank, (.milestone.number // 999), .number) | .[:5][]
    | [.number, .title, .role] | @tsv' <<<"$issues" |
  while IFS=$'\t' read -r n title role; do
    local mark="" tier want switch=""
    [ -n "$role" ] || { role=builder; mark="*"; }
    tier=$(tier_of "$role"); want=$(model_of "$tier")
    [ -n "$model_id" ] && [ "$model_id" != "$want" ] && switch="; switch with /model $want"
    echo "- #$n $title → $role$mark ($tier$switch)"
  done
  echo "  (* no role: label; builder by default)"
  echo
  echo "Waiting:"
  gh issue list --state open --label state:waiting --limit 100 --json number,title,body |
  jq -r '.[] | [.number, .title, ((.body | split("\n") | map(select(test("Waiting until"))) | .[0]) // "no Waiting until line")] | @tsv' |
  while IFS=$'\t' read -r n title line; do
    local due flag_due=""
    line=$(sed -E 's/\*\*//g' <<<"$line" | plain)
    due=$(grep -oE 'Waiting until [0-9]{4}-[0-9]{2}-[0-9]{2}' <<<"$line" | grep -oE '[0-9-]{10}' || true)
    [ -n "$due" ] && [[ "$due" < "$today" || "$due" = "$today" ]] && flag_due=" ⚠ date reached"
    echo "- #$n $title: $line$flag_due"
  done
  echo
  echo "Limits: $(scripts/session-usage.sh --limits)"
  echo
  echo "Which one do you want to take?"
}

run() {
  case "${1:-}" in
    list) roles ;;
    exit) gh_account; rm -f "$flag"; echo "Unbound: plain model from now on." ;;
    bind) gh_account; mkdir -p .briolink; touch "$flag" ;;
    "")   gh_account; mkdir -p .briolink; touch "$flag"; board ;;
    *)    echo "usage: scripts/briolink.sh [list|exit|bind]" >&2; return 2 ;;
  esac
}

block() { jq -n --arg r "$1" '{decision: "block", reason: $r}'; }

hook() {
  local input prompt transcript out arg
  input=$(cat)
  prompt=$(jq -r '.prompt // ""' <<<"$input" | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')
  transcript=$(jq -r '.transcript_path // ""' <<<"$input")
  [ -f "$transcript" ] && model_id=$(jq -r 'select(.type == "assistant") | .message.model // empty' "$transcript" 2>/dev/null | grep -v '^<' | tail -1 || true)
  [ -n "$model_id" ] || model_id=$(jq -r '.model // empty' .claude/settings.json | sed 's/^sonnet$/claude-sonnet-5-5/; s/^opus$/claude-opus-5-5/')

  case "$prompt" in
    /briolink|"/briolink list"|"/briolink exit")
      arg=${prompt#/briolink}
      out=$(run "${arg# }" 2>&1 || true)
      block "$out" ;;
    "/briolink done"|"/briolink done "*) gh_account >&2 || true ;;   # done is the model's work; the command file says what to do
    "/briolink "*)
      out=$(run bind 2>&1) || { block "$out"; return; }
      [ -n "$out" ] && echo "$out" ;;
    *) [ -f "$flag" ] && echo 'Briolink bound: as master, route this prompt to a role (agents/master.md), start the reply with Role: <name>, and halt if the tier of the role is not your model.' ;;
  esac
  return 0
}

if [ "${1:-}" = --hook ]; then hook; else run "${1:-}"; fi
