// Package actiontest provides test helpers for the action framework: Invoke
// runs an action's Handle directly, and NewServer freezes a registry and
// returns an httptest.Server exercising the full decode/validate/encode
// pipeline.
package actiontest
