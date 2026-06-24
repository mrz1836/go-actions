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
       <a href="https://goreportcard.com/report/github.com/mrz1836/go-actions"><img src="https://goreportcard.com/badge/github.com/mrz1836/go-actions?style=flat-square" alt="Go Report"></a>
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
      🛠️&nbsp;<a href="#-code-standards"><code>Code&nbsp;Standards</code></a>
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
       ⚖️&nbsp;<a href="#-license"><code>License</code></a>
    </td>
    <td align="center">
       👥&nbsp;<a href="#-maintainers"><code>Maintainers</code></a>
    </td>
  </tr>
</table>
<br/>

## 🧩 About

**go-actions** is a typed HTTP action framework with **OpenAPI 3.1 generation** for
[chi](https://github.com/go-chi/chi). You declare a route **once** as a typed
`Action[Req, Resp]`, and a `Registry` turns each declaration — all from the **same
reflected types** — into:

- an `http.HandlerFunc` (decode → validate → handle → encode),
- a JSON Schema 2020-12,
- an OpenAPI 3.1 document (JSON **and** YAML), and
- a browsable HTML/Markdown action index.

Because every artifact derives from the same types, the **published contract cannot
drift from runtime behavior**. `Freeze()` enforces invariants at startup — unique IDs,
unique method+path, and documented statuses — so a misconfigured route fails on boot,
not in production.

- **One declaration, many artifacts** — handler, schema, OpenAPI, and docs all generated from one typed struct.
- **No domain coupling** — the core imports only the standard library, `go-chi/chi/v5`, `google/uuid`, and `gopkg.in/yaml.v3`.
- **Pluggable error mapping** — decouple your domain errors from the wire shape with an `ErrorMapper`.
- **Self-documenting** — serve `/openapi.json`, `/openapi.yaml`, and a browsable `/_actions` index straight from the registry.
- **Struct-tag validation** — `required`, `min`, `max`, `oneof`, `uuid`, `email`, `e164`, and `rfc3339`, with the same tags feeding the JSON Schema.

> Why it matters: in 2026, your API contract is consumed by SDK generators, API
> gateways, and AI agents calling tools. A contract generated from the code that
> actually serves traffic is one you never have to hand-reconcile.

<br/>

## 📦 Installation

**go-actions** requires a [supported release of Go](https://golang.org/doc/devel/release.html#policy).
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

An `Action[Req, Resp]` declares one route. Request fields bind from the JSON body or
from `path`/`query`/`header` tags; `validate` tags are enforced before your handler runs.

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
`actions.Created[T]` → 201, `actions.Accepted[T]` → 202. Returning any other value
encodes as 200.

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

`Register` is the only typed seam — after `Freeze()` the registry is sealed and further
`Register` calls panic. `Handler()` returns an `http.Handler` mounting every action plus
the self-documentation endpoints.

### 3. Serve the contract

`Handler()` mounts three self-documenting endpoints automatically:

| Endpoint         | Returns                                              |
| ---------------- | --------------------------------------------------- |
| `/openapi.json`  | the OpenAPI 3.1 document as JSON                     |
| `/openapi.yaml`  | the same document as YAML                            |
| `/_actions`      | a browsable HTML index (Markdown via `Accept`)      |

The `/_actions` index ships in core and is always on — no build tag required. You can
also reach the raw bytes directly with `reg.OpenAPIJSON()` and `reg.OpenAPIYAML()` (for
example, to write a committed snapshot).

<br/>

<details>
<summary><strong><code>Pluggable error handling</code></strong></summary>
<br/>

Handlers return ordinary Go errors. An `ErrorMapper` translates them into the
transport-level `APIError`, decoupling the framework from your domain error model:

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

The default mapper passes an `*APIError` through unchanged (so handlers may return one
directly) and maps every other error to a redacted 500, ensuring internal detail never
reaches the wire. The error envelope is always
`{"error": ..., "code": ..., "request_id": ...}`.

**foundationx adapter** — the optional `foundationx` sub-package provides a ready-made
`ErrorMapper` that wires the [`go-foundation`](https://github.com/mrz1836/go-foundation)
error model (`*ValidationError` → 422, `ErrNotFound` → 404). It lives in its own package
so consumers that do not use `go-foundation` never pull it into their module graph:

```go
import "github.com/mrz1836/go-actions/foundationx"

reg := actions.NewRegistry(actions.WithErrorMapper(foundationx.NewErrorMapper()))
```

</details>

<details>
<summary><strong><code>Registry options</code></strong></summary>
<br/>

| Option                          | Effect                                                              |
| ------------------------------- | ------------------------------------------------------------------- |
| `WithInfo(title, desc, version)`| Sets the OpenAPI `info` block; the title also names the `_actions` index. |
| `WithErrorMapper(mapper)`       | Installs a custom error mapper (replaces the default generic one).  |
| `WithStripPrefix(prefix)`       | Strips a namespace prefix from each action's `Path` when routing (e.g. when the registry is mounted under that prefix). |

</details>

<details>
<summary><strong><code>Validation tags</code></strong></summary>
<br/>

The `validate` struct tag drives **both** request validation and the generated JSON
Schema constraints. Supported rules:

`required`, `min=N`, `max=N`, `oneof=a b c`, `uuid`, `email`, `e164`, `rfc3339`.

For strings and slices, `min`/`max` bound the length; for numbers they bound the value.
Format rules (`uuid`, `email`, etc.) are skipped on an empty value — use `required` to
reject emptiness.

</details>

<br/>

## 📚 Documentation

- **API Reference** – Dive into the godocs at [pkg.go.dev/github.com/mrz1836/go-actions](https://pkg.go.dev/github.com/mrz1836/go-actions)
- **Benchmarks** – Check the latest numbers in the [benchmarks](#-benchmarks) section
- **Test Suite** – Review the [unit tests](action_test.go) (powered by [`testify`](https://github.com/stretchr/testify))
- **Examples** – Browse the runnable pet-store API in [`examples/`](examples)

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

All unit tests run via [GitHub Actions](https://github.com/mrz1836/go-actions/actions) and use [Go version 1.25.x](https://go.dev/doc/go1.25). View the [configuration file](.github/workflows/fortress.yml).

The [`examples/`](examples) directory contains a runnable pet-store API:

- [`examples/main.go`](examples/main.go) — a server declaring actions, mounting `Handler()`, and serving `/openapi.json`, `/openapi.yaml`, and `/_actions`.
- [`examples/openapi-snapshot`](examples/openapi-snapshot) — a contract-drift guard that writes or `check`s a committed OpenAPI snapshot (wire `check` into CI to fail the build when the generated contract drifts).
- [`examples/petstore`](examples/petstore) — the shared registry both examples use.

Run the example server:

```bash script
cd examples && go run .
# then browse http://localhost:8080/_actions
```

The `actiontest` helper exercises actions through the real pipeline or directly:

```go
import "github.com/mrz1836/go-actions/actiontest"

// Spin up a test server running the full decode/validate/encode pipeline:
srv := actiontest.NewServer(t, reg)

// Or invoke a handler directly, bypassing decode/validate/encode:
resp, err := actiontest.Invoke(t, createUser(), createUserReq{Name: "Ada", Email: "ada@example.com"})
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

> Benchmarks cover the hot path — request decoding and response encoding through the typed pipeline.

<br/>

## 🛠️ Code Standards
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
