<div align="center">

# 🎬&nbsp;&nbsp;go-actions

**Typed HTTP actions for Go — one declaration, a handler and an OpenAPI 3.1 contract that can't drift**

<br/>

<a href="https://github.com/mrz1836/go-actions/releases"><img src="https://img.shields.io/github/release-pre/mrz1836/go-actions?include_prereleases&style=flat-square&logo=github&color=black" alt="Release"></a>
<a href="https://golang.org/"><img src="https://img.shields.io/github/go-mod/go-version/mrz1836/go-actions?style=flat-square&logo=go&color=00ADD8" alt="Go Version"></a>
<a href="https://github.com/mrz1836/go-actions/blob/master/LICENSE"><img src="https://img.shields.io/github/license/mrz1836/go-actions?style=flat-square&color=blue" alt="License"></a>

<br/>

<table align="center" border="0">
  <tr>
    <td align="right">
       <code>CI / CD</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://github.com/mrz1836/go-actions/actions"><img src="https://img.shields.io/github/actions/workflow/status/mrz1836/go-actions/fortress.yml?branch=master&label=build&logo=github&style=flat-square" alt="Build"></a>
       <a href="https://github.com/mrz1836/go-actions/actions"><img src="https://img.shields.io/github/last-commit/mrz1836/go-actions?style=flat-square&logo=git&logoColor=white&label=last%20update" alt="Last Commit"></a>
    </td>
    <td align="right">
       &nbsp;&nbsp;&nbsp;&nbsp; <code>Quality</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://codecov.io/gh/mrz1836/go-actions"><img src="https://codecov.io/gh/mrz1836/go-actions/branch/master/graph/badge.svg?style=flat-square" alt="Coverage"></a>
    </td>
  </tr>

  <tr>
    <td align="right">
       <code>Security</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://scorecard.dev/viewer/?uri=github.com/mrz1836/go-actions"><img src="https://api.scorecard.dev/projects/github.com/mrz1836/go-actions/badge?style=flat-square" alt="Scorecard"></a>
       <a href=".github/SECURITY.md"><img src="https://img.shields.io/badge/policy-active-success?style=flat-square&logo=security&logoColor=white" alt="Security"></a>
    </td>
    <td align="right">
       &nbsp;&nbsp;&nbsp;&nbsp; <code>Docs</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://pkg.go.dev/github.com/mrz1836/go-actions"><img src="https://img.shields.io/badge/godoc-reference-blue?style=flat-square&logo=go&logoColor=white" alt="Go Reference"></a>
       <a href="https://mrz1818.com/"><img src="https://img.shields.io/badge/donate-bitcoin-ff9900?style=flat-square&logo=bitcoin" alt="Bitcoin"></a>
    </td>
  </tr>
</table>

</div>

<br/>
<br/>

<div align="center">

### <code>Project Navigation</code>

</div>

<table align="center">
  <tr>
    <td align="center" width="33%">
       📦&nbsp;<a href="#-installation"><code>Installation</code></a>
    </td>
    <td align="center" width="33%">
       ⚡&nbsp;<a href="#-quick-start"><code>Quick&nbsp;Start</code></a>
    </td>
    <td align="center" width="33%">
       🧪&nbsp;<a href="#-examples--tests"><code>Examples&nbsp;&&nbsp;Tests</code></a>
    </td>
  </tr>
  <tr>
    <td align="center">
       📚&nbsp;<a href="#-documentation"><code>Documentation</code></a>
    </td>
    <td align="center">
      🧰&nbsp;<a href="#-code-standards"><code>Code&nbsp;Standards</code></a>
    </td>
    <td align="center">
      📊&nbsp;<a href="#-benchmarks"><code>Benchmarks</code></a>
    </td>
  </tr>
  <tr>
    <td align="center">
      🤖&nbsp;<a href="#-ai-usage--assistant-guidelines"><code>AI&nbsp;Usage</code></a>
    </td>
    <td align="center">
       📝&nbsp;<a href="#-license"><code>License</code></a>
    </td>
    <td align="center">
       👥&nbsp;<a href="#-maintainers"><code>Maintainers</code></a>
    </td>
  </tr>
</table>
<br/>

## 🧩 About

