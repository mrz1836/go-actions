package actions_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

// orderReq is the body of the keyed order actions. Its Item picks what the
// handler does: "block" waits until the test lets it finish, "fail" returns an
// error, "panic" panics, "slow" runs past the action's Timeout, and anything
// else creates the order.
type orderReq struct {
	Item string `json:"item"`
}

// orderResp is a created order.
type orderResp struct {
	Number int64  `json:"number"`
	Item   string `json:"item"`
}

// errOrderFailed is the keyed handler's failure, answered as a 500.
var errOrderFailed = errors.New("order failed")

// callerKey is the context key under which the keyed actions' own middleware
// puts the caller.
type callerKey struct{}

// callerMiddleware is the keyed actions' own middleware: it refuses a request
// carrying X-Refuse: 1 with a 403, and otherwise puts the X-Caller header's
// value on the context as the caller.
func callerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Refuse") == "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, r.Header.Get("X-Caller"))))
	})
}

// callerScope is the keyed registry's Scope: the caller the action's own
// middleware put on the request.
func callerScope(r *http.Request) string {
	caller, _ := r.Context().Value(callerKey{}).(string)
	return caller
}

// spyStore wraps the memory store and counts each method's calls. A method can
// be made to fail, and a response can be planted under a key as soon as it is
// first claimed.
type spyStore struct {
	inner actions.IdempotencyStore

	mu      sync.Mutex
	calls   map[string]int
	fail    map[string]error
	plant   *actions.StoredResponse
	claimed []string
}

func newSpyStore() *spyStore {
	return &spyStore{inner: actions.NewMemoryIdempotencyStore(), calls: map[string]int{}, fail: map[string]error{}}
}

func (s *spyStore) Lookup(ctx context.Context, key string) (actions.StoredResponse, bool, error) {
	if err := s.enter("Lookup"); err != nil {
		return actions.StoredResponse{}, false, err
	}
	return s.inner.Lookup(ctx, key)
}

func (s *spyStore) Claim(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	if err := s.enter("Claim"); err != nil {
		return "", false, err
	}
	token, claimed, err := s.inner.Claim(ctx, key, ttl)
	if !claimed {
		return token, claimed, err
	}
	s.mu.Lock()
	s.claimed = append(s.claimed, key)
	plant := s.plant
	s.plant = nil
	s.mu.Unlock()
	if plant != nil {
		if saveErr := s.inner.Save(ctx, key, *plant, time.Minute); saveErr != nil {
			return "", false, saveErr
		}
	}
	return token, claimed, err
}

func (s *spyStore) Save(ctx context.Context, key string, resp actions.StoredResponse, ttl time.Duration) error {
	if err := s.enter("Save"); err != nil {
		return err
	}
	return s.inner.Save(ctx, key, resp, ttl)
}

func (s *spyStore) Release(ctx context.Context, key, token string) error {
	if err := s.enter("Release"); err != nil {
		return err
	}
	return s.inner.Release(ctx, key, token)
}

// enter counts a call to method and returns the error it is made to fail with.
func (s *spyStore) enter(method string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[method]++
	return s.fail[method]
}

// failOn makes method fail with err.
func (s *spyStore) failOn(method string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = err
}

// plantOnClaim saves resp under the next key claimed, right after its claim.
func (s *spyStore) plantOnClaim(resp actions.StoredResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plant = &resp
}

// count returns how many times method was called.
func (s *spyStore) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[method]
}

// total returns how many store calls were made.
func (s *spyStore) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		n += c
	}
	return n
}

// lastClaimed returns the last key claimed.
func (s *spyStore) lastClaimed() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.claimed) == 0 {
		return ""
	}
	return s.claimed[len(s.claimed)-1]
}

// keyedServer serves two POST order actions that take idempotency keys,
// /orders and /orders/copy, over a spy store, and counts the handler's runs.
type keyedServer struct {
	url     string
	store   *spyStore
	runs    atomic.Int64
	entered chan struct{}
	unblock chan struct{}
	once    sync.Once
}

