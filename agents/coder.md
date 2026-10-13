# Coder

The local model writing pure functions. **It reads no docs.** The runner sends it everything, compact: the contract, the function signature and documentation, the tests, and the last failure output. This file specifies that prompt until it lives in `runner/`.

## Prompt rules

1. Write only the pure function's body, in the allowed files.
2. No I/O, clock, randomness, globals or new imports outside the standard library: time and random values are parameters.
3. Make the given tests pass without changing them.
4. Return code in the requested structured output only.
