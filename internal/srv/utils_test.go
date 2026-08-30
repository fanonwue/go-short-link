package srv

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/util"
)

func TestRequestBodyClassification(t *testing.T) {
	tests := []struct {
		method   string
		noBody   bool
		withBody bool
	}{
		{http.MethodHead, true, false},
		{http.MethodOptions, true, false},
		{http.MethodGet, false, true},
		{http.MethodPost, false, true},
		{"CUSTOM", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "/", nil)
			if got := NoBodyRequest(r); got != tt.noBody {
				t.Errorf("NoBodyRequest() = %v, want %v", got, tt.noBody)
			}
			if got := WithBodyRequest(r); got != tt.withBody {
				t.Errorf("WithBodyRequest() = %v, want %v", got, tt.withBody)
			}
		})
	}
}

func TestDefaultHeaders(t *testing.T) {
	cfg := conf.Config()
	oldShowServerHeader, oldCacheControl := cfg.ShowServerHeader, cfg.CacheControlHeader
	t.Cleanup(func() {
		cfg.ShowServerHeader = oldShowServerHeader
		cfg.CacheControlHeader = oldCacheControl
	})

	t.Run("server header enabled", func(t *testing.T) {
		cfg.ShowServerHeader = true
		h := make(http.Header)
		AddDefaultHeaders(h)
		if got := h.Get("Server"); got != conf.ServerIdentifierHeader {
			t.Errorf("Server = %q, want %q", got, conf.ServerIdentifierHeader)
		}
	})

	t.Run("server header disabled", func(t *testing.T) {
		cfg.ShowServerHeader = false
		h := make(http.Header)
		AddDefaultHeaders(h)
		if got := h.Get("Server"); got != "" {
			t.Errorf("Server = %q, want empty", got)
		}
	})

	t.Run("cache header", func(t *testing.T) {
		cfg.ShowServerHeader = true
		cfg.CacheControlHeader = "public, max-age=123"
		h := make(http.Header)
		AddDefaultHeadersWithCache(h)
		if got := h.Get("Cache-Control"); got != cfg.CacheControlHeader {
			t.Errorf("Cache-Control = %q, want %q", got, cfg.CacheControlHeader)
		}
		if got := h.Get("Server"); got != conf.ServerIdentifierHeader {
			t.Errorf("Server = %q, want %q", got, conf.ServerIdentifierHeader)
		}
	})
}

func TestStatusResponse(t *testing.T) {
	t.Run("writes status headers and body", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		called := 0
		err := StatusResponse(w, r, http.StatusCreated, func() (string, *bytes.Buffer, error) {
			called++
			return "application/example", bytes.NewBufferString("response body"), nil
		})

		if err != nil {
			t.Fatalf("StatusResponse() error = %v", err)
		}
		if called != 1 {
			t.Errorf("body function called %d times, want 1", called)
		}
		assertResponse(t, w, http.StatusCreated, "application/example", "response body")
	})

	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		t.Run(method+" suppresses body but retains its length", func(t *testing.T) {
			r := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()
			err := StatusResponse(w, r, http.StatusNoContent, func() (string, *bytes.Buffer, error) {
				return "text/custom", bytes.NewBufferString("hidden"), nil
			})
			if err != nil {
				t.Fatalf("StatusResponse() error = %v", err)
			}
			if w.Code != http.StatusNoContent {
				t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
			}
			if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len("hidden")) {
				t.Errorf("Content-Length = %q, want %d", got, len("hidden"))
			}
			if w.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", w.Body.String())
			}
		})
	}

	t.Run("omits empty content type", func(t *testing.T) {
		w := httptest.NewRecorder()
		err := StatusResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK,
			func() (string, *bytes.Buffer, error) { return "", bytes.NewBufferString("x"), nil })
		if err != nil {
			t.Fatal(err)
		}
		if got := w.Header().Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q, want empty", got)
		}
	})

	t.Run("body creation error becomes internal server error", func(t *testing.T) {
		wantErr := errors.New("cannot create body")
		w := httptest.NewRecorder()
		err := StatusResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusAccepted,
			func() (string, *bytes.Buffer, error) { return "text/custom", new(bytes.Buffer), wantErr })
		if !errors.Is(err, wantErr) {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		assertHTTPError(t, w, http.StatusInternalServerError, "Unknown Error\n")
	})

	t.Run("returns response writer error", func(t *testing.T) {
		wantErr := errors.New("write failed")
		w := &failingResponseWriter{header: make(http.Header), err: wantErr}
		err := StatusResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK,
			func() (string, *bytes.Buffer, error) { return "text/plain", bytes.NewBufferString("body"), nil })
		if !errors.Is(err, wantErr) {
			t.Errorf("error = %v, want %v", err, wantErr)
		}
		if w.status != http.StatusOK {
			t.Errorf("status = %d, want %d", w.status, http.StatusOK)
		}
	})
}

func TestJsonResponse(t *testing.T) {
	t.Run("encodes JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		err := JsonResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), map[string]any{"ok": true}, http.StatusAccepted)
		if err != nil {
			t.Fatal(err)
		}
		assertResponse(t, w, http.StatusAccepted, "application/json; charset=utf-8", "{\"ok\":true}")
	})

	t.Run("returns encoding errors", func(t *testing.T) {
		w := httptest.NewRecorder()
		err := JsonResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), make(chan int), http.StatusOK)
		if err == nil {
			t.Fatal("JsonResponse() error = nil, want encoding error")
		}
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
		}
		if got := w.Body.String(); got != "Unknown Error\n" {
			t.Errorf("body = %q, want %q", got, "Unknown Error\n")
		}
	})
}