// newKeyedServer serves the order actions with keys used as use. tune, when not
// nil, adjusts the idempotency options; opts are added to the registry's.
func newKeyedServer(t *testing.T, use actions.IdempotencyKeyUse, tune func(*actions.IdempotencyOptions), opts ...actions.Option) *keyedServer {
	t.Helper()
	k := &keyedServer{store: newSpyStore(), entered: make(chan struct{}, 4), unblock: make(chan struct{})}
	idem := actions.IdempotencyOptions{Scope: callerScope}
	if tune != nil {
		tune(&idem)
	}
	reg := actions.NewRegistry(append(slices.Clone(opts), actions.WithIdempotency(k.store, idem))...)
	for _, route := range []struct{ id, path string }{{"orders.create", "/orders"}, {"orders.copy", "/orders/copy"}} {
		actions.Register(reg, actions.Action[orderReq, actions.Response[orderResp]]{
			ID: route.id, Method: http.MethodPost, Path: route.path,
			Statuses:       []actions.StatusDoc{{Code: http.StatusCreated}},
			Timeout:        200 * time.Millisecond,
			Middleware:     []actions.Middleware{callerMiddleware},
			IdempotencyKey: use,
			Handle:         k.handle,
		})
	}
	k.url = actiontest.NewServer(t, reg).URL
	t.Cleanup(k.finish) // runs before the server closes, so no handler is left blocked
	return k
}

// handle is the order actions' Handle.
func (k *keyedServer) handle(ctx context.Context, req orderReq) (actions.Response[orderResp], error) {
	n := k.runs.Add(1)
	switch req.Item {
	case "block":
		k.entered <- struct{}{}
		<-k.unblock
	case "fail":
		return actions.Response[orderResp]{}, errOrderFailed
	case "panic":
		panic("the order handler panicked")
	case "slow":
		<-ctx.Done()
		return actions.Response[orderResp]{}, ctx.Err()
	}
	return actions.Response[orderResp]{
		Status: http.StatusCreated,
		Header: http.Header{"X-Order": {strconv.FormatInt(n, 10)}, "Set-Cookie": {"session=abc"}},
		Body:   orderResp{Number: n, Item: req.Item},
	}, nil
}

// finish lets every blocked handler run return.
func (k *keyedServer) finish() { k.once.Do(func() { close(k.unblock) }) }

// keyedReply is one response from the keyed server.
type keyedReply struct {
	status int
	header http.Header
	body   string
}

// envelope returns the error envelope's code and message.
func (r keyedReply) envelope() (code, message string) {
	var env struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal([]byte(r.body), &env)
	return env.Code, env.Error
}

// do posts body to path with header added, and returns the reply.
func (k *keyedServer) do(ctx context.Context, path, body string, header http.Header) (keyedReply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.url+path, strings.NewReader(body))
	if err != nil {
		return keyedReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, values := range header {
		req.Header.Del(name)
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return keyedReply{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return keyedReply{}, err
	}
	return keyedReply{status: resp.StatusCode, header: resp.Header, body: string(raw)}, nil
}

// post is do for the test's own goroutine: a transport failure fails the test.
func (k *keyedServer) post(t *testing.T, path, body string, header http.Header) keyedReply {
	t.Helper()
	reply, err := k.do(t.Context(), path, body, header)
	require.NoError(t, err)
	return reply
}

// keyHeader returns a header carrying the given Idempotency-Key values.
func keyHeader(values ...string) http.Header {
	return http.Header{"Idempotency-Key": values}
}

// assertRefused asserts a reply is the framework's error with this status,
// code, and message.
func assertRefused(t *testing.T, reply keyedReply, status int, code, message string) {
	t.Helper()
	gotCode, gotMessage := reply.envelope()
	assert.Equal(t, status, reply.status, "body: %s", reply.body)
	assert.Equal(t, code, gotCode)
	assert.Equal(t, message, gotMessage)
}

func TestIdempotencyKeyRunsTheHandlerOnceForConcurrentRequests(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)

	type result struct {
		reply keyedReply
		err   error
	}
	first := make(chan result, 1)
	go func() {
		reply, err := k.do(t.Context(), "/orders", `{"item":"block"}`, keyHeader("order-1"))
		first <- result{reply, err}
	}()
	select {
	case <-k.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first request never reached the handler")
	}

	second := k.post(t, "/orders", `{"item":"block"}`, keyHeader("order-1"))
	assertRefused(t, second, http.StatusConflict, actions.CodeIdempotencyKeyInUse,
		"a request with this Idempotency-Key is still running")
	assert.Equal(t, "1", second.header.Get("Retry-After"))

	k.finish()
	got := <-first
	require.NoError(t, got.err)
	assert.Equal(t, http.StatusCreated, got.reply.status)
	assert.Equal(t, int64(1), k.runs.Load(), "the handler ran once")
}

