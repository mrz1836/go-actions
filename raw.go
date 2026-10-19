package actions

import (
	"net/http"
	"reflect"
)

// Raw is a response written as given, for a body that isn't JSON, such as a
// CSV export: status 200, Header added, Content-Type set to the media type the
// action's 200 StatusDoc declares (StatusDoc.MediaType), then Body verbatim.
// Escaping the content, such as formula cells in a CSV, is up to the producer.
// Use Raw itself as the action's Resp; its contract documents the media type
// with a string body.
type Raw struct {
	// Header is added to the response. A Content-Type in it is replaced by the
	// declared media type.
	Header http.Header
	// Body is written as it is. An empty Body is a 200 with no content.
	Body []byte
}

// isRawType reports whether t is Raw itself.
func isRawType(t reflect.Type) bool { return t == reflect.TypeFor[Raw]() }

// rawMediaType reports whether respType is Raw, and if so the media type its
// responses are written under: its 200 StatusDoc's MediaType.
func rawMediaType(respType reflect.Type, statuses []StatusDoc) (string, bool) {
	if !isRawType(respType) {
		return "", false
	}
	if i := rawStatusIndex(statuses); i >= 0 {
		return statuses[i].MediaType, true
	}
	return "", true
}

// writeRaw writes raw as a 200: its Header added, then Content-Type set to
// mediaType, which so replaces any Content-Type in Header, then its Body.
func writeRaw(w http.ResponseWriter, raw Raw, mediaType string) {
	h := w.Header()
	for name, values := range raw.Header {
		for _, v := range values {
			h.Add(name, v)
		}
	}
	h.Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	if len(raw.Body) > 0 {
		_, _ = w.Write(raw.Body)
	}
}

// rawStatusIndex returns the index of the StatusDoc that documents a Raw
// action's response, its first non-error 200, or -1 when there is none.
func rawStatusIndex(statuses []StatusDoc) int {
	for i, sd := range statuses {
		if sd.Code == http.StatusOK && !sd.Error {
			return i
		}
	}
	return -1
}
