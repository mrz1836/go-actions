// Package foundationx adapts the github.com/mrz1836/go-foundation error model to
// the go-actions ErrorMapper seam. It is an optional adapter: importing it pulls
// go-foundation into your module graph, so the go-actions core never imports it.
// Wire it with actions.WithErrorMapper(foundationx.NewErrorMapper()).
package foundationx