func TestIdempotencyKeyReplaysTheFirstResponse(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)

	firstHeader := keyHeader("order-2")
	firstHeader.Set("X-Request-ID", "first-request")
	first := k.post(t, "/orders", `{"item":"tea"}`, firstHeader)
	require.Equal(t, http.StatusCreated, first.status)
	assert.Equal(t, "session=abc", first.header.Get("Set-Cookie"))
	assert.Empty(t, first.header.Get("Idempotent-Replayed"))

	retryHeader := keyHeader("order-2")
	retryHeader.Set("X-Request-ID", "second-request")
	retry := k.post(t, "/orders", `{"item":"tea"}`, retryHeader)
	assert.Equal(t, http.StatusCreated, retry.status)
	assert.Equal(t, first.body, retry.body, "the body replays byte for byte")
	assert.Equal(t, first.header.Get("Content-Type"), retry.header.Get("Content-Type"))
	assert.Equal(t, first.header.Get("X-Order"), retry.header.Get("X-Order"))
	assert.Equal(t, "true", retry.header.Get("Idempotent-Replayed"))
	assert.Equal(t, "second-request", retry.header.Get("X-Request-ID"), "the replay carries its own request id")
	assert.Empty(t, retry.header.Values("Set-Cookie"), "a cookie is never replayed")
	assert.Equal(t, int64(1), k.runs.Load(), "the retry did not run the handler")
}

