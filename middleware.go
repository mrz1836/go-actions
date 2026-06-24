package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

// errPanic is the static base error wrapping a recovered panic value.
var errPanic = errors.New("actions: recovered panic")

// Middleware is the standard net/http middleware shape: a function that wraps a
// handler and returns a new one. It is identical to chi's middleware signature,
// so chi and third-party middleware compose with go-actions directly.
type Middleware = func(http.Handler) http.Handler

// Observation is the per-request record handed to an ObserveFunc once an action
// handler completes — including a recovered panic, which is reported as status
// 500 with a non-nil Err.
type Observation struct {
	ActionID string
	Method   string
	Path     string
	Status   int
	Duration time.Duration
	Err      error
}

// ObserveFunc receives one Observation per handled action request. Install one
// with WithObserver; it is the seam for access logging, latency metrics, and
// tracing. It must not write to the response.
type ObserveFunc func(Observation)

// ctxKey is the private context-key type for the framework's request-scoped
// values.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	observeStateKey
)

// observeState carries the handled error out to the observe middleware, which
// runs outside the typed handler and so cannot see the error directly.
type observeState struct{ err error }

// RequestIDFromContext returns the request correlation id placed in ctx by the
// framework's request-id middleware, or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// defaultRequestID generates a fresh request correlation id (UUIDv4). Override
// the generator with WithRequestIDGenerator (e.g. to emit time-ordered UUIDv7).
func defaultRequestID() string { return uuid.NewString() }

// requestIDMiddleware guarantees every action request carries a correlation id.
// It reuses an inbound X-Request-ID / X-Amzn-Request-Id when present, otherwise
// it generates one; then it stores the id in the request context
// (RequestIDFromContext), normalizes it onto the request as X-Request-ID so the
// error envelope and handlers observe the same value, and echoes it on the
// response.
func (r *Registry) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := requestID(req)
		if id == "" {
			gen := r.requestIDGen
			if gen == nil {
				gen = defaultRequestID
			}
			id = gen()
		}
		req.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, req.WithContext(withRequestID(req.Context(), id)))
	})
}

// recoverMiddleware converts a panic in any inner handler into a logged,
// redacted 500 so a single bad handler never tears down the connection. The
// http.ErrAbortHandler sentinel is re-panicked unchanged, per net/http
// convention.
//
//nolint:contextcheck // the recovered request's context flows through req
func (r *Registry) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			slog.ErrorContext(req.Context(), "actions: handler panic",
				"panic", rec, "stack", string(debug.Stack()))
			if st, ok := req.Context().Value(observeStateKey).(*observeState); ok {
				st.err = fmt.Errorf("%w: %v", errPanic, rec)
			}
			r.writeAPIError(w, req, APIError{
				Status:  http.StatusInternalServerError,
				Code:    CodeInternal,
				Message: "an internal error occurred",
			})
		}()
		next.ServeHTTP(w, req)
	})
}

// observeMiddleware times an action request, captures its final status, and
// reports an Observation to the registry's observer. It is installed only when
// an observer is configured.
func (r *Registry) observeMiddleware(actionID, method, path string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			st := &observeState{}
			req = req.WithContext(context.WithValue(req.Context(), observeStateKey, st))
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, req)
			r.observer(Observation{
				ActionID: actionID,
				Method:   method,
				Path:     path,
				Status:   rec.status,
				Duration: time.Since(start),
				Err:      st.err,
			})
		})
	}
}

// timeoutMiddleware bounds an action's handler with a context deadline. The
// handler must honor ctx; the typed handler maps a deadline overrun to 504.
func timeoutMiddleware(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx, cancel := context.WithTimeout(req.Context(), d)
			defer cancel()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// maxBytesMiddleware caps the request body at the registry's configured limit;
// a read past the cap surfaces in decodeBody as a 413.
func (r *Registry) maxBytesMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Body != nil {
			req.Body = http.MaxBytesReader(w, req.Body, r.maxBodyBytes)
		}
		next.ServeHTTP(w, req)
	})
}

// statusRecorder wraps a ResponseWriter to capture the status code written, for
// observability. Defaults to 200 when the handler writes a body without an
// explicit WriteHeader.
type statusRecorder struct {
	http.ResponseWriter

	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// chain wraps h with the given middleware. The first middleware is the
// outermost (it sees the request first and the response last).
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
