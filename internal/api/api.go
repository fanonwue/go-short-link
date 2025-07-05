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
		//{Pattern: Prefix + "/update-mapping", Handler: UpdateMappingHandler},
	}

	return endpoints
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