func TestTextResponse(t *testing.T) {
	w := httptest.NewRecorder()
	err := TextResponse(w, httptest.NewRequest(http.MethodGet, "/", nil), "hello", http.StatusTeapot)
	if err != nil {
		t.Fatal(err)
	}
	assertResponse(t, w, http.StatusTeapot, "text/plain; charset=utf-8", "hello")
}

func TestStatusResponseTimeMapper(t *testing.T) {
	if got := StatusResponseTimeMapper(time.Time{}); got != nil {
		t.Errorf("zero time mapped to %v, want nil", got)
	}
	want := time.Date(2026, time.August, 30, 12, 0, 0, 123, time.UTC)
	got := StatusResponseTimeMapper(want)
	if got == nil || !got.Equal(want) {
		t.Errorf("non-zero time mapped to %v, want %v", got, want)
	}
}

func TestHtmlResponse(t *testing.T) {
	cfg := conf.Config()
	oldShowServerHeader, oldCacheControl, oldUseETag := cfg.ShowServerHeader, cfg.CacheControlHeader, cfg.UseETag
	t.Cleanup(func() {
		cfg.ShowServerHeader = oldShowServerHeader
		cfg.CacheControlHeader = oldCacheControl
		cfg.UseETag = oldUseETag
	})
	cfg.ShowServerHeader = true
	cfg.CacheControlHeader = "private, max-age=42"
	cfg.UseETag = true

	t.Run("writes complete HTML response", func(t *testing.T) {
		w := httptest.NewRecorder()
		HtmlResponse(w, true, http.StatusCreated, bytes.NewBufferString("<p>Hello</p>"), "etag input")
		assertResponse(t, w, http.StatusCreated, "text/html; charset=utf-8", "<p>Hello</p>")
		if got := w.Header().Get("Cache-Control"); got != cfg.CacheControlHeader {
			t.Errorf("Cache-Control = %q, want %q", got, cfg.CacheControlHeader)
		}
		if got := w.Header().Get("ETag"); got != EtagFromData("etag input") {
			t.Errorf("ETag = %q, want %q", got, EtagFromData("etag input"))
		}
	})

	t.Run("suppresses body", func(t *testing.T) {
		w := httptest.NewRecorder()
		HtmlResponse(w, false, http.StatusOK, bytes.NewBufferString("hidden"), "data")
		if w.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", w.Body.String())
		}
		if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len("hidden")) {
			t.Errorf("Content-Length = %q, want %d", got, len("hidden"))
		}
	})

	for _, tt := range []struct {
		name     string
		useETag  bool
		etagData string
	}{
		{"disabled", false, "data"},
		{"empty data", true, ""},
	} {
		t.Run("omits ETag when "+tt.name, func(t *testing.T) {
			cfg.UseETag = tt.useETag
			w := httptest.NewRecorder()
			HtmlResponse(w, true, http.StatusOK, bytes.NewBufferString("body"), tt.etagData)
			if got := w.Header().Get("ETag"); got != "" {
				t.Errorf("ETag = %q, want empty", got)
			}
		})
	}
}

func TestEtagFromData(t *testing.T) {
	const want = `"d7a8fbb307d78094"`
	if got := EtagFromData("The quick brown fox jumps over the lazy dog"); got != want {
		t.Errorf("EtagFromData() = %q, want %q", got, want)
	}
	if got := EtagFromData(""); len(got) != conf.EtagLength*2+2 || !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("EtagFromData(empty) = %q, want quoted %d-byte hash", got, conf.EtagLength)
	}
}

func TestCheckCredentials(t *testing.T) {
	userHash, err := util.HashPassword([]byte("admin"))
	if err != nil {
		t.Fatal(err)
	}
	passHash, err := util.HashPassword([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	creds := &conf.AdminCredentials{UserHash: userHash, PassHash: passHash}

	tests := []struct {
		name    string
		user    string
		pass    string
		setAuth bool
		creds   *conf.AdminCredentials
		want    bool
	}{
		{"matching", "admin", "secret", true, creds, true},
		{"wrong user", "other", "secret", true, creds, false},
		{"wrong password", "admin", "wrong", true, creds, false},
		{"missing authorization", "", "", false, creds, false},
		{"nil credentials", "admin", "secret", true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.setAuth {
				r.SetBasicAuth(tt.user, tt.pass)
			}
			if got := CheckCredentials(r, tt.creds); got != tt.want {
				t.Errorf("CheckCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	OnUnauthorized("admin area", w)
	assertHTTPError(t, w, http.StatusUnauthorized, "Unauthorized\n")
	if got, want := w.Header().Get("WWW-Authenticate"), `Basic realm="admin area", charset="UTF-8"`; got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func assertHTTPError(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("status = %d, want %d", w.Code, status)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
	if got := w.Body.String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func assertResponse(t *testing.T, w *httptest.ResponseRecorder, status int, contentType, body string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("status = %d, want %d", w.Code, status)
	}
	if got := w.Header().Get("Content-Type"); got != contentType {
		t.Errorf("Content-Type = %q, want %q", got, contentType)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %d", got, len(body))
	}
	if got := w.Body.String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

type failingResponseWriter struct {
	header http.Header
	status int
	err    error
}

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) WriteHeader(status int)    { w.status = status }
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, w.err }
