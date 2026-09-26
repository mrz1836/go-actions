package actions_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mrz1836/go-actions"
)

// The README's code blocks are copied from these examples, which `go test`
// compiles and checks against their Output comments.

// CreateUserRequest is the JSON body of users.create.
type CreateUserRequest struct {
	Name  string `json:"name"  validate:"required,max=64"`
	Email string `json:"email" validate:"required,email"`
}

// User is the resource the examples serve. Exported, so the contract names it
// as a component schema.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createUser() actions.Action[CreateUserRequest, actions.Created[User]] {
	return actions.Action[CreateUserRequest, actions.Created[User]]{
		ID:      "users.create",
		Method:  http.MethodPost,
		Path:    "/users",
		Summary: "Create a user",
		Tags:    []string{"users"},
		Statuses: []actions.StatusDoc{
			{Code: http.StatusCreated, Description: "the created user"},
			{Code: http.StatusBadRequest, Description: "malformed JSON", Error: true},
			{Code: http.StatusUnprocessableEntity, Description: "invalid body", Error: true},
		},
		Handle: func(_ context.Context, req CreateUserRequest) (actions.Created[User], error) {
			u := User{ID: "u_1", Name: req.Name, Email: req.Email}
			return actions.Created[User]{Body: u}, nil
		},
	}
}

// send issues one request through h, with a fixed X-Request-ID so the output
// is stable, and returns the status and body.
func send(h http.Handler, method, target, body string) string {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(context.Background(), method, target, rdr)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-ID", "req-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return fmt.Sprint(w.Code, " ", strings.TrimSpace(w.Body.String()))
}

// asJSON renders v as compact JSON, or the marshal error.
func asJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// Declare an action, register it, freeze the registry, and serve it. The
// envelope type sets the runtime status (Created → 201); Statuses states it in
// the contract; Freeze checks that the two agree.
func Example() {
	reg := actions.NewRegistry(actions.WithInfo("Users API", "Manage users.", "1.0.0"))
	actions.Register(reg, createUser())
	reg.Freeze() // checks every declaration and builds the contract

	handler := reg.Handler()
	fmt.Println(send(handler, http.MethodPost, "/users", `{"name":"Ada","email":"ada@example.com"}`))
	fmt.Println(send(handler, http.MethodPost, "/users", `{"name":"","email":"nope"}`))
	fmt.Println(send(handler, http.MethodPost, "/users", `{"name":`))
	// Output:
	// 201 {"id":"u_1","name":"Ada","email":"ada@example.com"}
	// 422 {"error":"validation failed: name: is required; email: must be a valid email address","code":"VALIDATION_ERROR","request_id":"req-1"}
	// 400 {"error":"malformed JSON body: unexpected EOF","code":"BAD_REQUEST","request_id":"req-1"}
}

// GetUserRequest binds only from the URL and headers; json:"-" keeps its
// fields out of the (never-read) GET body and the request-body schema.
type GetUserRequest struct {
	ID     string `json:"-" path:"id" validate:"required"`
	Expand bool   `json:"-" query:"expand"`
	Trace  string `json:"-" header:"X-Trace-Id"`
}

// Path, query, and header parameters bind by struct tag; a value that does not
// convert is a 422 naming the parameter.
func ExampleRegister_parameters() {
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[GetUserRequest, User]{
		ID:      "users.get",
		Method:  http.MethodGet,
		Path:    "/users/{id}",
		Summary: "Fetch a user",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, Description: "the user"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid parameters", Error: true},
		},
		Handle: func(_ context.Context, req GetUserRequest) (User, error) {
			return User{ID: req.ID, Name: fmt.Sprintf("expand=%t", req.Expand)}, nil
		},
	})
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodGet, "/users/u_7?expand=true", ""))
	fmt.Println(send(reg.Handler(), http.MethodGet, "/users/u_7?expand=maybe", ""))
	// Output:
	// 200 {"id":"u_7","name":"expand=true","email":""}
	// 422 {"error":"validation failed: expand: must be a boolean","code":"VALIDATION_ERROR","request_id":"req-1"}
}

