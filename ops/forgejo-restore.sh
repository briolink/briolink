#!/usr/bin/env bash
# Code host fallback restore (docs/4-choices.md E13, spike #17): loads one repo's
# mirror (git) and issue export (milestones, labels, issues, comments) into an
# empty Forgejo. Issue numbers are kept by filling gaps (the pull requests, which
# the export doesn't hold) with closed placeholders. Authors and timestamps can't
# be set through the API: they are written into each body as an "Imported" line.
# Measured on Forgejo 11: ~1 s per issue.
# The password comes from the environment, never from an argument; it reaches curl
# and git through a private temp folder that is removed on exit.
# usage: FORGEJO_PASS=... forgejo-restore.sh <mirror.git> <export/<date>/<repo>> <base url> <user> <repo>
set -euo pipefail
M=$1 E=$2 URL=${3%/} U=$4 REPO=$5
: "${FORGEJO_PASS:?set FORGEJO_PASS}"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; chmod 700 "$T"
host=${URL#*://}; host=${host%%[:/]*}
printf 'machine %s login %s password %s\n' "$host" "$U" "$FORGEJO_PASS" >"$T/netrc"
printf '#!/bin/sh\ncase "$1" in Username*) echo "%s";; *) echo "$FORGEJO_PASS";; esac\n' "$U" >"$T/askpass"
chmod 600 "$T/netrc"; chmod 700 "$T/askpass"
die() { echo "restore failed: $*" >&2; exit 1; }
f() { curl -fsS --netrc-file "$T/netrc" -H 'Content-Type: application/json' "$@"; }
API=$URL/api/v1/repos/$U/$REPO
t0=$(date +%s.%N)
f -X POST $URL/api/v1/user/repos -d "$(jq -n --arg n "$REPO" '{name:$n,private:true}')" >/dev/null   # fails if it exists: the target must be new
GIT_ASKPASS="$T/askpass" GIT_TERMINAL_PROMPT=0 git -c credential.helper= -C "$M" push -q "$URL/$U/$REPO.git" "refs/heads/*:refs/heads/*" "refs/tags/*:refs/tags/*"
t1=$(date +%s.%N); echo "git push: $(echo "$t1-$t0"|bc)s"
declare -A MS LB
while read -r m; do
  id=$(f -X POST $API/milestones -d "$(jq -c '{title,description:(.description//""),state} + (if .due_on then {due_on} else {} end)' <<<"$m")" | jq .id)
  MS[$(jq -r .number <<<"$m")]=$id
done < <(jq -c '.[]' $E/milestones.json)
while read -r l; do
  id=$(f -X POST $API/labels -d "$(jq -c '{name,color:("#"+.color),description:(.description//"")}' <<<"$l")" | jq .id)
  LB[$(jq -r .name <<<"$l")]=$id
done < <(jq -c '.[]' $E/labels.json)
COM=$(jq -c 'group_by(.issue_url|split("/")|last)|map({key:(.[0].issue_url|split("/")|last),value:.})|from_entries' $E/comments.json)
next=1
placeholder() { f -X POST $API/issues -d "{\"title\":\"placeholder $next (was a pull request)\",\"closed\":true}" >/dev/null; next=$((next+1)); }
while read -r iss; do
  n=$(jq .number <<<"$iss")
  while [ "$next" -lt "$n" ]; do placeholder; done
  lids=()
  while read -r name; do
    [ -n "${LB[$name]:-}" ] || die "issue $n has label '$name' that labels.json lacks"
    lids+=("${LB[$name]}")
  done < <(jq -r '.labels[].name' <<<"$iss")
  lj=$(printf '%s\n' "${lids[@]+"${lids[@]}"}" | jq -s 'map(select(.!=""))|map(tonumber)')
  ms=$(jq -r '.milestone.number // empty' <<<"$iss"); msid=0
  if [ -n "$ms" ]; then msid=${MS[$ms]:-}; [ -n "$msid" ] || die "issue $n has milestone $ms that milestones.json lacks"; fi
  got=$(f -X POST $API/issues -d "$(jq -n --argjson i "$iss" --argjson l "$lj" --argjson m "$msid" '{title:$i.title,body:("_Imported: originally by @\($i.user.login), opened \($i.created_at)_\n\n"+($i.body//"")),labels:$l,milestone:$m,closed:($i.state=="closed")}')" | jq .number)
  [ "$got" = "$n" ] || die "issue $n was created as #$got (target repo not empty?)"
  next=$((n+1))
  while read -r c; do
    f -X POST $API/issues/$n/comments -d "$(jq -n --argjson c "$c" '{body:"_Imported: @\($c.user.login), \($c.created_at)_\n\n\($c.body)"}')" >/dev/null
  done < <(jq -c --arg n "$n" '.[$n][]?' <<<"$COM")
done < <(jq -c 'sort_by(.number)|.[]' $E/issues.json)
t2=$(date +%s.%N); echo "issues import: $(echo "$t2-$t1"|bc)s; total $(echo "$t2-$t0"|bc)s"
