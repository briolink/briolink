#!/usr/bin/env bash
# Code host fallback restore (docs/4-choices.md E13, spike #17): loads one repo's
# mirror (git) and issue export (milestones, labels, issues, comments) into an
# empty Forgejo. Issue numbers are kept by filling gaps (the pull requests, which
# the export doesn't hold) with closed placeholders. Authors and timestamps can't
# be set through the API: they are written into each body as an "Imported" line.
# Measured on Forgejo 11: ~1 s per issue. Credentials here are a throwaway admin's.
# usage: forgejo-restore.sh <mirror.git> <export/<date>/<repo>> <base url> <user:pass> <repo>
set -euo pipefail
M=$1 E=$2 URL=$3 AUTH=$4 REPO=$5; U=${AUTH%%:*}
f() { curl -fsS -u "$AUTH" -H 'Content-Type: application/json' "$@"; }
t0=$(date +%s.%N)
f -X POST $URL/api/v1/user/repos -d "{\"name\":\"$REPO\",\"private\":true}" >/dev/null
git -C $M push -q "http://$AUTH@${URL#http://}/$U/$REPO.git" "refs/heads/*:refs/heads/*" "refs/tags/*:refs/tags/*" 2>&1 | tail -3
t1=$(date +%s.%N); echo "git push: $(echo "$t1-$t0"|bc)s"
# milestones
declare -A MS LB
while IFS=$'\t' read -r n title desc state due; do
  id=$(f -X POST $URL/api/v1/repos/$U/$REPO/milestones -d "$(jq -n --arg t "$title" --arg d "$desc" --arg s "$state" --arg due "$due" '{title:$t,description:$d,state:$s} + (if $due!="" then {due_on:$due} else {} end)')" | jq .id)
  MS[$n]=$id
done < <(jq -r '.[]|[.number,.title,(.description//""),.state,(.due_on//"")]|@tsv' $E/milestones.json)
while IFS=$'\t' read -r name color desc; do
  id=$(f -X POST $URL/api/v1/repos/$U/$REPO/labels -d "$(jq -n --arg n "$name" --arg c "#$color" --arg d "$desc" '{name:$n,color:$c,description:$d}')" | jq .id)
  LB[$name]=$id
done < <(jq -r '.[]|[.name,.color,(.description//"")]|@tsv' $E/labels.json)
max=$(jq 'map(.number)|max' $E/issues.json)
for n in $(seq 1 $max); do
  iss=$(jq --argjson n $n '.[]|select(.number==$n)' $E/issues.json)
  if [ -z "$iss" ]; then
    f -X POST $URL/api/v1/repos/$U/$REPO/issues -d "{\"title\":\"placeholder $n (was a pull request)\",\"closed\":true}" >/dev/null; continue
  fi
  lids=$(jq -r '.labels[].name' <<<"$iss" | while read -r l; do echo "${LB[$l]}"; done | jq -s .)
  ms=$(jq -r '.milestone.number // empty' <<<"$iss"); msid=0; [ -n "$ms" ] && msid=${MS[$ms]}
  body=$(jq -r '"_Imported: originally by @\(.user.login), opened \(.created_at)_\n\n\(.body//"")"' <<<"$iss")
  closed=$(jq '.state=="closed"' <<<"$iss")
  f -X POST $URL/api/v1/repos/$U/$REPO/issues -d "$(jq -n --argjson i "$iss" --arg b "$body" --argjson l "$lids" --argjson m "$msid" --argjson c "$closed" '{title:$i.title,body:$b,labels:$l,milestone:$m,closed:$c}')" >/dev/null
  jq -c --argjson n $n '.[]|select((.issue_url|split("/")|last|tonumber)==$n)' $E/comments.json | while read -r c; do
    f -X POST $URL/api/v1/repos/$U/$REPO/issues/$n/comments -d "$(jq -n --argjson c "$c" '{body:"_Imported: @\($c.user.login), \($c.created_at)_\n\n\($c.body)"}')" >/dev/null
  done
done
t2=$(date +%s.%N); echo "issues import: $(echo "$t2-$t1"|bc)s; total $(echo "$t2-$t0"|bc)s"
