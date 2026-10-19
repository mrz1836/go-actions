package actions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// IdempotencyStore keeps what the Idempotency-Key step needs between requests
// (see WithIdempotency): a claim on a key while its first request runs, and the
// response saved under it afterwards. Keys arrive hashed, already scoped to
// their caller. An implementation must be safe for concurrent use; one shared
// by several processes (over a cache or a database) makes a retry that reaches
// another process replay too.
type IdempotencyStore interface {
	// Lookup returns the response saved under key, and whether there is one.
	Lookup(ctx context.Context, key string) (StoredResponse, bool, error)
	// Claim holds key for ttl while its first request runs, and returns a
	// token that releases it. When another request holds key, it returns
	// claimed false and a nil error.
	Claim(ctx context.Context, key string, ttl time.Duration) (token string, claimed bool, err error)
	// Save keeps resp under key for ttl.
	Save(ctx context.Context, key string, resp StoredResponse, ttl time.Duration) error
	// Release drops key's claim, only when token is the one its Claim
	// returned.
	Release(ctx context.Context, key, token string) error
}

// StoredResponse is a saved response: the fingerprint of the request that made
// it (its method, request URI, and body), and the status, headers, and body it
// was answered with.
type StoredResponse struct {
	Fingerprint string      `json:"fingerprint"`
	Status      int         `json:"status"`
	Header      http.Header `json:"header,omitempty"`
	Body        []byte      `json:"body,omitempty"`
}

// IdempotencyOptions configures WithIdempotency.
type IdempotencyOptions struct {
	// Scope returns the caller a request's key belongs to, such as its user or
	// API key, so that one caller's key never replays another's response. It
	// runs after the action's own Middleware, so it sees what that middleware
	// put on the request. Every request it returns one value for shares one
	// namespace of keys. Required.
	Scope func(*http.Request) string
	// TTL is how long a saved response is kept. Zero or negative is 24 hours.
	TTL time.Duration
	// InFlightTTL is how long a claim holds a key while its first request
	// runs, at most. Zero or negative is 1 minute.
	InFlightTTL time.Duration
	// MaxBody is the largest response body saved, in bytes; a larger response
	// is sent but not saved. Zero or negative is 1 MiB.
	MaxBody int64
}

// IdempotencyKeyUse says whether an action takes an Idempotency-Key header
// (see WithIdempotency).
type IdempotencyKeyUse uint8

const (
	// IdempotencyKeyOff takes no key; the header is ignored. It is the zero
	// value.
	IdempotencyKeyOff IdempotencyKeyUse = iota
	// IdempotencyKeyOptional runs a request without the header as usual, and
	// one with it at most once per key.
	IdempotencyKeyOptional
	// IdempotencyKeyRequired refuses a request without the header with a 400,
	// and runs one with it at most once per key.
	IdempotencyKeyRequired
)

// The defaults of IdempotencyOptions.
const (
	defaultIdempotencyTTL      = 24 * time.Hour
	defaultIdempotencyInFlight = time.Minute
	defaultIdempotencyMaxBody  = 1 << 20
)

// idempotencyConfig is a registry's Idempotency-Key settings, with the
// options' defaults applied.
type idempotencyConfig struct {
	store       IdempotencyStore
	scope       func(*http.Request) string
	ttl         time.Duration
	inFlightTTL time.Duration
	maxBody     int64
}

