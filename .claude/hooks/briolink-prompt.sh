#!/bin/sh
# UserPromptSubmit: while the folder is bound, remind the model to route through master,
# and name the role of this session's last reply, read from its transcript.
[ -f "$CLAUDE_PROJECT_DIR/.briolink/bound" ] || exit 0
t=$(sed -n 's/.*"transcript_path" *: *"\([^"]*\)".*/\1/p')
last=""
[ -n "$t" ] && [ -f "$t" ] && last=$(grep '"role":"assistant"' "$t" | grep -o '"type":"text","text":"Role: [a-z]*' | tail -1 | sed 's/.*Role: //')
if [ -n "$last" ]; then
  echo "Briolink bound. The last reply was Role: $last. Continue in $last unless this prompt is clearly a different kind of work; then route as master (AGENTS.md). Start the reply with Role: <name>."
else
  echo "Briolink bound: as master, route this prompt to a role (AGENTS.md) and start the reply with Role: <name>."
fi
exit 0
