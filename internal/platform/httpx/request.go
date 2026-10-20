package httpx

import (
	"context"
	"github.com/google/uuid"
	"net/http"
)

type requestState struct{ route string }
type requestStateKey struct{}

// WithRequestState shares only safe routing metadata across middleware copies.
func WithRequestState(r *http.Request) *http.Request {
	RequestID(r)
	return r.WithContext(context.WithValue(r.Context(), requestStateKey{}, &requestState{}))
}
func RequestID(r *http.Request) string {
	id := r.Header.Get("X-Request-ID")
	if !ValidID(id) {
		id = uuid.NewString()
		r.Header.Set("X-Request-ID", id)
	}
	return id
}
func ObserveRoute(r *http.Request, pattern string) {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.route = pattern
	}
}
func Route(r *http.Request) string {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok && state.route != "" {
		return state.route
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
