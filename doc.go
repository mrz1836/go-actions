// Package actions is a typed HTTP action framework with OpenAPI generation for
// chi. You declare a route once as a typed Action[Req, Resp] and a Registry
// turns it into an http.HandlerFunc (decode → validate → handle → encode), an
// OpenAPI document (3.1 by default, or 3.0) whose component schemas are JSON
// Schema, served as JSON and YAML, and a browsable HTML/Markdown index — all
// derived from the same reflected types and the same parsed struct tags, so the
// published contract cannot drift from runtime behavior. Freeze checks every
// declaration at startup, including that each action documents the success
// status its response type produces.
//
// The core depends only on the standard library, github.com/go-chi/chi/v5,
// github.com/google/uuid, and gopkg.in/yaml.v3. It carries no domain types:
// error mapping is pluggable via ErrorMapper (see WithErrorMapper) and the
// OpenAPI info block is caller-configurable (see WithInfo).
package actions
