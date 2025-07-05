package api

import (
	"fmt"
	"github.com/fanonwue/go-short-link/internal/repo"
	"github.com/fanonwue/go-short-link/internal/srv"
	"net/http"
)

type (
	Endpoint struct {
		Pattern string
		Handler http.HandlerFunc
	}
)

const (
	Prefix = "/_api"
)

func Endpoints() []Endpoint {
	endpoints := []Endpoint{
		{Pattern: Prefix + "/update-mapping", Handler: UpdateMappingHandler},
	}

	for i := range endpoints {
		endpoint := &endpoints[i]
		endpoints[i].Handler = wrapMiddleware(endpoint.Handler)
	}

	return endpoints
}

func unauthorizedHandler(w http.ResponseWriter, r *http.Request) {
	_ = srv.TextResponse(w, r, "Unauthorized", http.StatusUnauthorized)
}

func isAuthenticated(r *http.Request) bool {
	// TODO Implement this
	return false
}

func requireAuthenticated(r *http.Request, next http.HandlerFunc) http.HandlerFunc {
	if isAuthenticated(r) {
		return next
	}
	return unauthorizedHandler
}

func wrapMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		newHandler := requireAuthenticated(r, handler)
		newHandler(w, r)
	}
}

func UpdateMappingHandler(w http.ResponseWriter, r *http.Request) {
	newMap, err := repo.UpdateRedirectMappingDefault(true)
	if err != nil {
		_ = srv.TextResponse(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	responseText := fmt.Sprintf("Update OK, mapping size: %d", len(newMap))
	_ = srv.TextResponse(w, r, responseText, http.StatusOK)
}
