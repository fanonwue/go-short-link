package api

import (
	"fmt"
	"github.com/fanonwue/go-short-link/internal/conf"
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
		endpoints[i].Handler = wrapMiddleware(endpoint)
	}

	return endpoints
}

func unauthorizedHandler(w http.ResponseWriter, r *http.Request) {
	srv.OnUnauthorized("api", w)
}

func illegalMethodHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func isMethod(method srv.HttpMethod, r *http.Request) bool {
	return srv.HttpMethod(r.Method) == method
}

func isAuthenticated(r *http.Request) bool {
	creds := conf.Config().AdminCredentials
	if creds == nil {
		return false
	}
	return srv.CheckCredentials(r, creds)
}

func requireAuthenticated(r *http.Request, next http.HandlerFunc) http.HandlerFunc {
	if isAuthenticated(r) {
		return next
	}
	return unauthorizedHandler
}

func wrapMiddleware(endpoint *Endpoint) http.HandlerFunc {
	originalHandler := endpoint.Handler
	return func(w http.ResponseWriter, r *http.Request) {
		newHandler := requireAuthenticated(r, originalHandler)
		newHandler(w, r)
	}
}

func UpdateMappingHandler(w http.ResponseWriter, r *http.Request) {
	if !isMethod(srv.POST, r) && !isMethod(srv.GET, r) {
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