func TestIdempotencyKeyRefusesReuseForAnotherRequest(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
	require.Equal(t, http.StatusCreated, k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-3")).status)

	t.Run("another body", func(t *testing.T) {
		reply := k.post(t, "/orders", `{"item":"coffee"}`, keyHeader("order-3"))
		assertRefused(t, reply, http.StatusUnprocessableEntity, actions.CodeIdempotencyKeyReused,
			"this Idempotency-Key was used for another request")
	})
	t.Run("another route that takes keys", func(t *testing.T) {
		reply := k.post(t, "/orders/copy", `{"item":"tea"}`, keyHeader("order-3"))
		assertRefused(t, reply, http.StatusUnprocessableEntity, actions.CodeIdempotencyKeyReused,
			"this Idempotency-Key was used for another request")
	})
	assert.Equal(t, int64(1), k.runs.Load(), "a reused key never runs the handler")
}

func TestIdempotencyKeyHeaderMustBeWellFormed(t *testing.T) {
	t.Run("missing under IdempotencyKeyRequired", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
		reply := k.post(t, "/orders", `{"item":"tea"}`, nil)
		assertRefused(t, reply, http.StatusBadRequest, actions.CodeBadRequest, "Idempotency-Key header is required")
		assert.Zero(t, k.store.total(), "the store is untouched")
		assert.Zero(t, k.runs.Load())
	})

	malformed := map[string][]string{
		"two values":                    {"order-4", "order-5"},
		"an empty value":                {""},
		"an empty quoted string":        {`""`},
		"256 characters":                {strings.Repeat("k", 256)},
		"256 characters once unescaped": {`"` + strings.Repeat(`\\`, 256) + `"`},
		"an unterminated quote":         {`"order-4`},
		"a bad escape":                  {`"order\n4"`},
		"an escaped closing quote":      {`"order-4\"`},
		"text after the closing quote":  {`"order"-4`},
		"a quote inside a bare key":     {`order"4`},
		"a backslash inside a bare key": {`order\4`},
		"a tab inside":                  {"order\t4"},
		"a character outside ASCII":     {"ordér-4"},
	}
	for name, values := range malformed {
		t.Run(name, func(t *testing.T) {
			k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
			reply := k.post(t, "/orders", `{"item":"tea"}`, keyHeader(values...))
			assertRefused(t, reply, http.StatusBadRequest, actions.CodeBadRequest, "Idempotency-Key header is malformed")
			assert.Zero(t, k.store.total(), "the store is untouched")
			assert.Zero(t, k.runs.Load())
		})
	}

	accepted := map[string]string{
		"a quoted key holding an escaped quote":     `"order\"4"`,
		"a quoted key holding an escaped backslash": `"order\\4"`,
		"255 characters":                strings.Repeat("k", 255),
		"255 characters once unescaped": `"` + strings.Repeat(`\\`, 255) + `"`,
	}
	for name, value := range accepted {
		t.Run(name, func(t *testing.T) {
			k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
			reply := k.post(t, "/orders", `{"item":"tea"}`, keyHeader(value))
			assert.Equal(t, http.StatusCreated, reply.status, "body: %s", reply.body)
			assert.Equal(t, int64(1), k.runs.Load())
		})
	}

	t.Run("missing under IdempotencyKeyOptional", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyOptional, nil)
		reply := k.post(t, "/orders", `{"item":"tea"}`, nil)
		assert.Equal(t, http.StatusCreated, reply.status)
		assert.Equal(t, int64(1), k.runs.Load())
		assert.Zero(t, k.store.total(), "the store is untouched")
	})

	t.Run("malformed under IdempotencyKeyOptional", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyOptional, nil)
		reply := k.post(t, "/orders", `{"item":"tea"}`, keyHeader(`order"4`))
		assertRefused(t, reply, http.StatusBadRequest, actions.CodeBadRequest, "Idempotency-Key header is malformed")
		assert.Zero(t, k.runs.Load())
	})
}

func TestIdempotencyKeyQuotedAndBareAreOneKey(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)

	quoted := k.post(t, "/orders", `{"item":"tea"}`, keyHeader(`"order-6"`))
	require.Equal(t, http.StatusCreated, quoted.status)
	bare := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-6"))
	assert.Equal(t, http.StatusCreated, bare.status)
	assert.Equal(t, "true", bare.header.Get("Idempotent-Replayed"))
	assert.Equal(t, quoted.body, bare.body)
	assert.Equal(t, int64(1), k.runs.Load())
}

func TestIdempotencyKeysAreScopedPerCaller(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
	as := func(caller string) http.Header {
		h := keyHeader("order-7")
		h.Set("X-Caller", caller)
		return h
	}

	ada := k.post(t, "/orders", `{"item":"tea"}`, as("ada"))
	require.Equal(t, http.StatusCreated, ada.status)
	bob := k.post(t, "/orders", `{"item":"coffee"}`, as("bob"))
	require.Equal(t, http.StatusCreated, bob.status, "another caller's key is its own: body %s", bob.body)

	adaRetry := k.post(t, "/orders", `{"item":"tea"}`, as("ada"))
	assert.Equal(t, "true", adaRetry.header.Get("Idempotent-Replayed"))
	assert.Equal(t, ada.body, adaRetry.body)
	bobRetry := k.post(t, "/orders", `{"item":"coffee"}`, as("bob"))
	assert.Equal(t, "true", bobRetry.header.Get("Idempotent-Replayed"))
	assert.Equal(t, bob.body, bobRetry.body)
	assert.Equal(t, int64(2), k.runs.Load(), "each caller's first request ran once")
}

