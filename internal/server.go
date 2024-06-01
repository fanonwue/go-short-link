package internal

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
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
	supportedMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions}
)

type wrappedHandler struct {
	handler http.HandlerFunc
}

func supportedMethodsString() string {
	return strings.Join(supportedMethods, ", ")
}

func OptionsHandler(w http.ResponseWriter) {
	h := w.Header()
	AddDefaultHeadersWithCache(h)
	h.Set("Allow", supportedMethodsString())
	w.WriteHeader(http.StatusOK)
}

func (wh wrappedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		OptionsHandler(w)
		return
	}

	if !slices.Contains(supportedMethods, r.Method) {
		errMsg := fmt.Sprintf("Method is not supported - only [%s] are allowed", supportedMethodsString())
		http.Error(w, errMsg, http.StatusMethodNotAllowed)
		return
	}

	wh.handler(w, r)
}

func checkBasicAuth(w http.ResponseWriter, r *http.Request) bool {
	creds := appConfig.AdminCredentials
	if creds == nil {
		return true
	}

	onUnauthorized := func() bool {
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	user, pass, ok := r.BasicAuth()
	if !ok {
		return onUnauthorized()
	}

	userHash := sha256.Sum256([]byte(user))
	passHash := sha256.Sum256([]byte(pass))

	userMatched := subtle.ConstantTimeCompare(creds.UserHash, userHash[:]) == 1
	passMatched := subtle.ConstantTimeCompare(creds.PassHash, passHash[:]) == 1

	if userMatched && passMatched {
		return true
	} else {
		return onUnauthorized()
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

func CreateHttpServer(shutdown chan<- error) *http.Server {
	util.Logger().Infof("Starting HTTP server on port %d", appConfig.Port)

	mux := http.NewServeMux()

	// Default handler
	mux.Handle("/", wrapHandlerTimeout(ServerHandler))

	if appConfig.StatusEndpointEnabled {
		mux.Handle(statusEndpoint+"/health", wrapHandlerTimeout(StatusHealthHandler))
		mux.Handle(statusEndpoint+"/info", wrapHandlerTimeout(StatusInfoHandler))
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", appConfig.Port),
		Handler:      mux,
		ReadTimeout:  requestTimeout,
		WriteTimeout: requestTimeout,
		IdleTimeout:  requestTimeout * 2,
	}

	go func() {
		err := srv.ListenAndServe()
		util.Logger().Debugf("HTTP Server closed")
		if !errors.Is(err, http.ErrServerClosed) {
			util.Logger().Errorf("Error creating server: %v", err)
			shutdown <- err
		} else {
			shutdown <- nil
		}
	}()

	return srv
}
