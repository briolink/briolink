# plugins/http

Routing on `net/http` patterns, JSON and errors: the `routes` spec kind's
generator, which turns `spec/routes.json` into typed requests and responses,
request validation, handlers and the route table.

- `spec/`: format v0 for `spec/routes.json`: decoding, `Validate`, and
  `CheckAgainst` (every route's context, call and types exist in the domain
  model).
- `gen/`: the generator `plugin.json` points `spec_kinds[].generator` at.
  `Host` loads the routes and the project's `spec/domain/*.json`, reads the
  options and calls `Generate`.
- `plugin.json`: the manifest. Options: `base_path` (default `/`, prefix of
  every route) and `error_format` (`problem`, RFC 9457, the only value).

## What it generates

Under `gen/http/` (package `api`):

| File | Holds |
| --- | --- |
| `runtime.go` | Fixed support code: `Auth`, `ErrNotFound`, `Problem`, strict JSON reading, helpers |
| `wire.go` | The JSON shape of each domain type a route carries (snake_case keys), with its decoder (which validates) and encoder |
| `routes.go` | `Service`, one `<Route>Request` and `<Route>Response` per route, `Routes` (the table), `Handler` and the handlers |
| `api_test.go` | The derived tests |

## The seam: `Service`

`Service` has one method per route, taking the validated `<Route>Request` and
returning `<Route>Response` (or only an error for a 204). Each handler checks
the role, validates the request, calls its method and encodes the answer.
What the method does (load the entity, fire the event, call the pure
function, persist) is the project's: the spec names the call in the method's
comment, and the persistence that surrounds it is not part of this plugin.
Requests and responses use the domain package's types.

## What a handler checks

- **Role:** no role from `Auth` is a 401; another role than the route's, a 403.
- **Path, query and body values:** type, required or optional (`?`), `min` and
  `max` (value for an int, characters for a string, items for a list), and
  `pattern`. Every problem is reported at once, as a 400 with a list of
  `{in, field, message}`.
- **Strictness:** an unknown query parameter, an unknown body field (at any
  depth), a query value given twice, a body that is not one JSON object, a
  wrong `Content-Type` (415) or a body over 1 MiB (413) is refused. Domain
  enums are checked against their values. Go's JSON decoder matches key
  names case-insensitively; this plugin does not change that.
- **Errors from `Service`:** `ErrNotFound` is a 404; `ErrConflict` a 409 and
  `ErrUnprocessable` a 422, for a request that clashes with the current state
  or that a rule outside the spec refuses; the domain package's `ErrForbidden`
  and `ErrNoTransition` a 409, `InvariantError` a 422; any other error a 500
  that does not repeat its text. The answer never repeats an error's text.

The mux's own 404 and 405 answers are plain text, not problem details.

## Derived tests

For each route, against any `Service`:

- the baseline request (every param, valid) and the one without optional
  params are answered with the declared status and fields and reach the
  route's method; the scalar values arrive unchanged;
- every way the spec rules a request out is refused with a problem and the
  `Service` is never called: each param missing, null, of the wrong type,
  below `min`, above `max`, off `pattern`, outside its enum, a nested value
  missing a field or carrying an unknown one, plus the whole-request cases
  above;
- each route is refused without its role;
- the `Service`'s errors map as above;
- each domain type's JSON shape decodes and encodes back unchanged.

The sample values come from the spec alone. A `pattern` is satisfied by
trying a fixed pool of strings, so a pattern none of them matches fails
generation with a message asking for a simpler one.

## Fixtures

`generate/testdata/acme` (generated through the host, committed) and
`gen/testdata/shop` (query values, lists, optional values, a base path and a
context without a statechart). The plugin's tests build both as Go modules and
run the generated tests, then break the generated code in 21 ways and check
the generated tests fail each time.
