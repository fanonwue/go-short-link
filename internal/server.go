package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/fanonwue/go-short-link/internal/api"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/srv"
	"github.com/fanonwue/go-short-link/internal/tmpl"
	"github.com/fanonwue/goutils/logging"
)

const (
	requestTimeout = 10 * time.Second
)

var (
	supportedMethods = []srv.HttpMethod{srv.GET, srv.HEAD, srv.OPTIONS}
)

type wrappedHandler struct {
	handler http.HandlerFunc
}

func supportedMethodsStringSlice() []string {
	methodsStringSlice := make([]string, len(supportedMethods))
	for i, method := range supportedMethods {
		methodsStringSlice[i] = string(method)
	}
	return methodsStringSlice
}

func supportedMethodsString() string {
	return strings.Join(supportedMethodsStringSlice(), ", ")
}

func OptionsHandler(w http.ResponseWriter) {
	h := w.Header()
	srv.AddDefaultHeadersWithCache(h)
	h.Set("Allow", supportedMethodsString())
	w.WriteHeader(http.StatusOK)
}

func (wh wrappedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if srv.HttpMethod(r.Method) == srv.OPTIONS {
		OptionsHandler(w)
		return
	}

	if !slices.Contains(supportedMethods, srv.HttpMethod(r.Method)) {
		errMsg := fmt.Sprintf("Method is not supported - only [%s] are allowed", supportedMethodsString())
		http.Error(w, errMsg, http.StatusMethodNotAllowed)
		return
	}

	wh.handler(w, r)
}

func checkBasicAuth(w http.ResponseWriter, r *http.Request) bool {
	creds := conf.Config().AdminCredentials
	if creds == nil {
		return true
	}

	if srv.CheckCredentials(r, creds) {
		return true
	} else {
		srv.OnUnauthorized("sensitive-status", w)
		return false
	}
}

func wrapHandler(handlerFunc func(http.ResponseWriter, *http.Request)) wrappedHandler {
	return wrappedHandler{
		handler: handlerFunc,
	}
}

func wrapHandlerTimeout(handlerFunc func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.TimeoutHandler(wrapHandler(handlerFunc), requestTimeout, "Request timeout exceeded")
}

func addFaviconHandler(iconType conf.FaviconType, mux *http.ServeMux) {
	favicon, found := conf.Config().FaviconByType(iconType)
	if !found {
		return
	}

	// Only register a handler if the specified favicon is actually a remote address
	isRemote := strings.Contains(favicon, "//")
	if !isRemote {
		return
	}

	mux.Handle(fmt.Sprintf("/favicon.%s", iconType.String()), wrapHandlerTimeout(func(w http.ResponseWriter, r *http.Request) {
		FaviconHandler(w, r, favicon)
	}))
}

func defaultHandlerWithAssets(defaultHandler http.HandlerFunc) http.HandlerFunc {
	if !conf.Config().UseAssets {
		return defaultHandler
	}
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := tmpl.AssetsFS().Open(r.URL.Path)
		if err != nil {
			defaultHandler(w, r)
			return
		}
		defer file.Close()

		stat, _ := file.Stat()
		reader, ok := file.(io.ReadSeeker)
		if !ok {
			logging.Errorf("File does not implement io.ReadSeeker: %s", r.URL.Path)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		srv.AddDefaultHeadersAssets(w.Header())
		http.ServeContent(w, r, r.URL.Path, stat.ModTime(), reader)
	}
}

func CreateHttpServer(shutdown chan<- error, ctx context.Context) *http.Server {
	logging.Infof("Starting HTTP server on port %d", conf.Config().Port)

	mux := http.NewServeMux()

	defaultHandler := wrapHandlerTimeout(defaultHandlerWithAssets(ServerHandler))

	// Default handler
	mux.Handle("/", defaultHandler)

	// Favicons Handler
	for iconType := range conf.Config().Favicons {
		addFaviconHandler(iconType, mux)
	}

	for _, endpoint := range api.Endpoints() {
		mux.Handle(endpoint.Pattern, wrapHandlerTimeout(endpoint.Handler))
	}

	for _, wellKnownFile := range wellKnownFiles() {
		mux.Handle(wellKnownFile, http.StripPrefix(srv.WellKnownPrefix, defaultHandler))
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", conf.Config().Port),
		Handler:      mux,
		ReadTimeout:  requestTimeout,
		WriteTimeout: requestTimeout,
		IdleTimeout:  requestTimeout * 2,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		defer close(shutdown)
		err := httpServer.ListenAndServe()
		logging.Debugf("HTTP Server closed")
		if !errors.Is(err, http.ErrServerClosed) {
			logging.Errorf("Error creating server: %v", err)
			shutdown <- err
		} else {
			shutdown <- nil
		}
	}()
	return httpServer
}

func wellKnownFiles() []string {
	return []string{srv.WellKnownPrefix + "/security.txt"}
}