// WithIdempotency lets the actions that set IdempotencyKey take an
// Idempotency-Key header, as the IETF Idempotency-Key draft describes, so that
// a client's retry of a POST or PATCH runs it at most once.
//
// The key step runs last of an action's own Middleware, before decoding, so a
// request that middleware refuses touches nothing. It reads the request body
// (within the registry's body cap) and answers from the store:
//
//   - a missing header is a 400 under IdempotencyKeyRequired, and runs the
//     handler as usual under IdempotencyKeyOptional; a malformed one is a 400;
//   - a key whose response was saved for the same request (its method,
//     request URI, and body) replays that response: its status, headers (but
//     Set-Cookie, Date, and X-Request-ID), and body, with Idempotent-Replayed:
//     true, and the handler doesn't run;
//   - a key saved for another request is a 422 IDEMPOTENCY_KEY_REUSED;
//   - a key another request still holds is a 409 IDEMPOTENCY_KEY_IN_USE, with
//     Retry-After: 1;
//   - a new key is claimed for InFlightTTL and the handler runs. A 2xx whose
//     body is at most MaxBody is saved for TTL; nothing else is. The claim is
//     then released, whatever the outcome (an error, a timeout, a panic).
//
// A store failure before the handler is a 503 SERVICE_UNAVAILABLE; one while
// saving is logged, and the response stands. Keys are scoped by opts.Scope: the
// store key is the SHA-256 of the scope and the key. The header is an RFC 8941
// String ("…", with \" and \\ as escapes) or the same characters bare, 1–255
// characters once unescaped. The contract lists the header on each keyed
// operation, with 409 and 422 responses unless the action documents them, and,
// with WithErrorCodes, the two codes in the Error schema's code enum.
//
// It panics when opts.Scope is nil. A nil store is ignored, so an action that
// sets IdempotencyKey then fails Freeze.
func WithIdempotency(store IdempotencyStore, opts IdempotencyOptions) Option {
	if opts.Scope == nil {
		panic("actions: WithIdempotency needs a Scope")
	}
	cfg := &idempotencyConfig{
		store:       store,
		scope:       opts.Scope,
		ttl:         opts.TTL,
		inFlightTTL: opts.InFlightTTL,
		maxBody:     opts.MaxBody,
	}
	if cfg.ttl <= 0 {
		cfg.ttl = defaultIdempotencyTTL
	}
	if cfg.inFlightTTL <= 0 {
		cfg.inFlightTTL = defaultIdempotencyInFlight
	}
	if cfg.maxBody <= 0 {
		cfg.maxBody = defaultIdempotencyMaxBody
	}
	return func(r *Registry) {
		if store != nil {
			r.idempotency = cfg
		}
	}
}

// The headers the key step reads and writes.
const (
	idempotencyKeyHeader     = "Idempotency-Key"
	idempotentReplayedHeader = "Idempotent-Replayed"
)

// maxIdempotencyKeyLen is the longest key, in characters once unescaped.
const maxIdempotencyKeyLen = 255

// The key step's answers.
const (
	msgIdempotencyKeyRequired  = "Idempotency-Key header is required"
	msgIdempotencyKeyMalformed = "Idempotency-Key header is malformed"
	msgIdempotencyKeyInUse     = "a request with this Idempotency-Key is still running"
	msgIdempotencyKeyReused    = "this Idempotency-Key was used for another request"
	msgIdempotencyStoreDown    = "the idempotency store is unavailable"
)

// parseIdempotencyKey reads an Idempotency-Key value, its surrounding spaces
// and tabs trimmed: an RFC 8941 String, whose key is its unescaped content, or
// the same characters bare. Either way the key is 1–255 characters.
func parseIdempotencyKey(v string) (string, bool) {
	v = strings.Trim(v, " \t")
	if strings.HasPrefix(v, `"`) {
		return parseQuotedIdempotencyKey(v)
	}
	if v == "" || len(v) > maxIdempotencyKeyLen {
		return "", false
	}
	for i := range len(v) {
		if !isIdempotencyKeyChar(v[i]) {
			return "", false
		}
	}
	return v, true
}

// parseQuotedIdempotencyKey reads an RFC 8941 String: a quote, characters or
// the escapes \" and \\, and a closing quote that ends the value. The key is
// the unescaped content, so it may hold a quote or a backslash.
func parseQuotedIdempotencyKey(v string) (string, bool) {
	var key strings.Builder
	escaped := false
	for i := 1; i < len(v); i++ {
		c := v[i]
		switch {
		case escaped:
			if c != '"' && c != '\\' {
				return "", false
			}
			key.WriteByte(c)
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			ok := i == len(v)-1 && key.Len() >= 1 && key.Len() <= maxIdempotencyKeyLen
			return key.String(), ok
		case isIdempotencyKeyChar(c):
			key.WriteByte(c)
		default:
			return "", false
		}
	}
	return "", false // no closing quote
}

