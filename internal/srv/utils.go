package srv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/tmpl/minify"
	"github.com/fanonwue/go-short-link/internal/util"
	"net/http"
	"strconv"
	"time"
)

type HttpMethod string

const (
	GET     HttpMethod = http.MethodGet
	POST    HttpMethod = http.MethodPost
	PUT     HttpMethod = http.MethodPut
	DELETE  HttpMethod = http.MethodDelete
	HEAD    HttpMethod = http.MethodHead
	OPTIONS HttpMethod = http.MethodOptions
	PATCH   HttpMethod = http.MethodPatch
	TRACE   HttpMethod = http.MethodTrace
	CONNECT HttpMethod = http.MethodConnect
)

func NoBodyRequest(r *http.Request) bool {
	return r.Method == http.MethodHead
}

func AddDefaultHeaders(h http.Header) {
	if conf.Config().ShowServerHeader {
		h.Set("Server", conf.ServerIdentifierHeader)
	}
}

func AddDefaultHeadersWithCache(h http.Header) {
	AddDefaultHeaders(h)
	h.Set("Cache-Control", conf.Config().CacheControlHeader)
}

func StatusResponse(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	bodyFunc func() (string, *bytes.Buffer, error),
) error {
	h := w.Header()
	AddDefaultHeaders(h)

	contentType, body, err := bodyFunc()

	if err != nil {
		util.Logger().Errorf("Error writing status data to buffer: %v", err)
		http.Error(w, "Unknown Error", http.StatusInternalServerError)
		return err
	}

	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	h.Set("Content-Length", strconv.Itoa(body.Len()))

	w.WriteHeader(status)

	if !NoBodyRequest(r) {
		_, err = body.WriteTo(w)
		if err != nil {
			util.Logger().Errorf("Error writing status data to response body: %v", err)
		}
		return err
	}
	return nil
}

func JsonResponse(w http.ResponseWriter, r *http.Request, body any, status int) error {
	return StatusResponse(w, r, status, func() (string, *bytes.Buffer, error) {
		contentType := "application/json; charset=utf-8"
		buf := util.NewBuffer(conf.DefaultBufferSize)
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return contentType, buf, err
		}
		return contentType, buf, nil
	})
}

func TextResponse(w http.ResponseWriter, r *http.Request, body string, status int) error {
	return StatusResponse(w, r, status, func() (string, *bytes.Buffer, error) {
		return "text/plain; charset=utf-8", bytes.NewBufferString(body), nil
	})
}

func StatusResponseTimeMapper(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func HtmlResponse(w http.ResponseWriter, withBody bool, status int, buffer *bytes.Buffer, etagData string) {
	responseHeader := w.Header()

	AddDefaultHeadersWithCache(responseHeader)

	if minify.EnableMinification {
		newBuf := util.NewBuffer(buffer.Len())
		_, err := newBuf.ReadFrom(minify.FromReader(buffer))
		if err != nil {
			util.Logger().Errorf("Could not minify response: %v", err)
		}
		buffer = newBuf
	}

	responseHeader.Set("Content-Type", "text/html; charset=utf-8")
	responseHeader.Set("Content-Length", strconv.Itoa(buffer.Len()))

	if conf.Config().UseETag && len(etagData) > 0 {
		responseHeader.Set("ETag", EtagFromData(etagData))
	}

	w.WriteHeader(status)

	if withBody {
		_, err := buffer.WriteTo(w)
		if err != nil {
			util.Logger().Errorf("Could not write response body: %v", err)
		}
	}
}

func EtagFromData(data string) string {
	hash := sha256.Sum256([]byte(data))
	return "\"" + hex.EncodeToString(hash[:conf.EtagLength]) + "\""
}
