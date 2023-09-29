package main

import (
	"bytes"
	"fmt"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"html/template"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	AppConfig struct {
		IgnoreCaseInPath   bool
		ShowServerHeader   bool
		Port               uint16
		UpdatePeriod       uint32
		HttpCacheMaxAge    uint32
		CacheControlHeader string
	}

	NotFoundTemplateData struct {
		RedirectName string
	}

	// RedirectMap is a map of string keys and string values. The key is meant to be interpreted as the redirect path,
	// which has been provided by the user, while the value represents the redirect target (as in, where the redirect
	// should lead to).
	RedirectMap = map[string]string

	// RedirectMapHook A function that takes a RedirectMap, processes it and returns a new RedirectMap with
	// the processed result.
	RedirectMapHook = func(RedirectMap) RedirectMap

	RedirectMapState struct {
		mapping RedirectMap
		hooks   []RedirectMapHook
		mutex   sync.RWMutex
		channel chan RedirectMap
	}
)

const (
	serverIdentifierHeader     = "go-short-link"
	cacheControlHeaderTemplate = "public, max-age=%d"
	defaultUpdatePeriod        = 300
	minimumUpdatePeriod        = 15
)

var (
	appConfig     *AppConfig
	isProd        bool
	logger        *zap.SugaredLogger
	redirectState = RedirectMapState{
		mapping: RedirectMap{},
		hooks:   make([]RedirectMapHook, 0),
	}
	notFoundTemplate *template.Template
	quitUpdateJob    = make(chan bool)
)

func (state *RedirectMapState) UpdateMapping(newMap RedirectMap) {
	// Synchronize using a mutex to prevent race conditions
	state.mutex.Lock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mutex.Unlock()
	state.mapping = newMap
}

func (state *RedirectMapState) GetTarget(key string) (string, bool) {
	// Synchronize using a mutex to prevent race conditions
	state.mutex.RLock()
	// Defer unlock to make sure it always happens, regardless of panics etc.
	defer state.mutex.RUnlock()
	target, ok := state.mapping[key]
	return target, ok
}

func (state *RedirectMapState) Hooks() []RedirectMapHook {
	return state.hooks
}

func (state *RedirectMapState) AddHook(hook RedirectMapHook) {
	newHooks := append(state.hooks, hook)
	state.hooks = newHooks
}

func (state *RedirectMapState) Channel() chan<- RedirectMap {
	if state.channel == nil {
		state.channel = make(chan RedirectMap)
	}
	return state.channel
}

func (state *RedirectMapState) ListenForUpdates() chan<- RedirectMap {
	channel := state.Channel()
	go state.updateListener()
	return channel
}

func (state *RedirectMapState) updateListener() {
	for mapping := range state.channel {
		state.UpdateMapping(mapping)
		logger.Infof("Updated redirect mapping, number of entries: %d", len(mapping))
	}
}

func CreateAppConfig() *AppConfig {
	ignoreCaseInPath, err := strconv.ParseBool(os.Getenv("IGNORE_CASE_IN_PATH"))
	if err != nil {
		ignoreCaseInPath = true
	}

	showServerHeader, err := strconv.ParseBool(os.Getenv("SHOW_SERVER_HEADER"))
	if err != nil {
		showServerHeader = true
	}

	port, err := strconv.ParseUint(os.Getenv("APP_PORT"), 0, 16)
	if err != nil {
		port = 3000
	}

	updatePeriod, err := strconv.ParseUint(os.Getenv("UPDATE_PERIOD"), 0, 32)
	if err != nil {
		updatePeriod = defaultUpdatePeriod
	}
	if updatePeriod < minimumUpdatePeriod {
		logger.Warnf(
			"UPDATE_PERIOD set to less than %d seconds (minimum), setting it to %d seconds (default)",
			minimumUpdatePeriod, defaultUpdatePeriod)
		updatePeriod = defaultUpdatePeriod
	}

	httpCacheMaxAge, err := strconv.ParseUint(os.Getenv("HTTP_CACHE_MAX_AGE"), 0, 32)
	if err != nil {
		httpCacheMaxAge = updatePeriod * 2
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath:   ignoreCaseInPath,
		ShowServerHeader:   showServerHeader,
		Port:               uint16(port),
		UpdatePeriod:       uint32(updatePeriod),
		HttpCacheMaxAge:    uint32(httpCacheMaxAge),
		CacheControlHeader: fmt.Sprintf(cacheControlHeaderTemplate, httpCacheMaxAge),
	}

	return appConfig
}

func Setup() {
	SetupEnvironment()
	SetupLogging()

	logger.Infof("Running in production mode: %s", strconv.FormatBool(isProd))

	CreateAppConfig()
	CreateSheetsConfig()

	addDefaultRedirectMapHooks()

	notFoundTemplatePath := "./resources/not-found.html"
	t, err := template.ParseFiles(notFoundTemplatePath)
	if err != nil {
		logger.Panicf("Could not load not-found template file %s: %v", notFoundTemplatePath, err)
	}

	notFoundTemplate = t

	fileWebLink, err := SpreadsheetWebLink()
	if err == nil {
		logger.Infof("Using document available at: %s", fileWebLink)
	}

	targetChannel := redirectState.ListenForUpdates()

	UpdateRedirectMapping(targetChannel, true)
	go StartBackgroundUpdates(targetChannel, quitUpdateJob)
}