// isIdempotencyKeyChar reports whether c may appear unescaped in a key:
// printable ASCII other than a quote and a backslash.
func isIdempotencyKeyChar(c byte) bool {
	return c >= ' ' && c <= '~' && c != '"' && c != '\\'
}

// idempotencyStoreKey is the store key for a caller's key: the lowercase hex
// SHA-256 of scope, a newline, and key.
func idempotencyStoreKey(scope, key string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, scope)
	_, _ = io.WriteString(h, "\n")
	_, _ = io.WriteString(h, key)
	return hex.EncodeToString(h.Sum(nil))
}

// requestFingerprint binds a key to its first request: the lowercase hex
// SHA-256 of the method, a newline, the request URI, a newline, and the body.
func requestFingerprint(req *http.Request, body []byte) string {
	h := sha256.New()
	_, _ = io.WriteString(h, req.Method)
	_, _ = io.WriteString(h, "\n")
	_, _ = io.WriteString(h, req.URL.RequestURI())
	_, _ = io.WriteString(h, "\n")
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// keyStep runs an action's requests through its Idempotency-Key step (see
// WithIdempotency).
type keyStep struct {
	reg      *Registry
	cfg      *idempotencyConfig
	actionID string
	use      IdempotencyKeyUse
	next     http.Handler
}

// ServeHTTP answers a request from the store, or runs it once for its key.
func (s *keyStep) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	values := req.Header.Values(idempotencyKeyHeader)
	if len(values) == 0 {
		if s.use == IdempotencyKeyRequired {
			s.reg.writeError(w, req, &APIError{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: msgIdempotencyKeyRequired})
			return
		}
		s.next.ServeHTTP(w, req)
		return
	}
	key, ok := "", false
	if len(values) == 1 {
		key, ok = parseIdempotencyKey(values[0])
	}
	if !ok {
		s.reg.writeError(w, req, &APIError{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: msgIdempotencyKeyMalformed})
		return
	}

	// The whole body is read, within the body cap, before any store call: it
	// is part of the fingerprint. The decoder then reads the same bytes.
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			s.reg.writeError(w, req, bodyError(err, s.reg.strictDecoding))
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	storeKey := idempotencyStoreKey(s.cfg.scope(req), key)
	fp := requestFingerprint(req, body)

	if s.answer(w, req, storeKey, fp) {
		return
	}
	token, claimed, err := s.cfg.store.Claim(req.Context(), storeKey, s.cfg.inFlightTTL)
	if err != nil {
		s.unavailable(w, req, err)
		return
	}
	if !claimed {
		s.reg.writeError(w, req, &APIError{
			Status: http.StatusConflict, Code: CodeIdempotencyKeyInUse, Message: msgIdempotencyKeyInUse,
			RetryAfter: time.Second,
		})
		return
	}
	defer s.release(req, storeKey, token)
	// A request with this key may have finished between the lookup and the
	// claim; its response is then answered rather than run again.
	if s.answer(w, req, storeKey, fp) {
		return
	}

	rec := &replayRecorder{ResponseWriter: w, limit: s.cfg.maxBody}
	s.next.ServeHTTP(rec, req)
	s.save(req, storeKey, fp, rec)
}

// answer looks the key up and answers when the store can: a response saved
// for the same request is replayed, one saved for another is refused, and a
// store failure is a 503. It reports whether it answered.
func (s *keyStep) answer(w http.ResponseWriter, req *http.Request, storeKey, fp string) bool {
	saved, found, err := s.cfg.store.Lookup(req.Context(), storeKey)
	switch {
	case err != nil:
		s.unavailable(w, req, err)
	case !found:
		return false
	case saved.Fingerprint != fp:
		s.reg.writeError(w, req, &APIError{
			Status: http.StatusUnprocessableEntity, Code: CodeIdempotencyKeyReused, Message: msgIdempotencyKeyReused,
		})
	default:
		replay(w, saved)
	}
	return true
}