func TestIdempotencyKeyRefusedRequestGetsTheMiddlewaresAnswer(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
	refused := func(h http.Header) http.Header {
		h.Set("X-Refuse", "1")
		return h
	}

	t.Run("with a key", func(t *testing.T) {
		reply := k.post(t, "/orders", `{"item":"tea"}`, refused(keyHeader("order-8")))
		assert.Equal(t, http.StatusForbidden, reply.status)
	})
	t.Run("without the required key", func(t *testing.T) {
		reply := k.post(t, "/orders", `{"item":"tea"}`, refused(http.Header{}))
		assert.Equal(t, http.StatusForbidden, reply.status, "the action's middleware answers before the key is checked")
	})
	assert.Zero(t, k.store.total(), "a refused request touches no store")

	later := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-8"))
	assert.Equal(t, http.StatusCreated, later.status)
	assert.Equal(t, int64(1), k.runs.Load(), "the key is still free for a request the middleware lets through")
}

func TestIdempotencyKeyFailedRequestIsNotSaved(t *testing.T) {
	for _, tc := range []struct {
		name   string
		item   string
		status int
	}{
		{"a 500", "fail", http.StatusInternalServerError},
		{"a panic", "panic", http.StatusInternalServerError},
		{"a 504 past the Timeout", "slow", http.StatusGatewayTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
			body := `{"item":"` + tc.item + `"}`

			assert.Equal(t, tc.status, k.post(t, "/orders", body, keyHeader("order-9")).status)
			assert.Equal(t, tc.status, k.post(t, "/orders", body, keyHeader("order-9")).status)
			assert.Equal(t, int64(2), k.runs.Load(), "the retry ran the handler again")
			assert.Zero(t, k.store.count("Save"), "nothing was saved")
			assert.Equal(t, 2, k.store.count("Release"), "each claim was released")
		})
	}
}

func TestIdempotencyKeyLargeResponseIsNotSaved(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, func(o *actions.IdempotencyOptions) { o.MaxBody = 8 })

	first := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-10"))
	assert.Equal(t, http.StatusCreated, first.status)
	assert.JSONEq(t, `{"number":1,"item":"tea"}`, first.body, "the response is sent whole")

	retry := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-10"))
	assert.Equal(t, http.StatusCreated, retry.status)
	assert.Empty(t, retry.header.Get("Idempotent-Replayed"))
	assert.Equal(t, int64(2), k.runs.Load(), "the retry ran the handler again")
	assert.Zero(t, k.store.count("Save"))
	assert.Equal(t, 2, k.store.count("Release"))
}

func TestIdempotencyKeyBodyOverTheCapTouchesNoStore(t *testing.T) {
	k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil, actions.WithMaxBodyBytes(16))

	body := `{"item":"` + strings.Repeat("t", 21) + `"}`
	require.Len(t, body, 32)
	reply := k.post(t, "/orders", body, keyHeader("order-11"))
	assertRefused(t, reply, http.StatusRequestEntityTooLarge, actions.CodePayloadTooLarge, "request body too large")
	assert.Zero(t, k.store.total(), "the body is read before any store call")
	assert.Zero(t, k.runs.Load())
}

// errStoreDown is a spy store's failure.
var errStoreDown = errors.New("store down")

// lockedBuffer is a buffer the server's goroutines write and the test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs sends slog's default logger to a buffer until the test ends. A
// test that calls it must not run in parallel.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

