package api

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/repo"
	"github.com/fanonwue/go-short-link/internal/srv"
	"github.com/fanonwue/go-short-link/internal/state"
)

type (
	Endpoint struct {
		// Pattern is the URL pattern that this endpoint matches. The matching will be done by the HTTP server.
		Pattern string
		// Handler the handler that can handle the incoming request
		Handler http.HandlerFunc
		// Anonymous specifies whether anonymous (unauthenticated) access to this endpoint is allowed
		Anonymous bool
	}

	StatusHealthcheck struct {
		MappingSize int        `json:"mappingSize"`
		Running     bool       `json:"running"`
		Healthy     bool       `json:"healthy"`
		LastUpdate  *time.Time `json:"lastUpdate"`
	}

	StatusInfo struct {
		Mapping       state.RedirectMap `json:"mapping"`
		SpreadsheetId string            `json:"spreadsheetId"`
		LastUpdate    *time.Time        `json:"lastUpdate"`
		LastModified  *time.Time        `json:"lastModified"`
		LastError     string            `json:"lastError,omitempty"`
	}
)

const (
	Prefix = "/_api"
	// StatusPrefix is the prefix used for the old status endpoints.
	StatusPrefix = "/_status"
)

func createEndpoints() []Endpoint {
	var apiEndpoints []Endpoint
	var statusEndpoints []Endpoint

	if conf.Config().ApiEnabled {
		apiEndpoints = []Endpoint{
			{Pattern: Prefix + "/update-mapping", Handler: UpdateMappingHandler},
		}
	}

	if conf.Config().StatusEndpointEnabled {
		statusEndpoints = []Endpoint{}
		prefixes := []string{Prefix, StatusPrefix}
		for _, prefix := range prefixes {
			statusEndpoints = append(statusEndpoints, Endpoint{
				Pattern: prefix + "/info",
				Handler: StatusInfoHandler,
			})
			statusEndpoints = append(statusEndpoints, Endpoint{
				Pattern:   prefix + "/health",
				Handler:   StatusHealthHandler,
				Anonymous: true,
			})
		}
	}
	return slices.Concat(apiEndpoints, statusEndpoints)
}

func Endpoints() []Endpoint {
	endpoints := createEndpoints()
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
		newHandler := originalHandler
		if !endpoint.Anonymous {
			newHandler = requireAuthenticated(r, originalHandler)
		}
		newHandler(w, r)
	}
}

func StatusHealthHandler(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK

	healthy := repo.RedirectState().LastError() == nil
	if !healthy {
		status = http.StatusInternalServerError
	}

	_ = srv.JsonResponse(w, r, StatusHealthcheck{
		MappingSize: repo.RedirectState().MappingSize(),
		Running:     true, // FIXME this is hardcoded for now, but if the server isn't running... this will not get executed
		Healthy:     healthy,
		LastUpdate:  srv.StatusResponseTimeMapper(repo.DataSource().LastUpdate()),
	}, status)
}

func StatusInfoHandler(w http.ResponseWriter, r *http.Request) {
	lastError := repo.RedirectState().LastError()
	errorString := ""
	if lastError != nil {
		errorString = lastError.Error()
	}

	_ = srv.JsonResponse(w, r, StatusInfo{
		Mapping:       repo.RedirectState().CurrentMapping(),
		SpreadsheetId: repo.DataSource().Id(),
		LastUpdate:    srv.StatusResponseTimeMapper(repo.DataSource().LastUpdate()),
		LastModified:  srv.StatusResponseTimeMapper(repo.DataSource().LastModified()),
		LastError:     errorString,
	}, http.StatusOK)
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