**go-actions** is a typed HTTP action framework for [chi](https://github.com/go-chi/chi)
that generates your **OpenAPI** contract from the same code that serves traffic. You
declare a route **once** as a typed `Action[Req, Resp]`, and a `Registry` turns each
declaration into:

- an `http.HandlerFunc` that decodes, validates, handles, and encodes the request;
- an **OpenAPI 3.1** document (or 3.0.3 on request), served as JSON **and** YAML, with a
  component schema for each named type (JSON Schema 2020-12 in the 3.1 dialect); and
- a browsable `/_actions` index in HTML or Markdown.

Every artifact is built from the same reflected types and the same parsed struct tags:
the decoder, the validator, and the schema generator share one view of each type's
fields and one parser for its `validate` rules. That is what keeps the **published
contract from drifting from runtime behavior**.

`Freeze()` checks every declaration at startup and panics on the first problem, so a
misdeclared route fails on boot, not in production. It rejects:

- an empty `ID` or `Method`, or a `Path` that does not start with `/`;
- no `Statuses`, a nil `Handle`, or a `HeaderDoc` without a `Name`;
- a response type whose success status (`204` for `Empty`, `201` for `Created`, `202` for
  `Accepted`, `200` for anything else) is not documented by a non-error `StatusDoc`
  (`Response[T]` is exempt: it picks its status at runtime);
- a duplicate `ID`, or two actions routing the same method and path; and
- two different types that would share a component schema name, a type named `Error`,
  or a `<Name>Input` name that is already taken.

The highlights:

- **One declaration, many artifacts**: handler, schemas, OpenAPI, and docs all come from one typed struct.
- **Production-safe defaults**: panic recovery (always on), a 1 MiB request-body cap, request-id propagation, and JSON `404`/`405` responses. The cap, the id generator, and the 404/405 handlers are configurable.
- **Composable middleware**: registry-wide (`WithMiddleware`) and per-action (`Action.Middleware`), using the standard chi/`net/http` signature, for auth, CORS, logging, and rate limiting.
- **Observability hook**: one `WithObserver` callback receives each action request's action id, status, latency, request id, and error. It is the seam for access logs, metrics, and tracing.
- **Documented auth**: declare OpenAPI `securitySchemes` and per-operation `security` (with `BearerAuth`/`APIKeyAuth` helpers) so the contract states how to authenticate.
- **Pluggable error mapping**: decouple your domain errors from the wire shape with an `ErrorMapper`.
- **Self-documenting**: serve `/openapi.json`, `/openapi.yaml`, and a browsable `/_actions` index straight from the registry.
- **Struct-tag validation, with an escape hatch**: `required`, `min`, `max`, `oneof`, `uuid`, `email`, `e164`, and `rfc3339`, enforced on nested structs, slices, and maps too, and the same parsed rules become the schema's constraints. A `Validatable` interface covers rules a tag can't express.
- **No domain coupling**: the core imports only the standard library, `go-chi/chi/v5`, `google/uuid`, and `gopkg.in/yaml.v3`.

> Why it matters: in 2026, your API contract is consumed by SDK generators, API
> gateways, and AI agents calling tools. A contract generated from the code that
> actually serves traffic is one you never have to hand-reconcile.

<br/>

## 📦 Installation

**go-actions** requires **Go 1.26** or newer.
```shell script
go get -u github.com/mrz1836/go-actions
```

Get the [MAGE-X](https://github.com/mrz1836/mage-x) build tool for development:
```shell script
go install github.com/mrz1836/mage-x/cmd/magex@latest
```

<br/>

## ⚡ Quick Start

### 1. Declare an action

An `Action[Req, Resp]` declares one route. `Req`'s fields bind from the JSON body, or from
the path, query string, and headers through `path`/`query`/`header` tags; `validate` tags
are enforced before your handler runs.

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/mrz1836/go-actions"
)

type CreateUserRequest struct {
	Name  string `json:"name"  validate:"required,max=64"`
	Email string `json:"email" validate:"required,email"`
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createUser() actions.Action[CreateUserRequest, actions.Created[User]] {
	return actions.Action[CreateUserRequest, actions.Created[User]]{
		ID:      "users.create",
		Method:  http.MethodPost,
		Path:    "/users",
		Summary: "Create a user",
		Tags:    []string{"users"},
		Statuses: []actions.StatusDoc{
			{Code: http.StatusCreated, Description: "the created user"},
			{Code: http.StatusBadRequest, Description: "malformed JSON", Error: true},
			{Code: http.StatusUnprocessableEntity, Description: "invalid body", Error: true},
		},
		Handle: func(_ context.Context, req CreateUserRequest) (actions.Created[User], error) {
			u := User{ID: "u_1", Name: req.Name, Email: req.Email}
			return actions.Created[User]{Body: u}, nil
		},
	}
}
```

The envelope sets the **runtime** status: returning `actions.Created[User]` makes the
encoder send `201`. `Statuses` sets the **contract**: it lists the outcomes the OpenAPI
document describes. `Freeze` checks that the two agree, so leaving out the `201`
`StatusDoc` panics at startup. Exported types such as `User` become named component
schemas; the request body is always inlined in its operation.

### 2. Register, freeze, and serve

```go
func main() {
	reg := actions.NewRegistry(actions.WithInfo("Users API", "Manage users.", "1.0.0"))
	actions.Register(reg, createUser())
	reg.Freeze() // checks every declaration and builds the contract

	srv := &http.Server{Addr: ":8080", Handler: reg.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
```

```text
POST /users  {"name":"Ada","email":"ada@example.com"}
201 {"id":"u_1","name":"Ada","email":"ada@example.com"}

POST /users  {"name":"","email":"nope"}
422 {"error":"validation failed: name: is required; email: must be a valid email address","code":"VALIDATION_ERROR","request_id":"…"}

POST /users  {"name":
400 {"error":"malformed JSON body: unexpected EOF","code":"BAD_REQUEST","request_id":"…"}
```

- `Register` is the only typed seam. It uppercases `Method` and compiles the request's
  binding and validation plans once. It panics after `Freeze`.
- `Freeze` is idempotent. `Handler()` panics if it is called before `Freeze`.
- `Handler()` returns an `http.Handler` serving every action at its declared path, plus
  the three self-documentation endpoints.

**Mounting under a prefix.** Declare the full paths, strip the prefix the router
consumes with `WithStripPrefix`, and mount with chi. Here `health` is a small action
constructor, reused in later examples:

```go
type Health struct {
	OK bool `json:"ok"`
}

// health returns a GET action at path answering {"ok": true}.
func health(id, path string) actions.Action[struct{}, Health] {
	return actions.Action[struct{}, Health]{
		ID:       id,
		Method:   http.MethodGet,
		Path:     path,
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "healthy"}},
		Handle:   func(context.Context, struct{}) (Health, error) { return Health{OK: true}, nil },
	}
}
```

```go
reg := actions.NewRegistry(actions.WithStripPrefix("/v1"))
actions.Register(reg, health("health.get", "/v1/health"))
reg.Freeze()

r := chi.NewRouter()
r.Mount("/v1", reg.Handler())
```

The contract's paths then read `/v1/health`. Alternatively, declare short paths
(`/health`), mount the same way, and add `actions.WithServers(actions.Server{URL: "/v1"})`
so clients learn the prefix from `servers` instead. Either way, the self-documentation
endpoints move under the mount point (`/v1/openapi.json`).

### 3. Serve the contract

| Endpoint        | Content-Type                      | Returns                                            |
| --------------- | --------------------------------- | -------------------------------------------------- |
| `/openapi.json` | `application/json; charset=utf-8` | the OpenAPI document as indented JSON              |
| `/openapi.yaml` | `application/yaml; charset=utf-8` | the same document as YAML                          |
| `/_actions`     | `text/html; charset=utf-8`        | a browsable index of every action and its statuses |

- **Markdown index:** `/_actions` answers with `text/markdown; charset=utf-8` when `Accept`
  includes `text/markdown` or `text/plain`.
- **Caching:** the two OpenAPI endpoints send `Cache-Control: public, max-age=300`.
- **Serving:** all three are `GET` routes, built once by `Freeze` and served from memory.
- **Snapshots:** the output is deterministic (actions sorted by `ID`, object keys sorted), so
  it is safe to commit as a snapshot and diff in CI (see
  [`examples/openapi-snapshot`](examples/openapi-snapshot)).
- **Raw bytes:** `reg.OpenAPIJSON()` and `reg.OpenAPIYAML()` return the same bytes; both are
  `nil` before `Freeze`.

<br/>

<details>
<summary><strong><code>Request binding</code></strong></summary>
<br/>

**JSON body.** The body is decoded into `Req` for every method **except `GET` and
`DELETE`**: their bodies are never read, and the contract documents no request body for
them. It is decoded when `Content-Type` is empty or starts with `application/json`,
matched case-insensitively (so `application/json; charset=utf-8` and `Application/JSON`
both work).

- **Other content types:** the body is skipped, with no `415`. Parameters still bind, and
  validation sees zero values for the body fields.
- **Empty body:** an empty body, or `null`, decodes to the zero value.
- **Errors:** a malformed body is a `400 BAD_REQUEST`; one over the size cap is a
  `413 PAYLOAD_TOO_LARGE`.
- **Strictness:** by default, unknown fields and data after the first JSON value are
  ignored, and the 400 message includes the parser's detail. `WithStrictDecoding()`
  rejects both, and answers every malformed body with exactly `malformed JSON body`, so no
  parser internals reach the client:

```go
reg := actions.NewRegistry(actions.WithStrictDecoding())
// {"name":"Ada","email":"ada@example.com","admin":true} → 400 {"error":"malformed JSON body","code":"BAD_REQUEST",…}
// {"name":"Ada","email":"ada@example.com"} {}           → 400 (trailing data)
```

**Parameters.** A top-level exported field tagged `path:"…"`, `query:"…"`, or `header:"…"`
binds from the URL path (chi's `{name}`), the query string, or a request header. If a
field carries more than one of these tags, the first of `path`, `query`, `header` wins.

Tag parameter fields `json:"-"`. Otherwise the field is also a body field: it is
documented in the request-body schema and decoded from the body, and a parameter that is
present overrides it.

```go
type GetUserRequest struct {
	ID     string `json:"-" path:"id" validate:"required"`
	Expand bool   `json:"-" query:"expand"`
	Trace  string `json:"-" header:"X-Trace-Id"`
}

// actions.Action[GetUserRequest, User]{Method: http.MethodGet, Path: "/users/{id}", …}
// GET /users/u_7?expand=maybe → 422 {"error":"validation failed: expand: must be a boolean",…}
```

| Field type | Accepted values | `422` detail for a bad value |
| --- | --- | --- |
| `string` (and named string types) | anything | — |
| `bool` | what `strconv.ParseBool` accepts: `1`, `t`, `true`, `0`, `f`, `false`, … | `must be a boolean` |
| `int`, `int8` … `int64` | base-10 integers that fit the type | `must be an integer` |
| `uint`, `uint8` … `uint64` | base-10 non-negative integers that fit the type | `must be a non-negative integer` |
| `float32`, `float64` | what `strconv.ParseFloat` accepts | `must be a number` |
| `time.Time` | RFC 3339, or a bare `2006-01-02` date (UTC midnight) | `must be an RFC3339 timestamp` |
| a type whose pointer implements `encoding.TextUnmarshaler` (`uuid.UUID`, `netip.Addr`, …) | whatever its `UnmarshalText` accepts | `must be a valid UUID` for `uuid.UUID`, otherwise `has an invalid format` |
| a pointer to any of the above | the same; the field stays `nil` when the parameter is absent | the same |

The checks run in that order: `time.Time` first, then `TextUnmarshaler`, then the kind. So
a named string type with an `UnmarshalText` method binds through that method.

- **Absent and repeated values:** an empty value counts as absent (`?limit=` leaves the
  field untouched), and a repeated query parameter or header binds its first value.
- **Errors:** a parameter that fails to convert is reported on its own, and validation does
  not run. Any other field type (a slice, a map, a plain struct) cannot be a parameter: when one
  receives a value, the request fails with `500 INTERNAL_ERROR`, flagging the declaration
  mistake.
- **Not parameters:** fields inside embedded structs and unexported fields are neither bound
  nor documented as parameters.
- **Request types:** `Req` may be a struct (including `struct{}`) or a pointer to one, which
  is allocated for you. Any other type binds nothing, and the handler receives its zero
  value.

</details>

<details>
<summary><strong><code>Responses & pagination</code></strong></summary>
<br/>

| `Resp` type | Wire status | Body | Document in `Statuses` |
| --- | --- | --- | --- |
| `actions.Empty` | `204` | none | `204` |
| `actions.Created[T]` | `201` | `T` | `201` |
| `actions.Accepted[T]` | `202` | `T` | `202` |
| `actions.Response[T]` | its `Status` (`200` when zero) | `T` (none when `Status` is `204`), plus its `Header` | every status the handler can pick; `Freeze` cannot check these |
| anything else (a struct, a slice, `Page[T]`, `List[T]`, …) | `200` | the value | `200` |

Bodies are encoded with `encoding/json` as `application/json`. If encoding fails, the
response is a `500` with the fixed body
`{"error":"failed to encode response","code":"INTERNAL_ERROR"}`. For headers such as
`Cache-Control` or `ETag`, return a `Response[T]`; its `Header` is added to the response
before the body is written:

```go
Handle: func(_ context.Context, req GetUserRequest) (actions.Response[User], error) {
	return actions.Response[User]{
		Header: http.Header{"Cache-Control": {"max-age=60"}, "Etag": {`"v1"`}},
		Body:   User{ID: req.ID},
	}, nil
},
```

**Collections.** Both of these are ordinary `200` bodies.

- `actions.Page[T]` is the shared cursor-pagination body, with fields `Items`,
  `NextCursor`, and `HasMore`: `{"items": [...], "next_cursor": "...", "has_more": true}`.
  `next_cursor` is omitted when empty.
- `actions.List[T]` is the non-paginated form: `{"items": [...], "meta": {"total": N}}`, where
  `meta` is an `actions.ListMeta`.
  Build it with `actions.NewList(items)`, which sets the total and encodes a nil slice as
  `[]`.

**Pagination helpers.**

- `actions.ClampLimit(limit, def, ceiling)` turns a non-positive `limit` into `def` and caps
  the rest at `ceiling`. The result stays in `[1, ceiling]` as long as `def` does.
- `actions.Paginate(rows, limit, cursorOf)` expects you to fetch `limit+1` rows. It trims
  the extra row, sets `has_more`, and takes `next_cursor` from the last row it kept. A
  non-positive `limit` returns every row as one page.
- Cursor encoding stays yours: `cursorOf` returns whatever opaque string your surface uses.

In this example, `usersAfter` stands in for your store query:

```go
type ListUsersRequest struct {
	Cursor string `json:"-" query:"cursor"`
	Limit  int    `json:"-" query:"limit" validate:"max=100"`
}

Handle: func(_ context.Context, req ListUsersRequest) (actions.Page[User], error) {
	limit := actions.ClampLimit(req.Limit, 2, 100)
	rows := usersAfter(req.Cursor, limit+1) // one extra row detects a next page
	return actions.Paginate(rows, limit, func(u User) string { return u.ID }), nil
},
```

```text
GET /users             → 200 {"items":[{"id":"u_1",…},{"id":"u_2",…}],"next_cursor":"u_2","has_more":true}
GET /users?cursor=u_2  → 200 {"items":[{"id":"u_3",…}],"has_more":false}
```

</details>

<details>
<summary><strong><code>Pluggable error handling</code></strong></summary>
<br/>

Handlers return ordinary Go errors. An `ErrorMapper` translates each one into the
transport-level `APIError`, decoupling the framework from your domain error model:

```go
type APIError struct {
	Status     int
	Code       string
	Message    string
	Fields     []FieldError // flattened into the message on the wire
	RetryAfter time.Duration // > 0 sets the Retry-After header
}

type ErrorMapper func(error) APIError
```

**Every error goes through the mapper**, including the `*APIError`s the framework creates
for bad requests (`400`, `413`, `422`) and timeouts (`504`). The default mapper passes any
`*APIError` through unchanged. It matches with `errors.As`, so a wrapped one counts too.
It maps every other error to a redacted `500`, so internal detail never reaches the wire.
A custom mapper must keep that passthrough:

```go
var ErrUserNotFound = errors.New("user not found")

reg := actions.NewRegistry(actions.WithErrorMapper(func(err error) actions.APIError {
	var apiErr *actions.APIError
	if errors.As(err, &apiErr) {
		return *apiErr // keep framework and handler APIErrors as they are
	}
	if errors.Is(err, ErrUserNotFound) {
		return actions.APIError{Status: http.StatusNotFound, Code: actions.CodeNotFound, Message: "user not found"}
	}
	return actions.APIError{Status: http.StatusInternalServerError, Code: actions.CodeInternal, Message: "an internal error occurred"}
}))
```

> [!WARNING]
> Without the `errors.As(err, &apiErr)` passthrough, every built-in `400`, `413`, `422`,
> and `504` becomes whatever your fallback returns, usually a `500`.

Panics, the default 404/405 responses, and response-encoding failures are written
directly, without the mapper. A mapped `Status` of `0` becomes `500`.

**Wire shape.** Every error body is `{"error": "...", "code": "...", "request_id": "..."}`:

- `request_id` echoes the request's correlation id, and is omitted only when empty.
- Field details are flattened into `error`; there is no separate `fields` array.
- When `Fields` is non-empty, the message is always `validation failed: ` followed by each
  `field: message`, joined by `; `. A detail with an empty `Field` renders as just its
  message.

```json
{"error":"validation failed: name: is required; email: must be a valid email address","code":"VALIDATION_ERROR","request_id":"4f7c…"}
```

**Errors the framework emits itself:**

| Status | `code` | `error` | When |
| --- | --- | --- | --- |
| `400` | `BAD_REQUEST` | `malformed JSON body`, plus `: <parser detail>` outside strict mode | the body is not valid JSON for `Req` (in strict mode, also unknown fields or trailing data) |
| `413` | `PAYLOAD_TOO_LARGE` | `request body too large` | the body exceeds `WithMaxBodyBytes` |
| `422` | `VALIDATION_ERROR` | `validation failed: <field>: <message>; …` | a parameter does not convert, a `validate` rule fails, or `Validate()` reports |
| `504` | `TIMEOUT` | `request timed out` | the handler returned an error after the request context's deadline passed |
| `500` | `INTERNAL_ERROR` | `an internal error occurred` | a handler returned a non-`APIError` error (default mapper), or panicked |
| `500` | `INTERNAL_ERROR` | `unsupported request field type: <kind>` | a parameter field of an unbindable type received a value |
| `500` | `INTERNAL_ERROR` | `failed to encode response` | the response body failed to marshal (this body carries no `request_id`) |
| `404` | `NOT_FOUND` | `resource not found` | no route matches (default handler) |
| `405` | `METHOD_NOT_ALLOWED` | `method not allowed` | the path matches but the method does not (default handler) |

Every mapped `5xx` is logged in full through the default `slog` logger (message
`actions: handler error`), and every recovered panic is logged with its stack
(`actions: handler panic`). The client sees only the mapped message.

**Codes.** The `Code*` constants are a stable wire vocabulary:

| Constant | Value | Constant | Value |
| --- | --- | --- | --- |
| `CodeValidation` | `VALIDATION_ERROR` | `CodePayloadTooLarge` | `PAYLOAD_TOO_LARGE` |
| `CodeBadRequest` | `BAD_REQUEST` | `CodeTooManyRequests` | `TOO_MANY_REQUESTS` |
| `CodeUnauthorized` | `UNAUTHORIZED` | `CodeInternal` | `INTERNAL_ERROR` |
| `CodeForbidden` | `FORBIDDEN` | `CodeMethodNotAllowed` | `METHOD_NOT_ALLOWED` |
| `CodeNotFound` | `NOT_FOUND` | `CodeTimeout` | `TIMEOUT` |
| `CodeConflict` | `CONFLICT` | `CodeServiceUnavailable` | `SERVICE_UNAVAILABLE` |

The framework emits only the codes in the table above; the rest exist so your mappers and
handlers agree on spelling across services.

**Retry-After.** Set `RetryAfter` to tell clients when to try again, typically on a `429`
or `503`. When it is positive, the response carries a `Retry-After` header in whole
seconds, rounded up with a minimum of 1 (so `1500 * time.Millisecond` sends
`Retry-After: 2`). The header is omitted when `RetryAfter` is zero, and the JSON body is
the same either way:

```go
return actions.APIError{
	Status:     http.StatusTooManyRequests,
	Code:       actions.CodeTooManyRequests,
	Message:    "too many requests; please try again later",
	RetryAfter: 30 * time.Second,
}
```

**Documenting headers.** To put a response header in the contract, list it on the
status with `HeaderDoc`. Each one becomes an entry in that response's OpenAPI `headers`
object; an empty `Type` means `string`, and a `HeaderDoc` without a `Name` panics at
`Freeze`. `HeaderDoc` is documentation only: the framework sets no header from it
(`Retry-After` comes from `APIError.RetryAfter`, others from `Response[T].Header` or your
middleware).

```go
Statuses: []actions.StatusDoc{
	{Code: http.StatusOK, Description: "the item"},
	{
		Code: http.StatusTooManyRequests, Description: "rate limited", Error: true,
		Headers: []actions.HeaderDoc{{
			Name:        "Retry-After",
			Description: "Seconds to wait before retrying.",
			Type:        "integer",
			Required:    true,
		}},
	},
},
```

**The `Error` component.** Every `Error: true` status references the `Error` component.
Its `error` and `code` are required, and `request_id` is optional. To give generated
clients a closed set of codes, list the ones your mapper and middleware emit:

```go
reg := actions.NewRegistry(actions.WithErrorCodes("IDENTITY_CONFLICT", "STEP_UP_REQUIRED"))
```

`code` then carries an `enum` of the built-in `Code*` values plus yours, sorted and
de-duplicated. Without `WithErrorCodes`, `code` stays an open string.

**foundationx adapter.** The optional `foundationx` sub-package is a ready-made
`ErrorMapper` for the [`go-foundation`](https://github.com/mrz1836/go-foundation) error
model:

- an `*actions.APIError` passes through;
- `*ValidationError` becomes a `422` carrying its field detail;
- `ErrNotFound` (wrapped or not) becomes a `404`;
- anything else becomes a redacted `500`.

go-foundation is a dependency of the go-actions module, so it is always in your module
graph. It is compiled into your binary only if you import `foundationx`; the core never
imports it.

```go
import "github.com/mrz1836/go-actions/foundationx"

reg := actions.NewRegistry(actions.WithErrorMapper(foundationx.NewErrorMapper()))
```

</details>

<details>
<summary><strong><code>Registry options</code></strong></summary>
<br/>

| Option | Effect |
| --- | --- |
| `WithInfo(title, desc, version)` | Sets the OpenAPI `info` block; the title also names the `_actions` index. An empty argument keeps its default (`API`, `This contract is generated from the action registry.`, `1.0.0`). |
| `WithErrorMapper(mapper)` | Replaces the default error mapper. It must pass `*APIError` through (see error handling). `nil` is ignored. |
| `WithErrorCodes(codes...)` | Enumerates `Error.code` in the contract: the built-in `Code*` values plus `codes`, sorted and de-duplicated. Repeated calls accumulate; empty strings are ignored. |
| `WithStripPrefix(prefix)` | Strips `prefix` from each action's `Path` when routing, for a registry mounted under that prefix. The contract keeps the full paths. |
| `WithMiddleware(mw...)` | Registry-wide middleware on every route (actions, self-docs, `404`/`405`). The first is outermost; repeated calls append. It runs outside request-id and recovery (see middleware). |
| `WithMaxBodyBytes(n)` | Caps the request body (default 1 MiB). `0` disables the cap; a negative value is ignored. A decode that reads past the cap is a `413`. |
| `WithStrictDecoding()` | Rejects JSON bodies with unknown fields or trailing data (`400`, message exactly `malformed JSON body`, no parser detail). |
| `WithObserver(fn)` | Per-request hook with action id, status, latency, request id, and error. `nil` is ignored. |
| `WithRequestIDGenerator(fn)` | Mints the correlation id when none is inbound (default UUIDv4). `nil` is ignored. |
| `WithNotFoundHandler(h)` / `WithMethodNotAllowedHandler(h)` | Replace the JSON `404` / `405` defaults. `nil` is ignored. |
| `WithSecurityScheme(name, scheme)` | Declares an OpenAPI security scheme; the same name again replaces it. |
| `WithSecurity(reqs...)` | Sets the registry-wide default security requirements; repeated calls append. |
| `WithServers(servers...)` | Sets the OpenAPI `servers[]` block; repeated calls append. |
| `WithOpenAPIVersion(v)` | Declares the dialect: `"3.1.0"` (default) or `"3.0.3"`, which also switches the nullable and `[]byte` forms (see schema generation). Any other value panics in `NewRegistry`. |

</details>

<details>
<summary><strong><code>Production middleware & safety</code></strong></summary>
<br/>

A request passes through these layers, outermost first:

```text
WithMiddleware ...                 your registry-wide middleware (the first is outermost)
└─ request id                      reuse or mint the id; set X-Request-ID on request and response
   └─ recover                      backstop: a panic becomes a logged, redacted 500
      └─ router                    chi; /openapi.json, /openapi.yaml, /_actions; 404 / 405
         └─ observer               only with WithObserver, and only for action routes
            └─ recover             a handler panic becomes a 500 the observer sees
               └─ timeout          only when Action.Timeout > 0
                  └─ body cap      only when WithMaxBodyBytes > 0 (default 1 MiB)
                     └─ Action.Middleware ...   (the first is outermost)
                        └─ decode → validate → Handle → encode
```

What follows from that order:

- **`WithMiddleware` runs before the request id exists and outside recovery.** There,
  `actions.RequestIDFromContext` returns `""`, and a panic is not recovered by the
  framework. A request it answers itself (a CORS preflight, an auth rejection, a rate
  limit) gets no `X-Request-ID` header and never reaches the observer.
- **Put CORS in `WithMiddleware`** so it can answer `OPTIONS` preflights; actions do not
  declare `OPTIONS`, so the router would reject them with `405`.
- **The observer sees only action requests**: not the self-documentation endpoints, and
  not `404`/`405` responses.
- **`Action.Middleware` runs before decoding**, so an auth check there rejects a request
  before its body is read. Its first entry is the outermost.
- **Aborts pass through.** A handler that panics with `http.ErrAbortHandler` is re-panicked
  so `net/http` aborts the connection; it is not turned into a `500` and is not observed.
- **Inbound request ids are trusted.** The id comes from `X-Request-ID`, then
  `X-Amzn-Request-Id`, and is otherwise minted (UUIDv4 by default). It is used as-is, with
  no validation or length limit. It is set as the request's `X-Request-ID`, echoed in the
  response header and every error envelope, and available from
  `actions.RequestIDFromContext` and `Observation.RequestID`.
- **Panics are always recovered.** Recovery is always on: the panic is logged with its
  stack, and the client gets a redacted `500`.
- **The body cap only applies when the body is read.** It is enforced as the body is read,
  so a `GET`/`DELETE` or non-JSON body that is never decoded never trips it.

Per-action knobs live on the `Action` struct:

```go
actions.Action[Req, Resp]{
	// ...
	Middleware: []actions.Middleware{authOnly}, // wraps just this route
	Timeout:    2 * time.Second,                // context deadline; an error after it → 504
	Security:   []actions.SecurityRequirement{{"BearerAuth": {"admin"}}},
	Deprecated: true, // marked deprecated in OpenAPI
}
```

**Timeouts.** `Timeout` puts a deadline on the request context, and your handler must
honor `ctx`.

- If the handler returns an **error** once the deadline has passed, the response is
  `504 TIMEOUT`, whatever error it returned. This also applies to a deadline set by
  outer middleware.
- If the handler returns **successfully** after the deadline, its response is encoded as
  usual.

**Observability.** Wire it with one hook:

```go
reg := actions.NewRegistry(actions.WithObserver(func(o actions.Observation) {
	slog.Info("request",
		"action", o.ActionID, "status", o.Status, "request_id", o.RequestID,
		"ms", o.Duration.Milliseconds(), "err", o.Err)
}))
```

The hook is an `actions.ObserveFunc` (`func(actions.Observation)`). It runs synchronously
once the action's handler chain returns, and must not write to the response. Each
`Observation` carries `ActionID`, `Method`, `Path`, `RequestID`, `Status`, `Duration`,
and `Err`:

- `Path`: the declared route pattern (`/users/{id}`), not the request URL.
- `Status`: the first status written.
- `Err`: the unredacted error handed to the mapper, or the recovered panic. That is the
  handler's own error, or the framework's `*APIError` for a decode, validation, or timeout
  failure.

</details>

<details>
<summary><strong><code>Auth & OpenAPI security</code></strong></summary>
<br/>

Declare security schemes once, then require them registry-wide or per action. They
surface in the contract under `components.securitySchemes` and `security`. This example
reuses the `health` helper from the prefix-mounting example:

```go
reg := actions.NewRegistry(
	actions.WithSecurityScheme("BearerAuth", actions.BearerAuth("JWT")),
	actions.WithSecurityScheme("ApiKeyAuth", actions.APIKeyAuth("header", "X-API-Key")),
	actions.WithSecurityScheme("BasicAuth", actions.SecurityScheme{Type: "http", Scheme: "basic"}),
	actions.WithSecurity(actions.SecurityRequirement{"BearerAuth": nil}), // the default for every operation
)
public := health("health.get", "/health")
public.Security = []actions.SecurityRequirement{} // no auth for this operation
admin := health("admin.health", "/admin/health")
admin.Security = []actions.SecurityRequirement{{"ApiKeyAuth": nil}, {"BasicAuth": nil}} // either one
```

`Action.Security` works in three ways:

- **`nil`:** the operation inherits the registry-wide default (no `security` key).
- **Non-empty:** the operation uses its own requirements. Schemes inside one
  `SecurityRequirement` are all required together; separate entries are alternatives.
- **Explicitly empty (`[]actions.SecurityRequirement{}`):** the operation emits
  `security: []`, marking it public even when a default exists.

A requirement's value lists scopes: `{"BearerAuth": {"admin"}}`, where `nil` means none.

Schemes:

- **Helpers:** `BearerAuth(format)` and `APIKeyAuth(in, name)` cover the common cases.
  Basic auth is the struct literal shown above.
- **Fields:** a `SecurityScheme` has a `Type` and a `Description`, `Name` and `In` for
  `apiKey`, and `Scheme` and `BearerFormat` for `http`. Empty fields are omitted.
- **oauth2 / openIdConnect:** `SecurityScheme` has no fields for oauth2 flows or an
  `openIdConnectUrl`, so those schemes cannot be fully described.
- **Names aren't checked:** a requirement that names an undeclared scheme is emitted as
  written.

go-actions does not authenticate requests itself. Wire your auth as `WithMiddleware` or
per-action `Middleware`; the security blocks document *how* clients authenticate so SDK
generators and gateways configure it correctly.

</details>

<details>
<summary><strong><code>Validation tags</code></strong></summary>
<br/>

The `validate` struct tag drives **both** request validation and the schema's
constraints. One parser reads the rules for both sides:

| Rule | Applies to | Runtime check | Schema keyword |
| --- | --- | --- | --- |
| `required` | any field | fails when the value is empty (see below) | the field is listed in `required` |
| `min=N`, `max=N` | strings | length in characters (runes) | `minLength`, `maxLength` |
| | integers, floats | the value | `minimum`, `maximum` |
| | slices, arrays | the number of elements | `minItems`, `maxItems` |
| | maps | the number of entries | `minProperties`, `maxProperties` |
| `oneof=a b c` | strings, integers, floats | equals one of the space-separated values, compared as the field's type | a typed `enum`: `["a","b"]` or `[1,2]` |
| `uuid` | strings | `uuid.Parse` accepts it (also `urn:uuid:…` and braced forms) | `format: uuid` |
| `email` | strings | `net/mail.ParseAddress` accepts it (display names too: `Jane <jane@example.com>`) | `format: email` |
| `e164` | strings | `+`, a non-zero digit, then 1–14 digits | `pattern: ^\+[1-9]\d{1,14}$` |
| `rfc3339` | strings | `time.Parse(time.RFC3339, …)` accepts it | `format: date-time` |

- **Ignored rules.** A rule on a type it does not apply to (`email` on an `int`, `min` on a
  `bool`) is ignored on both sides: neither enforced nor documented. So is a malformed rule
  (a length bound that is not a non-negative integer, a non-finite number, a `oneof` value
  that does not parse as the field's type) and an unknown rule. On a `[]byte`, `min`/`max`
  count bytes at runtime, but the schema (a base64 string) carries no bound.
- **Zero values.** Every rule except `required` skips a non-pointer field holding its zero
  value, so `min=1` on an `int` accepts `0`. `required` fails on an empty string, a slice or
  map with no elements, a zero number, `false`, a nil pointer or interface, and a struct or
  array equal to its zero value (`time.Time{}`, `uuid.Nil`). To require a `bool` to be
  present rather than `true`, use `*bool`.
- **Pointers.** A nil pointer fails `required` and skips every other rule. A non-nil pointer
  satisfies `required`, and all other rules check its pointee, even a zero one. So a `*int`
  with `min=1` rejects an explicit `0`.
- **Nesting.** Rules on the fields of nested structs, pointers to structs, slice and array
  elements, and map values are enforced. Failures name the full path: `address.city`,
  `items[0].name`, `attrs[color].value`, with map values reported in key order. A type with
  its own JSON or text encoding (`MarshalJSON`, `MarshalText`, `time.Time`) is not
  descended into, matching the schema, which documents it as opaque.
- **Field names.** A failure is named after the parameter tag (`limit`, `X-Trace-Id`), else
  the JSON name, else the Go field name. Unexported fields are never bound, so their tags
  are ignored.

For rules a tag can't express, implement `Validatable` (`Validate() error`, on the value
or pointer receiver) on the top-level request type. It is **always** called, even when
tag rules failed, and its failures are appended to theirs in the one `422`:

- an `*APIError` contributes its `Fields`, or its `Message` when it has none;
- any other error becomes a single message-level failure.

Nested types' `Validate` methods are not called.

```go
type TransferRequest struct {
	From   string `json:"from"   validate:"required"`
	To     string `json:"to"     validate:"required"`
	Amount int    `json:"amount" validate:"min=1"`
}

// Validate is always called, and its failures join the tag rules' in one 422.
func (r TransferRequest) Validate() error {
	if r.From != "" && r.From == r.To {
		return &actions.APIError{Fields: []actions.FieldError{{Field: "to", Message: "must differ from from"}}}
	}
	return nil
}

// {"from":"a","to":"a","amount":-5} → 422 "validation failed: amount: must be at least 1; to: must differ from from"
```

```go
type (
	OrderLine struct {
		SKU string `json:"sku" validate:"required"`
		Qty int    `json:"qty" validate:"min=1"`
	}
	OrderRequest struct {
		Lines []OrderLine          `json:"lines" validate:"required,max=10"`
		Notes map[string]OrderLine `json:"notes"`
	}
)

// {"lines":[{"sku":"a","qty":1},{"qty":-1}],"notes":{"gift":{"sku":"b","qty":-2}}} → 422
// "validation failed: lines[1].sku: is required; lines[1].qty: must be at least 1; notes[gift].qty: must be at least 1"
```

</details>

<details>
<summary><strong><code>Schema generation</code></strong></summary>
<br/>

**Components.**

- **Named components:** a struct type that is named, exported, and not generic becomes a
  component (`#/components/schemas/User`), referenced by `$ref`. An anonymous, unexported,
  or generic type (`Page[User]`) is inlined, and so is the top-level request body.
- **Name collisions:** two different types with the same name (say, from two packages)
  panic at `Freeze`; rename one. `Error` is reserved for the framework's error envelope.

**Fields.** The field set follows `encoding/json`'s rules:

- exported fields, named by their `json` tag (a tag without a name keeps the Go name, and
  `"-"` skips the field);
- fields of embedded structs flattened into the parent, including embedded unexported
  structs and embedded pointers;
- the same shadowing rules: the shallowest field wins, then a tagged one, and otherwise the
  name is dropped.

The `,string` tag option is not modeled: such a field is documented with its Go type's
schema.

| Go type | Schema |
| --- | --- |
| `string` / `bool` | `{"type": "string"}` / `{"type": "boolean"}` |
| integer kinds / float kinds | `{"type": "integer"}` / `{"type": "number"}` |
| `time.Time` | `{"type": "string", "format": "date-time"}` |
| `uuid.UUID` | `{"type": "string", "format": "uuid"}` |
| a type with `MarshalJSON`, such as `json.RawMessage` | `{}` (any value) |
| a type with `MarshalText` | `{"type": "string"}` |
| `[]byte` | `{"type": "string", "contentEncoding": "base64"}` in 3.1; `{"type": "string", "format": "byte"}` in 3.0 |
| other slices and arrays, including `[N]byte` | `{"type": "array", "items": …}` |
| `map[K]V` | `{"type": "object", "additionalProperties": …}` |
| a named exported struct | `{"$ref": "#/components/schemas/Name"}` |
| `any`, other interfaces, `func`, `chan` | `{}` |
| `*T` | `T`'s schema (nullable in responses; see below) |

**Requests vs responses.** Request and response schemas follow what each side of the
wire can rely on:

| Field | Request schema | Response schema |
| --- | --- | --- |
| `validate:"required"` | required | required |
| no `omitempty`/`omitzero` | optional | required (the encoder always writes it) |
| `omitempty`/`omitzero` | optional | optional |
| promoted through an embedded pointer | optional | optional (omitted while the pointer is nil) |
| pointer without `omitempty`/`omitzero` | the pointee's schema | required **and** nullable (still nullable, but optional, behind an embedded pointer) |

A nullable field uses the dialect's own form:

- **OpenAPI 3.1:** a type array (`"type": ["string", "null"]`), or
  `"oneOf": [{"$ref": …}, {"type": "null"}]` for a named struct.
- **OpenAPI 3.0** (`WithOpenAPIVersion("3.0.3")`): `"nullable": true`, with a `$ref`
  wrapped in `allOf`.

An `enum` gains `null` too. A schema that already accepts anything, such as an `any` or
`json.RawMessage` field, stays `{}`. So this view:

```go
type UserView struct {
	ID          string  `json:"id"`                   // required
	DisplayName *string `json:"display_name"`         // required, string or null
	Avatar      string  `json:"avatar_url,omitempty"` // optional
}
```

generates `required: ["id", "display_name"]` and
`display_name: {"type": ["string", "null"]}`, which `openapi-typescript` renders as
`{ id: string; display_name: string | null; avatar_url?: string }`.

**Shared types.** A named struct used by both a request and a response can need two
schemas.

- When they differ (directly, or through a nested type that differs), the response schema
  keeps the type's name (`UserView`), and the request schema is emitted as
  `UserViewInput`, with request `$ref`s pointing at it.
- When they are identical, one component serves both.
- Naming another type `<Name>Input` while `<Name>` needs the split panics at `Freeze()`.

**Opaque JSON objects.** A field that carries an arbitrary JSON object, such as a
`json.RawMessage` protocol payload, can be documented as an object instead of `{}`:

```go
type FinishRequest struct {
	Credential json.RawMessage `json:"credential" openapi:"type=object"`
}
```

The override replaces the generated schema with
`{"type": "object", "additionalProperties": true}` (`{ [key: string]: unknown }` in
TypeScript). The field's `validate` constraints are not documented, but they are still
enforced at runtime, and `required` still lists the field.

**Operations.** Each action becomes one operation:

- `operationId` comes from `ID`, alongside `summary` (`Summary`), `description`
  (`Description`, when set), `tags` (`Tags`), `deprecated` (`Deprecated`), and `security`
  (`Security`).
- `parameters` come from the same fields the decoder binds. A path parameter is always
  required; the others are required by `validate:"required"`.
- `requestBody` appears only for methods with a decoded body and a request with body
  fields. It is `required` exactly when some body field is `validate:"required"`, because
  an absent body decodes as zero values, which fail validation only then.
- Each `StatusDoc` becomes a response. An `Error: true` status references the `Error`
  component, and any other status carries the `Resp` body schema (none for `Empty`).
  `Description` defaults to the status text.

</details>

<br/>

## 📚 Documentation

- **API Reference** – Dive into the godocs at [pkg.go.dev/github.com/mrz1836/go-actions](https://pkg.go.dev/github.com/mrz1836/go-actions)
- **Benchmarks** – Check the latest numbers in the [benchmarks](#-benchmarks) section
- **Test Suite** – Review the [unit tests](action_test.go) (powered by [`testify`](https://github.com/stretchr/testify))
- **Examples** – Read the runnable [`Example*` functions](example_test.go) and browse the pet-store API in [`examples/`](examples)

<br/>

<details>
<summary><strong><code>Repository Features</code></strong></summary>
<br/>

This repository includes 25+ built-in features covering CI/CD, security, code quality, developer experience, and community tooling.

**[View the full Repository Features list →](.github/docs/repository-features.md)**

</details>

<details>
<summary><strong><code>Library Deployment</code></strong></summary>
<br/>

This project uses [goreleaser](https://github.com/goreleaser/goreleaser) for streamlined binary and library deployment to GitHub. To get started, install it via:

```bash
brew install goreleaser
```

The release process is defined in the [.goreleaser.yml](.goreleaser.yml) configuration file.


Then create and push a new Git tag using:

```bash
magex version:bump push=true bump=patch branch=master
```

This process ensures consistent, repeatable releases with properly versioned artifacts and metadata.

</details>

<details>
<summary><strong><code>Pre-commit Hooks</code></strong></summary>
<br/>

Set up the Go-Pre-commit System to run the same formatting, linting, and tests defined in [AGENTS.md](.github/AGENTS.md) before every commit:

```bash
go install github.com/mrz1836/go-pre-commit/cmd/go-pre-commit@latest
go-pre-commit install
```

The system is configured via modular env files in [`.github/env/`](.github/env/README.md) and provides 17x faster execution than traditional Python-based pre-commit hooks. See the [complete documentation](http://github.com/mrz1836/go-pre-commit) for details.

</details>

<details>
<summary><strong><code>GitHub Workflows</code></strong></summary>
<br/>

All workflows are driven by modular configuration in [`.github/env/`](.github/env/README.md) — no YAML editing required.

**[View all workflows and the control center →](.github/docs/workflows.md)**

</details>

<details>
<summary><strong><code>Updating Dependencies</code></strong></summary>
<br/>

To update all dependencies (Go modules, linters, and related tools), run:

```bash
magex deps:update
```

This command ensures all dependencies are brought up to date in a single step, including Go modules and any tools managed by [MAGE-X](https://github.com/mrz1836/mage-x). It is the recommended way to keep your development environment and CI in sync with the latest versions.

</details>

<details>
<summary><strong><code>Build Commands</code></strong></summary>
<br/>

View all build commands

```bash script
magex help
```

</details>

<br/>

## 🧪 Examples & Tests

All unit tests run via [GitHub Actions](https://github.com/mrz1836/go-actions/actions) and use [Go version 1.26.x](https://go.dev/doc/go1.26). View the [configuration file](.github/workflows/fortress.yml).

- [`example_test.go`](example_test.go): runnable `Example*` functions, compiled and checked against their output by `go test`. The README's code blocks come from them, and they're also shown on [pkg.go.dev](https://pkg.go.dev/github.com/mrz1836/go-actions#pkg-examples).
- [`examples/main.go`](examples/main.go): a server for the pet-store registry, with every action plus `/openapi.json`, `/openapi.yaml`, and `/_actions`. It listens on `:8080`; set `ADDR` to override.
- [`examples/openapi-snapshot`](examples/openapi-snapshot): a contract-drift guard. `write` (the default) saves the pet-store contract as `openapi.snapshot.json` in the current directory, and `check` exits non-zero when the generated contract no longer matches it. No snapshot is committed here; commit one in your project and run `check` in CI.
- [`examples/petstore`](examples/petstore): the registry both examples share, with a list action bound from a query parameter, a get bound from a path parameter, and a create with a validated body.

Run the example server:

```bash script
cd examples && go run .
# then browse http://localhost:8080/_actions
```

The `actiontest` helper exercises actions through the real pipeline or directly:

```go
import "github.com/mrz1836/go-actions/actiontest"

// Freeze reg and serve reg.Handler() at the root of an httptest.Server (closed on cleanup):
srv := actiontest.NewServer(t, reg)

// Or call Handle directly with context.Background(), skipping decode, validate,
// encode, and all middleware:
resp, err := actiontest.Invoke(t, createUser(), CreateUserRequest{Name: "Ada", Email: "ada@example.com"})
```

Run all tests (fast):

```bash script
magex test
```

Run all tests with race detector (slower):
```bash script
magex test:race
```

<br/>

## 📊 Benchmarks

Run the Go benchmarks:

```bash script
magex bench
```

Medians of `go test -run '^$' -bench . -benchmem -count 6`, measured with Go 1.27.1 on darwin/arm64 (Apple M1 Max):

| Benchmark | What it measures | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| `Handler/create_201` | one request through `Handler()`: routing, middleware, decode, validate, handle, encode | 3,732 | 8,305 | 49 |
| `Handler/validation_422` | the same, rejected by validation | 4,337 | 8,974 | 63 |
| `Handler/get_params_200` | the same, binding path, query, and header parameters | 3,230 | 8,461 | 43 |
| `Handler/not_found_404` | an unmatched route | 2,736 | 7,676 | 36 |
| `Handler/with_observer` | `create_201` with a `WithObserver` hook | 3,964 | 8,723 | 53 |
| `DecodeRequest` | decoding a small JSON body (request construction included) | 1,640 | 6,022 | 18 |
| `DecodeRequest_Params` | binding five path, query, and header parameters | 433 | 513 | 7 |
| `ValidateRequest` | six fields covering the whole rule vocabulary | 329 | 88 | 5 |
| `ValidateRequest_Nested` | a slice of ten structs with rules | 644 | 320 | 1 |
| `EncodeResponse/created` | encoding a `Created[T]` body | 588 | 1,080 | 12 |
| `WriteAPIError` | writing a `422` with two field details | 837 | 1,545 | 16 |
| `Paginate/has_more` | trimming a page and deriving its cursor | 13.8 | 0 | 0 |
| `ClampLimit/500` | clamping a page size | 2.2 | 0 | 0 |
| `BuildOpenAPI` | registering and freezing a two-action registry (once per process) | 138,328 | 223,210 | 1,160 |
| `BuildOpenAPIPetstore` | building and freezing the pet-store registry (once per process) | 183,542 | 249,952 | 1,579 |

> The `Handler` and `DecodeRequest` rows include building the `*http.Request` (and, for
> `Handler`, the response recorder). The per-request hot path is decode, validate, and
> encode through the typed pipeline.
> Binding and validation plans are compiled once per request type, so a request does no
> struct-tag parsing. Contract generation runs once, at `Freeze`.

<br/>

## 🧰 Code Standards
Read more about this Go project's [code standards](.github/CODE_STANDARDS.md).

<br/>

## 🤖 AI Usage & Assistant Guidelines
Read the [AI Usage & Assistant Guidelines](.github/tech-conventions/ai-compliance.md) for details on how AI is used in this project and how to interact with the AI assistants.

<br/>

## 👥 Maintainers
| [<img src="https://github.com/mrz1836.png" height="50" width="50" alt="MrZ" />](https://github.com/mrz1836) |
|:-----------------------------------------------------------------------------------------------------------:|
|                                      [MrZ](https://github.com/mrz1836)                                      |

<br/>

## 🤝 Contributing
View the [contributing guidelines](.github/CONTRIBUTING.md) and please follow the [code of conduct](.github/CODE_OF_CONDUCT.md).

### How can I help?
All kinds of contributions are welcome :raised_hands:!
The most basic way to show your support is to star :star2: the project, or to raise issues :speech_balloon:.
You can also support this project by [becoming a sponsor on GitHub](https://github.com/sponsors/mrz1836) :clap:
or by making a [**bitcoin donation**](https://mrz1818.com/?tab=tips&utm_source=github&utm_medium=sponsor-link&utm_campaign=go-actions&utm_term=go-actions&utm_content=go-actions) to ensure this journey continues indefinitely! :rocket:

[![Stars](https://img.shields.io/github/stars/mrz1836/go-actions?label=Please%20like%20us&style=social&v=1)](https://github.com/mrz1836/go-actions/stargazers)

<br/>

## 📝 License

[![License](https://img.shields.io/github/license/mrz1836/go-actions.svg?style=flat&v=1)](LICENSE)
