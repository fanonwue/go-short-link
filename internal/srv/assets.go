package srv

import (
	"net/http"

	"github.com/fanonwue/go-short-link/internal/conf"
)

func AddDefaultHeadersAssets(h http.Header) {
	AddDefaultHeaders(h)
	h.Set("Cache-Control", conf.Config().AssetsCacheControlHeader)
}