func TestIdempotencyKeyStoreFailures(t *testing.T) {
	logs := captureLogs(t)

	for _, method := range []string{"Lookup", "Claim"} {
		t.Run(method+" fails", func(t *testing.T) {
			k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
			k.store.failOn(method, errStoreDown)

			reply := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-12"))
			assertRefused(t, reply, http.StatusServiceUnavailable, actions.CodeServiceUnavailable,
				"the idempotency store is unavailable")
			assert.Zero(t, k.runs.Load(), "the handler doesn't run")
			assert.Contains(t, logs.String(), "actions: idempotency store failed")
			assert.Contains(t, logs.String(), "store down")
		})
	}

	t.Run("Save fails", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
		k.store.failOn("Save", errStoreDown)

		reply := k.post(t, "/orders", `{"item":"tea"}`, keyHeader("order-12"))
		assert.Equal(t, http.StatusCreated, reply.status, "the handler's response stands")
		assert.JSONEq(t, `{"number":1,"item":"tea"}`, reply.body)
		assert.Equal(t, int64(1), k.runs.Load())
		assert.Equal(t, 1, k.store.count("Release"), "the claim was released")
		assert.Contains(t, logs.String(), "actions: idempotency response not saved")
	})
}

// fingerprint is the documented fingerprint of a POST to /orders with body.
func fingerprint(body string) string {
	sum := sha256.Sum256([]byte("POST\n/orders\n" + body))
	return hex.EncodeToString(sum[:])
}

func TestIdempotencyKeyLooksUpAgainAfterClaiming(t *testing.T) {
	const body = `{"item":"tea"}`

	t.Run("a response saved meanwhile is replayed", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
		k.store.plantOnClaim(actions.StoredResponse{
			Fingerprint: fingerprint(body),
			Status:      http.StatusCreated,
			Header:      http.Header{"Content-Type": {"application/json"}},
			Body:        []byte(`{"number":99,"item":"tea"}`),
		})

		reply := k.post(t, "/orders", body, keyHeader("order-13"))
		assert.Equal(t, http.StatusCreated, reply.status)
		assert.JSONEq(t, `{"number":99,"item":"tea"}`, reply.body)
		assert.Equal(t, "true", reply.header.Get("Idempotent-Replayed"))
		assert.Zero(t, k.runs.Load(), "the handler doesn't run")
		assert.Equal(t, 1, k.store.count("Release"))
		_, claimed, err := k.store.inner.Claim(t.Context(), k.store.lastClaimed(), time.Minute)
		require.NoError(t, err)
		assert.True(t, claimed, "the claim was released")
	})

	t.Run("a response saved meanwhile for another request is refused", func(t *testing.T) {
		k := newKeyedServer(t, actions.IdempotencyKeyRequired, nil)
		k.store.plantOnClaim(actions.StoredResponse{Fingerprint: fingerprint(`{"item":"coffee"}`), Status: http.StatusCreated})

		reply := k.post(t, "/orders", body, keyHeader("order-14"))
		assertRefused(t, reply, http.StatusUnprocessableEntity, actions.CodeIdempotencyKeyReused,
			"this Idempotency-Key was used for another request")
		assert.Zero(t, k.runs.Load())
		assert.Equal(t, 1, k.store.count("Release"))
	})
}

// keyedContract returns a frozen registry with a POST action that requires a
// key, a PATCH action that takes an optional key and documents its own 422, and
// a POST action without keys.
func keyedContract(t *testing.T, opts ...actions.Option) *actions.Registry {
	t.Helper()
	reg := actions.NewRegistry(opts...)
	actions.Register(reg, actions.Action[orderReq, actions.Created[orderResp]]{
		ID: "keys.required", Method: http.MethodPost, Path: "/required",
		Statuses:       []actions.StatusDoc{{Code: http.StatusCreated}},
		IdempotencyKey: actions.IdempotencyKeyRequired,
		Handle:         okHandle[orderReq, actions.Created[orderResp]],
	})
	actions.Register(reg, actions.Action[orderReq, orderResp]{
		ID: "keys.optional", Method: http.MethodPatch, Path: "/optional",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK},
			{Code: http.StatusUnprocessableEntity, Error: true, Description: "the order cannot change"},
		},
		IdempotencyKey: actions.IdempotencyKeyOptional,
		Handle:         okHandle[orderReq, orderResp],
	})
	actions.Register(reg, actions.Action[orderReq, actions.Created[orderResp]]{
		ID: "keys.none", Method: http.MethodPost, Path: "/none",
		Statuses: []actions.StatusDoc{{Code: http.StatusCreated}},
		Handle:   okHandle[orderReq, actions.Created[orderResp]],
	})
	reg.Freeze()
	return reg
}

