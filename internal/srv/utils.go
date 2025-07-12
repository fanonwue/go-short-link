package srv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/tmpl/minify"
	"github.com/fanonwue/go-short-link/internal/util"
	"net/http"
	"strconv"
	"time"
)

type HttpMethod string

// BodyFunc A function that provides the content type, the actual body (in bytes)
// and any error that occurred while producing the body
type BodyFunc func() (contentType string, body *bytes.Buffer, err error)

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
	switch HttpMethod(r.Method) {
	case HEAD, OPTIONS:
		return true
	default:
		return false
	}
}

func WithBodyRequest(r *http.Request) bool {
	return !NoBodyRequest(r)
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
	bodyFunc BodyFunc,
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

	if WithBodyRequest(r) {
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

func CheckCredentials(r *http.Request, creds *conf.AdminCredentials) bool {
	if creds == nil {
		return false
	}

	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}

	userMatchErr := util.ComparePasswords([]byte(user), creds.UserHash)
	passMatchErr := util.ComparePasswords([]byte(pass), creds.PassHash)

	return userMatchErr == nil && passMatchErr == nil
}

func OnUnauthorized(realm string, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s", charset="UTF-8"`, realm))
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}