// ErrUserNotFound is a domain error the example's mapper translates.
var ErrUserNotFound = errors.New("user not found")

// A custom ErrorMapper receives every error — including the framework's own
// 400/413/422/504 *APIErrors — so it must pass *APIError through, or those
// become 500s.
func ExampleWithErrorMapper() {
	reg := actions.NewRegistry(actions.WithErrorMapper(func(err error) actions.APIError {
		var apiErr *actions.APIError
		if errors.As(err, &apiErr) {
			return *apiErr // keep framework and handler APIErrors as they are
		}
		if errors.Is(err, ErrUserNotFound) {
			return actions.APIError{Status: http.StatusNotFound, Code: actions.CodeNotFound, Message: "user not found"}
		}
		return actions.APIError{Status: http.StatusInternalServerError, Code: actions.CodeInternal, Message: "an internal error occurred"}
	}))
	actions.Register(reg, actions.Action[GetUserRequest, User]{
		ID:     "users.get",
		Method: http.MethodGet,
		Path:   "/users/{id}",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, Description: "the user"},
			{Code: http.StatusNotFound, Description: "no such user", Error: true},
			{Code: http.StatusUnprocessableEntity, Description: "invalid parameters", Error: true},
		},
		Handle: func(context.Context, GetUserRequest) (User, error) {
			return User{}, fmt.Errorf("lookup: %w", ErrUserNotFound)
		},
	})
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodGet, "/users/u_404", ""))
	fmt.Println(send(reg.Handler(), http.MethodGet, "/users/u_404?expand=maybe", ""))
	// Output:
	// 404 {"error":"user not found","code":"NOT_FOUND","request_id":"req-1"}
	// 422 {"error":"validation failed: expand: must be a boolean","code":"VALIDATION_ERROR","request_id":"req-1"}
}

// Strict decoding rejects unknown fields and trailing data, and never echoes
// parser detail.
func ExampleWithStrictDecoding() {
	reg := actions.NewRegistry(actions.WithStrictDecoding())
	actions.Register(reg, createUser())
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodPost, "/users", `{"name":"Ada","email":"ada@example.com","admin":true}`))
	fmt.Println(send(reg.Handler(), http.MethodPost, "/users", `{"name":"Ada","email":"ada@example.com"} {}`))
	// Output:
	// 400 {"error":"malformed JSON body","code":"BAD_REQUEST","request_id":"req-1"}
	// 400 {"error":"malformed JSON body","code":"BAD_REQUEST","request_id":"req-1"}
}

// ListUsersRequest carries the page cursor and size from the query string.
type ListUsersRequest struct {
	Cursor string `json:"-" query:"cursor"`
	Limit  int    `json:"-" query:"limit" validate:"max=100"`
}

// usersAfter stands in for a store query: up to n users whose ID sorts after
// cursor.
func usersAfter(cursor string, n int) []User {
	all := []User{{ID: "u_1", Name: "Ada"}, {ID: "u_2", Name: "Grace"}, {ID: "u_3", Name: "Linus"}}
	start, _ := slices.BinarySearchFunc(all, cursor, func(u User, c string) int { return strings.Compare(u.ID, c+"\x00") })
	return all[start:min(start+n, len(all))]
}

