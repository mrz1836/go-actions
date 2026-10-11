package actions

import (
	"encoding/json"
	"net/http"
)

// responseEnvelope is implemented by the Empty, Created[T], Accepted[T], and
// Response[T] response wrappers. It lets the encoder pick the HTTP status and body without
// reflecting on the concrete type.
type responseEnvelope interface {
	envelopeStatus() int
	envelopeBody() any
}

// envelopeStatus reports the 204 status for an Empty response.
func (Empty) envelopeStatus() int { return http.StatusNoContent }

// envelopeBody reports the (absent) body for an Empty response.
func (Empty) envelopeBody() any { return nil }

// envelopeStatus reports the 201 status for a Created response.
func (Created[T]) envelopeStatus() int { return http.StatusCreated }

// envelopeBody returns the wrapped Created body.
func (c Created[T]) envelopeBody() any { return c.Body }

// envelopeStatus reports the 202 status for an Accepted response.
func (Accepted[T]) envelopeStatus() int { return http.StatusAccepted }

// envelopeBody returns the wrapped Accepted body.
func (a Accepted[T]) envelopeBody() any { return a.Body }

// envelopeStatus reports a Response's status, defaulting to 200 OK when unset.
func (r Response[T]) envelopeStatus() int {
	if r.Status == 0 {
		return http.StatusOK
	}
	return r.Status
}

// envelopeBody returns the wrapped Response body.
func (r Response[T]) envelopeBody() any { return r.Body }

// envelopeHeaders returns the Response's optional extra headers.
func (r Response[T]) envelopeHeaders() http.Header { return r.Header }

// headerEnvelope is the optional second interface a response wrapper may
// implement to contribute extra response headers (see Response).
type headerEnvelope interface {
	envelopeHeaders() http.Header
}

// encodeResponse writes a successful response. An Empty/Created/Accepted/Response
// wrapper sets the documented status (and Response may add headers); any other
// value is encoded as 200. A 204 or 304 is written without a body (see
// writeJSON).
func encodeResponse(w http.ResponseWriter, resp any) {
	if h, ok := resp.(headerEnvelope); ok {
		for key, vals := range h.envelopeHeaders() {
			for _, v := range vals {
				w.Header().Add(key, v)
			}
		}
	}
	if env, ok := resp.(responseEnvelope); ok {
		writeJSON(w, env.envelopeStatus(), env.envelopeBody())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON serializes data as JSON and writes it to w with the given HTTP
// status and Content-Type application/json. If marshaling fails, a 500 with a
// static error body is written instead — the original status is discarded.
//
// A 204 No Content or 304 Not Modified carries no content (RFC 9110), so for
// those data is ignored: the headers that describe a body (Content-Type,
// Content-Length, Transfer-Encoding) are removed, and only the status and the
// remaining headers are written. Success and error responses share this rule,
// since both are written here.
func writeJSON(w http.ResponseWriter, status int, data any) {
	if status == http.StatusNoContent || status == http.StatusNotModified {
		h := w.Header()
		h.Del("Content-Type")
		h.Del("Content-Length")
		h.Del("Transfer-Encoding")
		w.WriteHeader(status)
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		w.Header()["Content-Type"] = jsonContentType
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to encode response","code":"` + CodeInternal + `"}`))
		return
	}
	w.Header()["Content-Type"] = jsonContentType
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// jsonContentType is the shared Content-Type header value of every JSON
// response. Assigning it directly skips the per-response slice allocation of
// Header.Set; Set and Add on the header replace or copy it rather than
// mutating it.
//
//nolint:gochecknoglobals // an immutable, shared header value
var jsonContentType = []string{"application/json"}
