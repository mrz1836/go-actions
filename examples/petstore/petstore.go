// Package petstore builds the example action registry shared by the runnable
// server example and the OpenAPI snapshot tool. It declares a tiny pet-store API
// in one place so both examples exercise the same contract, demonstrating
// path/query/body binding, a repeated query filter typed by an enum,
// validation tags, and the response envelopes, as well as a create that takes
// an optional Idempotency-Key and a read that carries tool annotations.
package petstore

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/mrz1836/go-actions"
)

// Pet is the example resource exchanged by the API.
type Pet struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tag  string `json:"tag,omitempty"`
}

// PetTag is the kind of pet the list action filters by. Its EnumValues makes
// it an enum: the contract lists the tags once, as the PetTag component, and a
// request naming any other tag is refused.
type PetTag string

// EnumValues lists the tags a pet may carry.
func (PetTag) EnumValues() []PetTag { return []PetTag{"dog", "cat"} }

// listReq carries the optional paging bound and tag filter for the list action.
// The filter is repeated, one value each: GET /pets?tag=dog&tag=cat.
type listReq struct {
	Limit int      `json:"-" query:"limit" validate:"min=0,max=100"`
	Tags  []PetTag `json:"-" query:"tag" validate:"max=2"`
}

// getReq carries the path id for the fetch action.
type getReq struct {
	ID string `json:"-" path:"id" validate:"required"`
}

// createReq is the JSON body for the create action.
type createReq struct {
	Name string `json:"name" validate:"required,min=1,max=64"`
	Tag  string `json:"tag" validate:"max=32"`
}

// store is a tiny in-memory pet repository for the example.
type store struct {
	mu   sync.Mutex
	pets map[string]Pet
	next int
}

// newStore returns a store seeded with a couple of pets.
func newStore() *store {
	return &store{
		pets: map[string]Pet{
			"1": {ID: "1", Name: "Rex", Tag: "dog"},
			"2": {ID: "2", Name: "Mittens", Tag: "cat"},
		},
		next: 3,
	}
}

// Registry builds and returns the example registry, not yet frozen. Callers
// Freeze it (directly or via actiontest.NewServer) before serving.
func Registry() *actions.Registry {
	s := newStore()
	reg := actions.NewRegistry(actions.WithInfo(
		"Pet Store",
		"A tiny example API built with go-actions.",
		"1.0.0",
	),
		// The example has no callers to tell apart, so every key shares one
		// scope; a real service returns the caller's identity here.
		actions.WithIdempotency(actions.NewMemoryIdempotencyStore(), actions.IdempotencyOptions{
			Scope: func(*http.Request) string { return "example" },
		}),
	)
	actions.Register(reg, s.listAction())
	actions.Register(reg, s.getAction())
	actions.Register(reg, s.createAction())
	return reg
}

// listAction declares GET /pets, returning every pet with one of the given
// tags (every pet when none is given), up to the optional limit.
func (s *store) listAction() actions.Action[listReq, []Pet] {
	return actions.Action[listReq, []Pet]{
		ID:       "pets.list",
		Method:   http.MethodGet,
		Path:     "/pets",
		Summary:  "List pets",
		Tags:     []string{"pets"},
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "the pets"}},
		Handle: func(_ context.Context, req listReq) ([]Pet, error) {
			return s.list(req.Limit, req.Tags), nil
		},
	}
}

// getAction declares GET /pets/{id}, returning one pet or a 404.
func (s *store) getAction() actions.Action[getReq, Pet] {
	return actions.Action[getReq, Pet]{
		ID:      "pets.get",
		Method:  http.MethodGet,
		Path:    "/pets/{id}",
		Summary: "Fetch a pet by id",
		Tags:    []string{"pets"},
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, Description: "the pet"},
			{Code: http.StatusNotFound, Description: "no such pet", Error: true},
		},
		Annotations: &actions.Annotations{ReadOnly: true, Idempotent: true},
		Handle: func(_ context.Context, req getReq) (Pet, error) {
			pet, ok := s.get(req.ID)
			if !ok {
				return Pet{}, &actions.APIError{
					Status:  http.StatusNotFound,
					Code:    actions.CodeNotFound,
					Message: "pet not found",
				}
			}
			return pet, nil
		},
	}
}

// createAction declares POST /pets, validating the body and returning 201.
func (s *store) createAction() actions.Action[createReq, actions.Created[Pet]] {
	return actions.Action[createReq, actions.Created[Pet]]{
		ID:      "pets.create",
		Method:  http.MethodPost,
		Path:    "/pets",
		Summary: "Create a pet",
		Tags:    []string{"pets"},
		Statuses: []actions.StatusDoc{
			{Code: http.StatusCreated, Description: "the created pet"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid body", Error: true},
		},
		IdempotencyKey: actions.IdempotencyKeyOptional,
		Handle: func(_ context.Context, req createReq) (actions.Created[Pet], error) {
			return actions.Created[Pet]{Body: s.create(req)}, nil
		},
	}
}

// list returns the stored pets ordered by id, keeping those whose tag is one
// of tags (all of them when tags is empty), capped at limit (0 = no cap).
func (s *store) list(limit int, tags []PetTag) []Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Pet, 0, len(s.pets))
	for _, p := range s.pets {
		if len(tags) == 0 || slices.Contains(tags, PetTag(p.Tag)) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Pet) int { return cmp.Compare(a.ID, b.ID) })
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// get returns the pet with the given id.
func (s *store) get(id string) (Pet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pets[id]
	return p, ok
}

// create stores a new pet and returns it with its assigned id.
func (s *store) create(req createReq) Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strconv.Itoa(s.next)
	s.next++
	pet := Pet{ID: id, Name: req.Name, Tag: req.Tag}
	s.pets[id] = pet
	return pet
}
