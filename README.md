# go-actions

> Typed HTTP action framework with OpenAPI 3.1 generation for [chi](https://github.com/go-chi/chi).

[![Go Reference](https://pkg.go.dev/badge/github.com/mrz1836/go-actions.svg)](https://pkg.go.dev/github.com/mrz1836/go-actions)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

You declare a route **once** as a typed `Action[Req, Resp]`, and a `Registry`
turns each declaration — all from the **same reflected types** — into:

- an `http.HandlerFunc` (decode → validate → handle → encode),
- a JSON Schema 2020-12,
- an OpenAPI 3.1 document (JSON **and** YAML), and
- a browsable HTML/Markdown action index.

Because every artifact derives from the same types, the published contract
cannot drift from runtime behavior. `Freeze()` enforces invariants at startup —
unique IDs, unique method+path, and documented statuses.

## Features

- **One declaration, many artifacts** — handler, schema, OpenAPI, and docs all
  generated from one typed struct.
- **No domain coupling** — the core imports only the standard library,
  `go-chi/chi/v5`, `google/uuid`, and `gopkg.in/yaml.v3`.
- **Pluggable error mapping** — decouple your domain errors from the wire shape
  with an `ErrorMapper`.
- **Self-documenting** — serve `/openapi.json`, `/openapi.yaml`, and a browsable
  `/_actions` index straight from the registry.
- **Struct-tag validation** — `required`, `min`, `max`, `oneof`, `uuid`,
  `email`, `e164`, and `rfc3339`, with the same tags feeding the JSON Schema.

## Install

```bash
go get github.com/mrz1836/go-actions
```

## Quick start

### 1. Declare an action

An `Action[Req, Resp]` declares one route. Request fields bind from the JSON
body or from `path`/`query`/`header` tags; `validate` tags are enforced before
your handler runs.

```go
package main

import (
	"context"
	"net/http"

	"github.com/mrz1836/go-actions"
)

type createUserReq struct {
	Name  string `json:"name"  validate:"required,min=1,max=64"`
	Email string `json:"email" validate:"required,email"`
}

type user struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createUser() actions.Action[createUserReq, actions.Created[user]] {
	return actions.Action[createUserReq, actions.Created[user]]{
		ID:      "users.create",
		Method:  http.MethodPost,
		Path:    "/users",
		Summary: "Create a user",
		Tags:    []string{"users"},
		Statuses: []actions.StatusDoc{
			{Code: http.StatusCreated, Description: "the created user"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid body", Error: true},
		},
		Handle: func(_ context.Context, req createUserReq) (actions.Created[user], error) {
			u := user{ID: "u_1", Name: req.Name, Email: req.Email}
			return actions.Created[user]{Body: u}, nil
		},
	}
}
```

Response envelopes set the documented status: `actions.Empty` → 204,
`actions.Created[T]` → 201, `actions.Accepted[T]` → 202. Returning any other
value encodes as 200.

### 2. Register, freeze, and mount

```go
func main() {
	reg := actions.NewRegistry(actions.WithInfo(
		"Users API",
		"Manage users.",
		"1.0.0",
	))

	actions.Register(reg, createUser())
	reg.Freeze() // validates declarations and builds the contract artifacts

	http.Handle("/", reg.Handler())
	_ = http.ListenAndServe(":8080", nil)
}
```

`Register` is the only typed seam — after `Freeze()` the registry is sealed and
further `Register` calls panic. `Handler()` returns an `http.Handler` mounting
every action plus the self-documentation endpoints.

### 3. Serve the contract

`Handler()` mounts three self-documenting endpoints automatically:

| Endpoint         | Returns                                              |
| ---------------- | --------------------------------------------------- |
| `/openapi.json`  | the OpenAPI 3.1 document as JSON                     |
| `/openapi.yaml`  | the same document as YAML                            |
| `/_actions`      | a browsable HTML index (Markdown via `Accept`)      |

The `/_actions` index ships in core and is always on — no build tag required.
You can also reach the raw bytes directly with `reg.OpenAPIJSON()` and
`reg.OpenAPIYAML()` (for example, to write a committed snapshot).

## Pluggable error handling

Handlers return ordinary Go errors. An `ErrorMapper` translates them into the
transport-level `APIError`, decoupling the framework from your domain error
model:

```go
type APIError struct {
	Status  int
	Code    string
	Message string
	Fields  []FieldError
}

type ErrorMapper func(error) APIError
```

Install one with `WithErrorMapper`:

```go
reg := actions.NewRegistry(actions.WithErrorMapper(func(err error) actions.APIError {
	if errors.Is(err, ErrNotFound) {
		return actions.APIError{
			Status:  http.StatusNotFound,
			Code:    actions.CodeNotFound,
			Message: "resource not found",
		}
	}
	return actions.APIError{
		Status:  http.StatusInternalServerError,
		Code:    actions.CodeInternal,
		Message: "an internal error occurred",
	}
}))
```

The default mapper passes an `*APIError` through unchanged (so handlers may
return one directly) and maps every other error to a redacted 500, ensuring
internal detail never reaches the wire. The error envelope is always
`{"error": ..., "code": ..., "request_id": ...}`.

### foundationx adapter

The optional `foundationx` sub-package provides a ready-made `ErrorMapper` that
wires the [`go-foundation`](https://github.com/mrz1836/go-foundation) error
model (`*ValidationError` → 422, `ErrNotFound` → 404). It lives in its own
package so consumers that do not use `go-foundation` never pull it into their
module graph:

```go
import "github.com/mrz1836/go-actions/foundationx"

reg := actions.NewRegistry(actions.WithErrorMapper(foundationx.NewErrorMapper()))
```

## Options

| Option                          | Effect                                                              |
| ------------------------------- | ------------------------------------------------------------------- |
| `WithInfo(title, desc, version)`| Sets the OpenAPI `info` block; the title also names the `_actions` index. |
| `WithErrorMapper(mapper)`       | Installs a custom error mapper (replaces the default generic one).  |
| `WithStripPrefix(prefix)`       | Strips a namespace prefix from each action's `Path` when routing (e.g. when the registry is mounted under that prefix). |

## Validation tags

The `validate` struct tag drives both request validation and the generated JSON
Schema constraints. Supported rules:

`required`, `min=N`, `max=N`, `oneof=a b c`, `uuid`, `email`, `e164`, `rfc3339`.

For strings and slices, `min`/`max` bound the length; for numbers they bound the
value. Format rules (`uuid`, `email`, etc.) are skipped on an empty value — use
`required` to reject emptiness.

## Testing

The `actiontest` helper exercises actions through the real pipeline or directly:

```go
import "github.com/mrz1836/go-actions/actiontest"

// Spin up a test server running the full decode/validate/encode pipeline:
srv := actiontest.NewServer(t, reg)

// Or invoke a handler directly, bypassing decode/validate/encode:
resp, err := actiontest.Invoke(t, createUser(), createUserReq{Name: "Ada", Email: "ada@example.com"})
```

## Examples

The [`examples/`](examples) directory contains a runnable pet-store API:

- [`examples/main.go`](examples/main.go) — a server declaring actions, mounting
  `Handler()`, and serving `/openapi.json`, `/openapi.yaml`, and `/_actions`.
- [`examples/openapi-snapshot`](examples/openapi-snapshot) — a contract-drift
  guard that writes or `check`s a committed OpenAPI snapshot (wire `check` into
  CI to fail the build when the generated contract drifts).
- [`examples/petstore`](examples/petstore) — the shared registry both examples use.

Run the server:

```bash
cd examples && go run .
# then browse http://localhost:8080/_actions
```

## License

[MIT](LICENSE)