// unavailable logs a store failure and answers 503.
func (s *keyStep) unavailable(w http.ResponseWriter, req *http.Request, err error) {
	slog.ErrorContext(req.Context(), "actions: idempotency store failed", "action", s.actionID, "error", err)
	s.reg.writeError(w, req, &APIError{
		Status: http.StatusServiceUnavailable, Code: CodeServiceUnavailable, Message: msgIdempotencyStoreDown,
	})
}

// save keeps a 2xx response whose body fit within MaxBody; nothing else is
// saved. It runs without the request's cancellation, so a client that left
// still has its response kept. A failure is logged, and the response stands.
func (s *keyStep) save(req *http.Request, storeKey, fp string, rec *replayRecorder) {
	if rec.status < http.StatusOK || rec.status >= http.StatusMultipleChoices || rec.overflow {
		return
	}
	saved := StoredResponse{Fingerprint: fp, Status: rec.status, Header: rec.header, Body: rec.body}
	for _, name := range unsavedHeaders() {
		saved.Header.Del(name)
	}
	ctx := context.WithoutCancel(req.Context())
	if err := s.cfg.store.Save(ctx, storeKey, saved, s.cfg.ttl); err != nil {
		slog.ErrorContext(ctx, "actions: idempotency response not saved", "action", s.actionID, "error", err)
	}
}

// release drops the request's claim, without the request's cancellation, so
// a client that left still frees its key. A failure is logged; the claim then
// ends after InFlightTTL.
func (s *keyStep) release(req *http.Request, storeKey, token string) {
	ctx := context.WithoutCancel(req.Context())
	if err := s.cfg.store.Release(ctx, storeKey, token); err != nil {
		slog.ErrorContext(ctx, "actions: idempotency claim not released", "action", s.actionID, "error", err)
	}
}

// unsavedHeaders are the response headers never saved or replayed: a cookie
// belongs to the request that set it, and the date and the request id to the
// response that carries them.
func unsavedHeaders() []string {
	return []string{"Set-Cookie", "Date", "X-Request-ID"}
}

// replay writes a saved response: its status, its headers (replacing any the
// request already has, but never a cookie, a date, or a request id), the
// Idempotent-Replayed header, and its body.
func replay(w http.ResponseWriter, saved StoredResponse) {
	h := w.Header()
	header := saved.Header.Clone()
	for _, name := range unsavedHeaders() {
		header.Del(name)
	}
	for name, values := range header {
		h.Del(name)
		for _, v := range values {
			h.Add(name, v)
		}
	}
	h.Set(idempotentReplayedHeader, "true")
	w.WriteHeader(saved.Status)
	if len(saved.Body) > 0 {
		_, _ = w.Write(saved.Body)
	}
}

// replayRecorder writes a response through to the client and records what a
// replay needs: the first status, the header as it stood when the status was
// written, and the body while it fits within limit.
type replayRecorder struct {
	http.ResponseWriter

	limit    int64
	status   int
	header   http.Header
	body     []byte
	overflow bool
}

// WriteHeader records the first status and the header at that moment.
func (rr *replayRecorder) WriteHeader(code int) {
	if rr.status == 0 {
		rr.status = code
		rr.header = rr.ResponseWriter.Header().Clone()
	}
	rr.ResponseWriter.WriteHeader(code)
}

// Write records p while the body fits within the limit, and writes it.
func (rr *replayRecorder) Write(p []byte) (int, error) {
	if rr.status == 0 {
		rr.WriteHeader(http.StatusOK)
	}
	if !rr.overflow {
		if int64(len(rr.body))+int64(len(p)) > rr.limit {
			rr.overflow = true
			rr.body = nil
		} else {
			rr.body = append(rr.body, p...)
		}
	}
	return rr.ResponseWriter.Write(p)
}

// Unwrap returns the wrapped ResponseWriter, for http.ResponseController.
func (rr *replayRecorder) Unwrap() http.ResponseWriter { return rr.ResponseWriter }

// idempotencyStep returns the middleware that runs an action's Idempotency-Key
// step; wrapAction puts it last of the action's own Middleware.
func (r *Registry) idempotencyStep(a anyAction) Middleware {
	return func(next http.Handler) http.Handler {
		return &keyStep{reg: r, cfg: r.idempotency, actionID: a.id, use: a.idempotencyKey, next: next}
	}
}
