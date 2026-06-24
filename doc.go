// Package actions is a typed HTTP action framework with OpenAPI 3.1 generation
// for chi. You declare a route once as a typed Action[Req, Resp] and a Registry
// turns it into an http.HandlerFunc (decode → validate → handle → encode), a
// JSON Schema 2020-12, an OpenAPI 3.1 document (JSON and YAML), and a browsable
// HTML/Markdown index — all derived from the same reflected types, so the
// published contract cannot drift from runtime behavior.
//
// The core depends only on the standard library, github.com/go-chi/chi/v5,
// github.com/google/uuid, and gopkg.in/yaml.v3. It carries no domain types:
// error mapping is pluggable via ErrorMapper (see WithErrorMapper) and the
// OpenAPI info block is caller-configurable (see WithInfo).
package actions
