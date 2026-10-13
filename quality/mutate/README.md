# quality/mutate

The mutation checker: mutates operators, boundaries and constants in a pure
function, reruns its tests, and reports every surviving mutant with its
position. Built on `go/ast`, standard library only.

See [docs/2-architecture.md § Specs and generators](../docs/2-architecture.md#specs-and-generators)
and the roadmap's [quality] spike.