// keyParameter returns an operation's Idempotency-Key parameter, or nil.
func keyParameter(op map[string]any) map[string]any {
	params, _ := op["parameters"].([]any)
	for _, p := range params {
		if param, _ := p.(map[string]any); param["name"] == "Idempotency-Key" {
			return param
		}
	}
	return nil
}

// errorRefContent is a response's content when it is the Error schema.
func errorRefContent() map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}
}

func TestOpenAPIDocumentsIdempotencyKeys(t *testing.T) {
	reg := keyedContract(t, withMemoryStore(), actions.WithErrorCodes())
	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
	required := operation(t, doc, "post", "/required")
	optional := operation(t, doc, "patch", "/optional")
	none := operation(t, doc, "post", "/none")

	t.Run("a keyed action lists the header parameter", func(t *testing.T) {
		param := keyParameter(required)
		require.NotNil(t, param)
		assert.Equal(t, "header", param["in"])
		assert.Equal(t, true, param["required"])
		assert.Equal(t, map[string]any{"type": "string"}, param["schema"])
		assert.NotEmpty(t, param["description"])

		param = keyParameter(optional)
		require.NotNil(t, param)
		assert.Equal(t, false, param["required"])

		assert.Nil(t, keyParameter(none), "an action without keys lists no header")
	})

	t.Run("a keyed action documents 409 and 422", func(t *testing.T) {
		responses, _ := required["responses"].(map[string]any)
		conflict, _ := responses["409"].(map[string]any)
		assert.Equal(t, "a request with this Idempotency-Key is still running", conflict["description"])
		assert.Equal(t, errorRefContent(), conflict["content"])
		headers, _ := conflict["headers"].(map[string]any)
		retryAfter, _ := headers["Retry-After"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "integer"}, retryAfter["schema"])
		reused, _ := responses["422"].(map[string]any)
		assert.Equal(t, "this Idempotency-Key was used for another request", reused["description"])
		assert.Equal(t, errorRefContent(), reused["content"])

		responses, _ = optional["responses"].(map[string]any)
		assert.Contains(t, responses, "409")
		own, _ := responses["422"].(map[string]any)
		assert.Equal(t, "the order cannot change", own["description"], "an action's own 422 is kept")

		responses, _ = none["responses"].(map[string]any)
		assert.NotContains(t, responses, "409")
		assert.NotContains(t, responses, "422")
	})

	t.Run("the code enum lists the two codes only with WithIdempotency", func(t *testing.T) {
		want := append(builtinCodes(), actions.CodeIdempotencyKeyInUse, actions.CodeIdempotencyKeyReused)
		slices.Sort(want)
		assert.Equal(t, want, errorCodeEnum(t, errorComponent(t, reg)))
		assert.Equal(t, builtinCodes(), errorCodeEnum(t, errorComponent(t, frozenPingRegistry(t, actions.WithErrorCodes()))))
		assert.Nil(t, errorCodeEnum(t, errorComponent(t, frozenPingRegistry(t, withMemoryStore()))),
			"without WithErrorCodes, code stays an open string")
	})

	t.Run("the codes are spelled as the wire spells them", func(t *testing.T) {
		assert.Equal(t, "IDEMPOTENCY_KEY_IN_USE", actions.CodeIdempotencyKeyInUse)
		assert.Equal(t, "IDEMPOTENCY_KEY_REUSED", actions.CodeIdempotencyKeyReused)
	})
}
