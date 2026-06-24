package actions

import (
	"encoding/json"
	"net/http"
)

// responseEnvelope is implemented by the Empty, Created[T], and Accepted[T]
// response wrappers. It lets the encoder pick the HTTP status and body without
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
// value is encoded as 200.
func encodeResponse(w http.ResponseWriter, resp any) {
	if h, ok := resp.(headerEnvelope); ok {
		for key, vals := range h.envelopeHeaders() {
			for _, v := range vals {
				w.Header().Add(key, v)
			}
		}
	}
	if env, ok := resp.(responseEnvelope); ok {
		status := env.envelopeStatus()
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, env.envelopeBody())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON serializes data as JSON and writes it to w with the given HTTP
// status. Content-Type is always set to application/json. If marshaling fails,
// a 500 with a static error body is written instead — the original status is
// discarded.
func writeJSON(w http.ResponseWriter, status int, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to encode response","code":"` + CodeInternal + `"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
