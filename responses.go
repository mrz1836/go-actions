package actions

// Empty is the response envelope for an action that returns no response body.
// It marshals to an empty JSON object. Created[T] and Accepted[T] envelopes are
// added alongside the registry during extraction.
type Empty struct{}
