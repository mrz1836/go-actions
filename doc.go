// Package actions is a typed HTTP action framework with OpenAPI 3.1 generation
// for chi. You declare a route once as a typed Action[Req, Resp] and a Registry
// derives the http.Handler, JSON Schema, OpenAPI document, and a browsable index
// from the same reflected types, so the published contract cannot drift from
// runtime behavior.
//
// This is the initial module scaffold; the framework implementation is added in
// the extraction phases that follow.
package actions