func SetupEnvironment() {
	_ = godotenv.Load()
	prodEnvValues := []string{"prod", "production"}
	envValue := strings.ToLower(os.Getenv("APP_ENV"))
	isProd = slices.Contains(prodEnvValues, envValue)
}

func SetupLogging() {
	logConfig := zap.NewDevelopmentConfig()
	if isProd {
		logConfig.Development = false
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
		logConfig.OutputPaths = []string{"stdout"}
	}
	baseLogger, _ := logConfig.Build()
	// Make sure to flush logger to avoid mangled output
	defer baseLogger.Sync()
	logger = baseLogger.Sugar()

	// Set zap's globals
	zap.ReplaceGlobals(baseLogger)

	// Set global logger as well
	_, err := zap.RedirectStdLogAt(baseLogger, logConfig.Level.Level())
	if err != nil {
		logger.Errorf("Could not set global logger: %v", err)
	}
}

func ServerHandler(w http.ResponseWriter, r *http.Request) {
	responseHeader := w.Header()
	redirectTarget, ok := RedirectTargetForRequest(r)
	if !ok {
		NotFoundHandler(w, r.URL.Path)
	} else {
		responseHeader["Content-Type"] = nil
		AddDefaultHeaders(responseHeader)
		http.Redirect(w, r, redirectTarget, http.StatusTemporaryRedirect)
	}
}

func RedirectTargetForRequest(r *http.Request) (string, bool) {
	normalizedPath := normalizeRedirectPath(r.URL.Path)

	// Try to find target by hostname if Path is empty
	if len(normalizedPath) == 0 {
		normalizedPath = normalizeRedirectPath(r.Host)
	}

	return redirectState.GetTarget(normalizedPath)
}

func normalizeRedirectPath(path string) string {
	path = strings.Trim(path, "/")
	if appConfig.IgnoreCaseInPath {
		path = strings.ToLower(path)
	}
	return path
}

func NotFoundHandler(w http.ResponseWriter, requestPath string) {
	var renderedBuf bytes.Buffer
	// Pre initialize to 2KiB, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf.Grow(2048)

	err := notFoundTemplate.Execute(&renderedBuf, &NotFoundTemplateData{
		RedirectName: requestPath,
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	responseHeader := w.Header()

	responseHeader.Set("Content-Type", "text/html; charset=utf-8")
	responseHeader.Set("Content-Length", strconv.Itoa(renderedBuf.Len()))
	AddDefaultHeaders(responseHeader)
	w.WriteHeader(http.StatusNotFound)

	_, err = renderedBuf.WriteTo(w)
	if err != nil {
		logger.Errorf("Could not write response body: %v", err)
	}
}

func StartBackgroundUpdates(targetChannel chan<- RedirectMap, quitChannel <-chan bool) {
	logger.Infof("Starting background updates at an interval of %d seconds", appConfig.UpdatePeriod)
	for {
		time.Sleep(time.Duration(appConfig.UpdatePeriod) * time.Second)

		select {
		case <-quitChannel:
			logger.Info("Received quit signal on update job")
			return
		default:
			UpdateRedirectMapping(targetChannel, false)
		}
	}
}

func UpdateRedirectMapping(target chan<- RedirectMap, force bool) {
	if !force && !NeedsUpdate() {
		logger.Debugf("File has not changed since last update, skipping update")
		return
	}

	fetchedMapping := FetchRedirectMapping()

	var newMap = fetchedMapping

	for _, hook := range redirectState.Hooks() {
		newMap = hook(newMap)
	}

	target <- newMap
}

func AddDefaultHeaders(h http.Header) {
	h.Set("Cache-Control", appConfig.CacheControlHeader)
	if appConfig.ShowServerHeader {
		h.Set("Server", serverIdentifierHeader)
	}
}

func addDefaultRedirectMapHooks() {
	// This helper function allows modification of a key using the supplied keyModifierFunc
	// When the modified key differs from the original key, the modified key replaces the
	// original key
	modifyKey := func(redirectMap RedirectMap, key string, keyModifierFunc func(string) string) {
		newKey := keyModifierFunc(key)
		if key != newKey {
			value := redirectMap[key]
			delete(redirectMap, key)
			redirectMap[newKey] = value
		}
	}

	logger.Debug("Adding update hook to strip leading and trailing slashes from redirect paths")
	redirectState.AddHook(func(originalMap RedirectMap) RedirectMap {
		for key := range originalMap {
			modifyKey(originalMap, key, func(s string) string {
				return strings.Trim(s, "/")
			})
		}
		return originalMap
	})

	if appConfig.IgnoreCaseInPath {
		logger.Debug("Adding update hook to make redirect paths lowercase")
		redirectState.AddHook(func(originalMap RedirectMap) RedirectMap {
			// Edit map in place
			for key := range originalMap {
				modifyKey(originalMap, key, func(s string) string {
					return strings.ToLower(s)
				})
			}
			return originalMap
		})
	}
}

func main() {
	Setup()
	// Flush log buffer before exiting
	defer logger.Sync()
	logger.Infof("Starting HTTP server on port %d", appConfig.Port)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", appConfig.Port),
		Handler: http.HandlerFunc(ServerHandler),
	}
	err := server.ListenAndServe()
	if err != nil {
		return
	}
}