// Cursor pagination: fetch limit+1 rows, and Paginate trims the extra one,
// sets has_more, and takes next_cursor from the last row kept.
func ExamplePaginate() {
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[ListUsersRequest, actions.Page[User]]{
		ID:       "users.list",
		Method:   http.MethodGet,
		Path:     "/users",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "a page of users"}},
		Handle: func(_ context.Context, req ListUsersRequest) (actions.Page[User], error) {
			limit := actions.ClampLimit(req.Limit, 2, 100)
			rows := usersAfter(req.Cursor, limit+1) // one extra row detects a next page
			return actions.Paginate(rows, limit, func(u User) string { return u.ID }), nil
		},
	})
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodGet, "/users", ""))
	fmt.Println(send(reg.Handler(), http.MethodGet, "/users?cursor=u_2", ""))
	// Output:
	// 200 {"items":[{"id":"u_1","name":"Ada","email":""},{"id":"u_2","name":"Grace","email":""}],"next_cursor":"u_2","has_more":true}
	// 200 {"items":[{"id":"u_3","name":"Linus","email":""}],"has_more":false}
}

// NewList wraps a whole collection with its count; a nil slice encodes as [].
func ExampleNewList() {
	fmt.Println(asJSON(actions.NewList([]string{"a", "b"})))
	fmt.Println(asJSON(actions.NewList[string](nil)))
	// Output:
	// {"items":["a","b"],"meta":{"total":2}}
	// {"items":[],"meta":{"total":0}}
}

// Response sets the status and headers at runtime. Its status is not checked
// by Freeze, so document every status the handler can choose.
func ExampleResponse() {
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[GetUserRequest, actions.Response[User]]{
		ID:       "users.get",
		Method:   http.MethodGet,
		Path:     "/users/{id}",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "the user"}},
		Handle: func(_ context.Context, req GetUserRequest) (actions.Response[User], error) {
			return actions.Response[User]{
				Header: http.Header{"Cache-Control": {"max-age=60"}, "Etag": {`"v1"`}},
				Body:   User{ID: req.ID},
			}, nil
		},
	})
	reg.Freeze()

	w := httptest.NewRecorder()
	reg.Handler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/u_1", nil))
	fmt.Println(w.Code, w.Header().Get("Cache-Control"), w.Header().Get("ETag"))
	// Output:
	// 200 max-age=60 "v1"
}

// TransferRequest needs a cross-field rule no tag can express.
type TransferRequest struct {
	From   string `json:"from"   validate:"required"`
	To     string `json:"to"     validate:"required"`
	Amount int    `json:"amount" validate:"min=1"`
}

// Validate is always called, and its failures join the tag rules' in one 422.
func (r TransferRequest) Validate() error {
	if r.From != "" && r.From == r.To {
		return &actions.APIError{Fields: []actions.FieldError{{Field: "to", Message: "must differ from from"}}}
	}
	return nil
}

// Tag rules and a Validatable hook report together.
func ExampleValidatable() {
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[TransferRequest, actions.Accepted[TransferRequest]]{
		ID:     "transfers.create",
		Method: http.MethodPost,
		Path:   "/transfers",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusAccepted, Description: "queued"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid transfer", Error: true},
		},
		Handle: func(_ context.Context, req TransferRequest) (actions.Accepted[TransferRequest], error) {
			return actions.Accepted[TransferRequest]{Body: req}, nil
		},
	})
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodPost, "/transfers", `{"from":"a","to":"a","amount":-5}`))
	// Output:
	// 422 {"error":"validation failed: amount: must be at least 1; to: must differ from from","code":"VALIDATION_ERROR","request_id":"req-1"}
}

// OrderLine and OrderRequest nest validated types.
type (
	OrderLine struct {
		SKU string `json:"sku" validate:"required"`
		Qty int    `json:"qty" validate:"min=1"`
	}
	OrderRequest struct {
		Lines []OrderLine          `json:"lines" validate:"required,max=10"`
		Notes map[string]OrderLine `json:"notes"`
	}
)

