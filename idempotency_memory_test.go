package actions_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
)

func TestMemoryIdempotencyStoreClaimsOnceUntilReleased(t *testing.T) {
	ctx := t.Context()
	store := actions.NewMemoryIdempotencyStore()

	token, claimed, err := store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.NotEmpty(t, token)

	again, claimedAgain, err := store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.False(t, claimedAgain, "a held key is not claimed twice")
	assert.Empty(t, again)

	_, claimedOther, err := store.Claim(ctx, "other", time.Minute)
	require.NoError(t, err)
	assert.True(t, claimedOther, "each key has its own claim")

	require.NoError(t, store.Release(ctx, "k", token))
	next, claimedNext, err := store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.True(t, claimedNext, "a released key is claimed again")
	assert.NotEqual(t, token, next, "each claim has its own token")
}

func TestMemoryIdempotencyStoreReleasesOnlyWithItsToken(t *testing.T) {
	ctx := t.Context()
	store := actions.NewMemoryIdempotencyStore()

	token, _, err := store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)

	require.NoError(t, store.Release(ctx, "k", "not-the-token"))
	_, claimed, err := store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.False(t, claimed, "another token leaves the claim held")

	require.NoError(t, store.Release(ctx, "k", token))
	_, claimed, err = store.Claim(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.True(t, claimed, "the claim's own token releases it")

	require.NoError(t, store.Release(ctx, "missing", "any-token"), "releasing a key that isn't held is no error")
}

func TestMemoryIdempotencyStoreForgetsAfterTheTTL(t *testing.T) {
	ctx := t.Context()
	store := actions.NewMemoryIdempotencyStore()

	saved := actions.StoredResponse{
		Fingerprint: "fp",
		Status:      http.StatusCreated,
		Header:      http.Header{"X-Order": {"1"}},
		Body:        []byte(`{"number":1}`),
	}
	require.NoError(t, store.Save(ctx, "k", saved, 50*time.Millisecond))
	got, found, err := store.Lookup(ctx, "k")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, saved, got)
	require.Eventually(t, func() bool {
		_, stillFound, lookupErr := store.Lookup(ctx, "k")
		return lookupErr == nil && !stillFound
	}, 2*time.Second, 10*time.Millisecond, "a saved response is forgotten after its TTL")

	_, claimed, err := store.Claim(ctx, "c", 50*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, claimed)
	require.Eventually(t, func() bool {
		_, claimedAgain, claimErr := store.Claim(ctx, "c", time.Minute)
		return claimErr == nil && claimedAgain
	}, 2*time.Second, 10*time.Millisecond, "a claim ends after its TTL")

	_, found, err = store.Lookup(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, found)
}
