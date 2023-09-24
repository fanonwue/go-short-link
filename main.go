package main

import (
	"bytes"
	"fmt"
	"github.com/cbroglie/mustache"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type (
	AppConfig struct {
		IgnoreCaseInPath   bool
		Port               uint16
		UpdatePeriod       uint32
		HttpCacheMaxAge    uint32
		CacheControlHeader string
	}

	NotFoundTemplateData struct {
		RedirectName string
	}

	// RedirectMap is a map of string keys and string values. The key is meant to be interpreted as the redirect path,
	// which has been provided by the user, while the value represents the redirect target (as in, where the redirect)
	// should lead to).
	RedirectMap = map[string]string

	// RedirectMapHook A function that takes a RedirectMap, processes it and returns a new RedirectMap with
	// the processed result.
	RedirectMapHook = func(RedirectMap) RedirectMap
)

const (
	cacheControlHeaderTemplate = "public, max-age=%d"
)

var (
	appConfig        *AppConfig
	isProd           bool
	logger           *zap.SugaredLogger
	redirectMap      = RedirectMap{}
	redirectMapHooks = make([]RedirectMapHook, 0)
	notFoundTemplate *mustache.Template
)

func CreateAppConfig() *AppConfig {
	ignoreCaseInPath, err := strconv.ParseBool(os.Getenv("IGNORE_CASE_IN_PATH"))
	if err != nil {
		ignoreCaseInPath = true
	}

	port, err := strconv.ParseUint(os.Getenv("APP_PORT"), 0, 16)
	if err != nil {
		port = 3000
	}

	updatePeriod, err := strconv.ParseUint(os.Getenv("UPDATE_PERIOD"), 0, 32)
	if err != nil {
		updatePeriod = 300
	}

	httpCacheMaxAge, err := strconv.ParseUint(os.Getenv("HTTP_CACHE_MAX_AGE"), 0, 32)
	if err != nil {
		httpCacheMaxAge = updatePeriod * 2
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath:   ignoreCaseInPath,
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

	notFoundTemplatePath := "./resources/not-found.mustache"
	template, err := mustache.ParseFile(notFoundTemplatePath)
	if err != nil {
		logger.Panicf("Could not load not-found template file %s: %v", notFoundTemplatePath, err)
	}

	notFoundTemplate = template

	fileWebLink, err := SpreadsheetWebLink()
	if err == nil {
		logger.Infof("Using document available at: %s", fileWebLink)
	}

	UpdateRedirectMapping(true)
	go StartBackgroundUpdates()
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
	tmpLogger, _ := logConfig.Build()
	// Make sure to flush logger to avoid mangled output
	defer tmpLogger.Sync()
	logger = tmpLogger.Sugar()
}

func ServerHandler(w http.ResponseWriter, r *http.Request) {
	responseHeader := w.Header()
	redirectTarget, ok := RedirectTargetForRequest(r)
	if !ok {
		NotFoundHandler(w, r.URL.Path)
	} else {
		responseHeader["Content-Type"] = nil
		AddDefaultHeaders(&responseHeader)
		http.Redirect(w, r, redirectTarget, http.StatusTemporaryRedirect)
	}
}

func RedirectTargetForRequest(r *http.Request) (string, bool) {
	trimmedPath := strings.Trim(r.URL.Path, "/")
	if appConfig.IgnoreCaseInPath {
		trimmedPath = strings.ToLower(trimmedPath)
	}

	// Try to find target by hostname if Path is empty
	if len(trimmedPath) == 0 {
		trimmedPath = r.Host
	}

	target, ok := redirectMap[trimmedPath]

	return target, ok
}

func NotFoundHandler(w http.ResponseWriter, requestPath string) {
	var renderedBuf bytes.Buffer
	// Pre initialize to 2KiB, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf.Grow(2048)

	err := notFoundTemplate.FRender(&renderedBuf, &NotFoundTemplateData{
		RedirectName: requestPath,
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	responseHeader := w.Header()

	responseHeader.Set("Content-Type", "text/html; charset=utf-8")
	responseHeader.Set("Content-Length", strconv.Itoa(renderedBuf.Len()))
	AddDefaultHeaders(&responseHeader)
	w.WriteHeader(http.StatusNotFound)

	_, err = renderedBuf.WriteTo(w)
	if err != nil {
		logger.Errorf("Could not write response body: %v", err)
	}
}

func StartBackgroundUpdates() {
	logger.Infof("Starting background updates at an interval of %d seconds", appConfig.UpdatePeriod)
	for {
		time.Sleep(time.Duration(appConfig.UpdatePeriod) * time.Second)
		UpdateRedirectMapping(false)
	}
}

func UpdateRedirectMapping(force bool) {
	if !force && !NeedsUpdate() {
		logger.Debugf("File has not changed since last update, skipping update")
		return
	}

	fetchedMapping := FetchRedirectMapping()

	var newMap = fetchedMapping

	for _, hook := range redirectMapHooks {
		newMap = hook(newMap)
	}

	redirectMap = newMap
	logger.Infof("Updated redirect mapping, number of entries: %d", len(newMap))
}

func AddDefaultHeaders(h *http.Header) {
	h.Set("Cache-Control", appConfig.CacheControlHeader)
}

func addRedirectMapHook(hook RedirectMapHook) {
	redirectMapHooks = append(redirectMapHooks, hook)
}

func addDefaultRedirectMapHooks() {
	if appConfig.IgnoreCaseInPath {
		addRedirectMapHook(func(originalMap RedirectMap) RedirectMap {
			// Allocate new map with enough space for all entries after their keys have been made lowercase
			newMap := make(RedirectMap, len(originalMap))
			for key, value := range originalMap {
				newMap[strings.ToLower(key)] = value
			}
			return newMap
		})
	}
}

func main() {
	Setup()
	// Flush log buffer before exiting
	defer logger.Sync()
	logger.Infof("Starting HTTP server on port %d", appConfig.Port)
	err := http.ListenAndServe(":"+strconv.FormatUint(uint64(appConfig.Port), 10), http.HandlerFunc(ServerHandler))
	if err != nil {
		return
	}
}
