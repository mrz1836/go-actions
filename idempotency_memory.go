package actions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// NewMemoryIdempotencyStore returns an IdempotencyStore that keeps its claims
// and saved responses in this process's memory, safe for concurrent use. It is
// for tests and single-process servers: each process has its own, so a service
// that runs several needs a store they share. A claim is held until it is
// released with its token or its TTL ends; a saved response is returned until
// its TTL ends; expired entries are removed on each Claim and Save.
func NewMemoryIdempotencyStore() IdempotencyStore {
	return &memoryIdempotencyStore{
		claims:    map[string]memoryClaim{},
		responses: map[string]memoryResponse{},
	}
}

// memoryIdempotencyStore is NewMemoryIdempotencyStore's store.
type memoryIdempotencyStore struct {
	mu        sync.Mutex
	claims    map[string]memoryClaim
	responses map[string]memoryResponse
}

// memoryClaim is a held key: the token that releases it, and when it ends.
type memoryClaim struct {
	token   string
	expires time.Time
}

// memoryResponse is a saved response, and when it is forgotten.
type memoryResponse struct {
	resp    StoredResponse
	expires time.Time
}

// Lookup returns the response saved under key while its TTL lasts.
func (m *memoryIdempotencyStore) Lookup(_ context.Context, key string) (StoredResponse, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	saved, ok := m.responses[key]
	if !ok || !time.Now().Before(saved.expires) {
		return StoredResponse{}, false, nil
	}
	return cloneStoredResponse(saved.resp), true, nil
}

// Claim holds key for ttl, unless a claim on it is still held.
func (m *memoryIdempotencyStore) Claim(_ context.Context, key string, ttl time.Duration) (string, bool, error) {
	token := newClaimToken()
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropExpired(now)
	if _, held := m.claims[key]; held {
		return "", false, nil
	}
	m.claims[key] = memoryClaim{token: token, expires: now.Add(ttl)}
	return token, true, nil
}

// Save keeps a copy of resp under key for ttl.
func (m *memoryIdempotencyStore) Save(_ context.Context, key string, resp StoredResponse, ttl time.Duration) error {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropExpired(now)
	m.responses[key] = memoryResponse{resp: cloneStoredResponse(resp), expires: now.Add(ttl)}
	return nil
}

// Release drops key's claim when token is its own; any other token, or a key
// that isn't held, changes nothing.
func (m *memoryIdempotencyStore) Release(_ context.Context, key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if claim, ok := m.claims[key]; ok && claim.token == token {
		delete(m.claims, key)
	}
	return nil
}

// dropExpired removes the claims and responses whose TTL has ended. The
// caller holds m.mu.
func (m *memoryIdempotencyStore) dropExpired(now time.Time) {
	for key, claim := range m.claims {
		if !now.Before(claim.expires) {
			delete(m.claims, key)
		}
	}
	for key, saved := range m.responses {
		if !now.Before(saved.expires) {
			delete(m.responses, key)
		}
	}
}

// newClaimToken returns a random token for one claim: 16 bytes, in hex.
func newClaimToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand's Read never fails
	return hex.EncodeToString(b)
}

// cloneStoredResponse copies resp's header and body, so the store never shares
// them with a caller.
func cloneStoredResponse(resp StoredResponse) StoredResponse {
	resp.Header = resp.Header.Clone()
	if resp.Body != nil {
		resp.Body = append([]byte(nil), resp.Body...)
	}
	return resp
}