// Rules on nested structs, slice elements, and map values are enforced, and
// each failure names its full path.
func Example_nestedValidation() {
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[OrderRequest, actions.Empty]{
		ID:     "orders.create",
		Method: http.MethodPost,
		Path:   "/orders",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusNoContent, Description: "accepted"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid order", Error: true},
		},
		Handle: func(context.Context, OrderRequest) (actions.Empty, error) { return actions.Empty{}, nil },
	})
	reg.Freeze()

	fmt.Println(send(reg.Handler(), http.MethodPost, "/orders", `{"lines":[{"sku":"a","qty":1},{"qty":-1}],"notes":{"gift":{"sku":"b","qty":-2}}}`))
	// Output:
	// 422 {"error":"validation failed: lines[1].sku: is required; lines[1].qty: must be at least 1; notes[gift].qty: must be at least 1","code":"VALIDATION_ERROR","request_id":"req-1"}
}

// Health is a tiny response body for the examples below.
type Health struct {
	OK bool `json:"ok"`
}

// health returns a GET action at path answering {"ok": true}.
func health(id, path string) actions.Action[struct{}, Health] {
	return actions.Action[struct{}, Health]{
		ID:       id,
		Method:   http.MethodGet,
		Path:     path,
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "healthy"}},
		Handle:   func(context.Context, struct{}) (Health, error) { return Health{OK: true}, nil },
	}
}

// Security schemes are declared once and required registry-wide or per
// operation; an explicitly empty Security marks an operation public.
func ExampleWithSecurityScheme() {
	reg := actions.NewRegistry(
		actions.WithSecurityScheme("BearerAuth", actions.BearerAuth("JWT")),
		actions.WithSecurityScheme("ApiKeyAuth", actions.APIKeyAuth("header", "X-API-Key")),
		actions.WithSecurityScheme("BasicAuth", actions.SecurityScheme{Type: "http", Scheme: "basic"}),
		actions.WithSecurity(actions.SecurityRequirement{"BearerAuth": nil}), // the default for every operation
	)
	public := health("health.get", "/health")
	public.Security = []actions.SecurityRequirement{} // no auth for this operation
	admin := health("admin.health", "/admin/health")
	admin.Security = []actions.SecurityRequirement{{"ApiKeyAuth": nil}, {"BasicAuth": nil}} // either one
	actions.Register(reg, public)
	actions.Register(reg, admin)
	actions.Register(reg, health("status.get", "/status")) // inherits BearerAuth
	reg.Freeze()

	var doc struct {
		Security []map[string][]string                `json:"security"`
		Paths    map[string]map[string]map[string]any `json:"paths"`
	}
	_ = json.Unmarshal(reg.OpenAPIJSON(), &doc)
	for _, path := range []string{"/health", "/admin/health", "/status"} {
		fmt.Println(path, asJSON(doc.Paths[path]["get"]["security"]))
	}
	fmt.Println("global", asJSON(doc.Security))
	// Output:
	// /health []
	// /admin/health [{"ApiKeyAuth":[]},{"BasicAuth":[]}]
	// /status null
	// global [{"BearerAuth":[]}]
}

// Mount a registry under a prefix: declare the full paths, strip the prefix
// the router consumes, and mount with chi.
func ExampleWithStripPrefix() {
	reg := actions.NewRegistry(actions.WithStripPrefix("/v1"))
	actions.Register(reg, health("health.get", "/v1/health"))
	reg.Freeze()

	r := chi.NewRouter()
	r.Mount("/v1", reg.Handler())

	fmt.Println(send(r, http.MethodGet, "/v1/health", ""))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/openapi.json", nil))
	fmt.Println(w.Code, strings.Contains(w.Body.String(), `"/v1/health"`))
	// Output:
	// 200 {"ok":true}
	// 200 true
}

// The observer sees each action request once it completes.
func ExampleWithObserver() {
	reg := actions.NewRegistry(actions.WithObserver(func(o actions.Observation) {
		fmt.Println(o.ActionID, o.Method, o.Path, o.Status, o.RequestID, o.Err)
	}))
	actions.Register(reg, createUser())
	reg.Freeze()

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users", strings.NewReader(`{}`))
	r.Header.Set("X-Request-ID", "req-1")
	reg.Handler().ServeHTTP(w, r)
	// Output:
	// users.create POST /users 422 req-1 validation failed
}
