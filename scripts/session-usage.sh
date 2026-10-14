#!/usr/bin/env bash
# Token use of the current Claude Code session (newest transcript, subagents included) and the plan's two limit windows, on one line.
# Usage: scripts/session-usage.sh [--limits | transcript.jsonl]
set -euo pipefail

limits=$(jq -r '.cachedUsageUtilization.utilization // empty
  | "plan: 5h \(.five_hour.utilization)% (resets \(.five_hour.resets_at[11:16])Z), 7d \(.seven_day.utilization)% (resets \(.seven_day.resets_at[0:10]))"' \
  ~/.claude.json 2>/dev/null || true)
limits=${limits:-plan: unknown}
[ "${1:-}" = --limits ] && { echo "$limits"; exit; }

root=$(git rev-parse --show-toplevel)
dir="$HOME/.claude/projects/$(printf '%s' "$root" | sed 's/[^a-zA-Z0-9]/-/g')"
main=${1:-$(ls -t "$dir"/*.jsonl | head -1)}
subs=("${main%.jsonl}"/subagents/*.jsonl)
[ -e "${subs[0]}" ] || subs=()

k() { awk '{ if ($1 >= 1e6) printf "%.1fM", $1/1e6; else if ($1 >= 1e3) printf "%.1fk", $1/1e3; else print $1 }' <<<"$1"; }

# Each response is logged once per content block: count each message id once.
while IFS=$'\t' read -r model turns in out cr cw; do
  parts+=("$model: $turns turns, in $(k "$in"), out $(k "$out"), cache $(k "$cr") read / $(k "$cw") write")
done < <(jq -rs '
  [.[] | select(.type == "assistant" and .message.usage != null)]
  | unique_by(.message.id) | group_by(.message.model)[]
  | [.[0].message.model, length,
     (map(.message.usage.input_tokens // 0) | add),
     (map(.message.usage.output_tokens // 0) | add),
     (map(.message.usage.cache_read_input_tokens // 0) | add),
     (map(.message.usage.cache_creation_input_tokens // 0) | add)]
  | @tsv' "$main" "${subs[@]}")

line=$(IFS=';'; echo "${parts[*]:-no turns}")
echo "${line//;/ · } · $limits"
