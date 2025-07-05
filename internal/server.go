package internal

import (
	"errors"
	"fmt"
	"github.com/fanonwue/go-short-link/internal/api"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/srv"
	"github.com/fanonwue/go-short-link/internal/util"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	requestTimeout = 10 * time.Second
	statusEndpoint = "/_status"
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
	mux.Handle(fmt.Sprintf("/favicon.%s", string(iconType)), wrapHandlerTimeout(func(w http.ResponseWriter, r *http.Request) {
		FaviconHandler(w, r, favicon)
	}))
}

func CreateHttpServer(shutdown chan<- error) *http.Server {
	util.Logger().Infof("Starting HTTP server on port %d", conf.Config().Port)

	mux := http.NewServeMux()

	// Default handler
	mux.Handle("/", wrapHandlerTimeout(ServerHandler))

	// Favicons Handler
	for iconType := range conf.Config().Favicons {
		addFaviconHandler(iconType, mux)
	}

	if conf.Config().StatusEndpointEnabled {
		mux.Handle(statusEndpoint+"/health", wrapHandlerTimeout(StatusHealthHandler))
		mux.Handle(statusEndpoint+"/info", wrapHandlerTimeout(StatusInfoHandler))
	}

	if conf.Config().ApiEnabled {
		for _, endpoint := range api.Endpoints() {
			mux.Handle(endpoint.Pattern, wrapHandlerTimeout(endpoint.Handler))
		}
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", conf.Config().Port),
		Handler:      mux,
		ReadTimeout:  requestTimeout,
		WriteTimeout: requestTimeout,
		IdleTimeout:  requestTimeout * 2,
	}

	go func() {
		defer close(shutdown)
		err := httpServer.ListenAndServe()
		util.Logger().Debugf("HTTP Server closed")
		if !errors.Is(err, http.ErrServerClosed) {
			util.Logger().Errorf("Error creating server: %v", err)
			shutdown <- err
		} else {
			shutdown <- nil
		}
	}()

	return httpServer
}
