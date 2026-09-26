// Package foundationx adapts the github.com/mrz1836/go-foundation error model to
// the go-actions ErrorMapper seam. It is an optional adapter: go-foundation is a
// dependency of the go-actions module, so it is always in your module graph, but
// it is compiled into your binary only when you import foundationx — the
// go-actions core never imports it. Wire it with
// actions.WithErrorMapper(foundationx.NewErrorMapper()).
package foundationx
