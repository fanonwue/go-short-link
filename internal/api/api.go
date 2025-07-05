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
		Methods []srv.HttpMethod
	}
)

const (
	Prefix = "/_api"
)

func Endpoints() []Endpoint {
	endpoints := []Endpoint{
		{Pattern: Prefix + "/update-mapping", Handler: UpdateMappingHandler, Methods: []srv.HttpMethod{
			srv.POST,
		}},
	}

	for i := range endpoints {
		endpoint := &endpoints[i]

		if endpoint.Methods == nil {
			endpoint.Methods = []srv.HttpMethod{srv.GET}
		}

		endpoints[i].Handler = wrapMiddleware(endpoint)
	}

	return endpoints
}

func unauthorizedHandler(w http.ResponseWriter, r *http.Request) {
	_ = srv.TextResponse(w, r, "Unauthorized", http.StatusUnauthorized)
}

func illegalMethodHandler(w http.ResponseWriter, r *http.Request) {
	_ = srv.TextResponse(w, r, "Method not allowed", http.StatusMethodNotAllowed)
}

func isMethod(method srv.HttpMethod, r *http.Request) bool {
	return srv.HttpMethod(r.Method) == method
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

func wrapMiddleware(endpoint *Endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		newHandler := requireAuthenticated(r, endpoint.Handler)
		newHandler(w, r)
	}
}

func UpdateMappingHandler(w http.ResponseWriter, r *http.Request) {
	if !isMethod(srv.POST, r) {
		illegalMethodHandler(w, r)
		return
	}

	newMap, err := repo.UpdateRedirectMappingDefault(true)
	if err != nil {
		_ = srv.TextResponse(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	responseText := fmt.Sprintf("Update OK, mapping size: %d", len(newMap))
	_ = srv.TextResponse(w, r, responseText, http.StatusOK)
}
