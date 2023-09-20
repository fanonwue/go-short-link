package main

import (
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
	Port             int64
	UpdatePeriod     int64
}

var appConfig *AppConfig
var isProd bool
var logger *zap.SugaredLogger
var redirectMap = map[string]string{}
var notFoundTemplate *mustache.Template

func createAppConfig() {
	ignoreCaseInPath, err := strconv.ParseBool(os.Getenv("IGNORE_CASE_IN_PATH"))
	if err != nil {
		ignoreCaseInPath = true
	}

	port, err := strconv.ParseInt(os.Getenv("APP_PORT"), 0, 16)
	if err != nil {
		port = 3000
	}

	updatePeriod, err := strconv.ParseInt(os.Getenv("UPDATE_PERIOD"), 0, 16)
	if err != nil {
		updatePeriod = 300
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath: ignoreCaseInPath,
		Port:             port,
		UpdatePeriod:     updatePeriod,
	}
}

func setup() {
	setupEnvironment()
	setupLogging()

	createAppConfig()

	logger.Infof("Running in production mode: %s", strconv.FormatBool(isProd))

	notFoundTemplatePath := "./resources/not-found.mustache"
	template, err := mustache.ParseFile(notFoundTemplatePath)
	if err != nil {
		logger.Panicf("Could not load not-found template file %s: %v", notFoundTemplatePath, err)
		os.Exit(1)
	}

	notFoundTemplate = template

	UpdateRedirectMapping(true)
	go StartBackgroundUpdates()
}

func setupEnvironment() {
	_ = godotenv.Load()
	prodEnvValues := []string{"prod", "production"}
	envValue := strings.ToLower(os.Getenv("APP_ENV"))
	isProd = slices.Contains(prodEnvValues, envValue)
}

func setupLogging() {
	logConfig := zap.NewDevelopmentConfig()
	if isProd {
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
		logConfig.OutputPaths = []string{"stdout"}
	}
	tmpLogger, _ := logConfig.Build()
	defer tmpLogger.Sync()
	logger = tmpLogger.Sugar()
}

func serverHandler(w http.ResponseWriter, r *http.Request) {
	responseHeaders := w.Header()
	trimmedPath := strings.Trim(r.URL.Path, "/")
	if appConfig.IgnoreCaseInPath {
		trimmedPath = strings.ToLower(trimmedPath)
	}
	redirectTarget, ok := redirectMap[trimmedPath]
	if !ok {
		notFoundHandler(w, r.URL.Path)
	} else {
		responseHeaders["Content-Type"] = nil
		http.Redirect(w, r, redirectTarget, http.StatusTemporaryRedirect)
	}
}

func notFoundHandler(w http.ResponseWriter, requestPath string) {
	rendered, err := notFoundTemplate.Render(map[string]string{
		"redirectName": requestPath,
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	responseBytes := []byte(rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(responseBytes)))
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

func main() {
	setup()
	logger.Infof("Starting HTTP server on port %d", appConfig.Port)

	err := http.ListenAndServe(":"+strconv.FormatInt(appConfig.Port, 10), http.HandlerFunc(serverHandler))
	if err != nil {
		return
	}
	logger.Infof("Server ready!")
}
