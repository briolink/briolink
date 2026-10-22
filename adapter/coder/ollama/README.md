# adapter/coder/ollama

The [Coder port](../../../port/coder) on a local Ollama server (E14), over
`POST /api/chat`. Thin: no Ollama feature beyond a JSON-schema constrained
answer.

- **Request:** a fixed system message (so the prefix is stable and cached), then
  the request's parts joined in order as one user message; `stream` off,
  temperature 0, `format` the schema `{"files": {path: content}}`.
- **Answer:** the files and the model Ollama names (the configured one if it
  names none). A path that is empty, absolute, unclean or escaping is
  `ErrAnswer`, as is anything but a non-empty `{"files": …}`; the runner retries
  on it. Ollama unreachable or not 200 is `ErrAPI`. The context bounds the
  call: there is no client timeout.
- **Config:** `Model` (the routing table's `coder` tier) and `BaseURL`
  (`http://127.0.0.1:11434` by default).

Tests run the port's contract against a fake server. `TestLive` runs it against
a real model: `BRIOLINK_LIVE_OLLAMA=qwen3-coder:30b go test -run TestLive ./adapter/coder/ollama/`.
