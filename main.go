package main

import (
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

type AppConfig struct {
	IgnoreCaseInPath bool
	Port             uint16
	UpdatePeriod     uint32
	HttpCacheMaxAge  uint32
}

var appConfig *AppConfig
var isProd bool
var logger *zap.SugaredLogger
var redirectMap = map[string]string{}
var notFoundTemplate *mustache.Template

func CreateAppConfig() {
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
		httpCacheMaxAge = updatePeriod
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath: ignoreCaseInPath,
		Port:             uint16(port),
		UpdatePeriod:     uint32(updatePeriod),
		HttpCacheMaxAge:  uint32(httpCacheMaxAge),
	}
}

func Setup() {
	SetupEnvironment()
	SetupLogging()
	CreateAppConfig()
	CreateSheetsConfig()

	logger.Infof("Running in production mode: %s", strconv.FormatBool(isProd))

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
	logger = tmpLogger.Sugar()
}

func ServerHandler(w http.ResponseWriter, r *http.Request) {
	responseHeaders := w.Header()
	redirectTarget, ok := RedirectTargetForRequest(r)
	if !ok {
		NotFoundHandler(w, r.URL.Path)
	} else {
		responseHeaders["Content-Type"] = nil
		AddCacheControl(w)
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
	rendered, err := notFoundTemplate.Render(map[string]string{
		"redirectName": requestPath,
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	responseBytes := []byte(rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(responseBytes)))
	AddCacheControl(w)
	w.WriteHeader(http.StatusNotFound)

	_, err = w.Write(responseBytes)
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
	newMap := GetRedirectMapping()
	redirectMap = newMap
	logger.Infof("Updated redirect mapping, number of entries: %d", len(newMap))
}

func AddCacheControl(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", appConfig.HttpCacheMaxAge))
}

func main() {
	Setup()
	// Flush log buffer before exitign
	defer logger.Sync()
	logger.Infof("Starting HTTP server on port %d", appConfig.Port)
	err := http.ListenAndServe(":"+strconv.FormatUint(uint64(appConfig.Port), 10), http.HandlerFunc(ServerHandler))
	if err != nil {
		return
	}
}
