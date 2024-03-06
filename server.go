package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

const (
	requestTimeout = 5 * time.Second
	statusEndpoint = "/_status"
)

var (
	supportedMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions}
)

type wrappedHandler struct {
	handler http.HandlerFunc
}

func (wh wrappedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		OptionsHandler(w)
		return
	}

	if !slices.Contains(supportedMethods, r.Method) {
		http.Error(w, "Method Not Allowed - only GET, HEAD and OPTIONS are allowed", http.StatusMethodNotAllowed)
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
	logger.Infof("Starting HTTP server on port %d", appConfig.Port)

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
		IdleTimeout:  10 * time.Second,
	}

	go func() {
		err := srv.ListenAndServe()
		logger.Debugf("HTTP Server closed")
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf("Error creating server: %v", err)
			shutdown <- err
		} else {
			shutdown <- nil
		}
	}()

	return srv
}
