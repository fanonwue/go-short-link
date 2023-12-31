package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"html/template"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

type (
	AdminCredentials struct {
		UserHash []byte
		PassHash []byte
	}

	AppConfig struct {
		IgnoreCaseInPath      bool
		ShowServerHeader      bool
		Port                  uint16
		UpdatePeriod          uint32
		HttpCacheMaxAge       uint32
		CacheControlHeader    string
		StatusEndpointEnabled bool
		UseETag               bool
		UseRedirectBody       bool
		AdminCredentials      *AdminCredentials
	}

	NotFoundTemplateData struct {
		RedirectName string
	}

	RedirectInfoTemplateData struct {
		RedirectName string
		Target       string
	}

	StatusHealthcheck struct {
		MappingSize int  `json:"mappingSize"`
		Running     bool `json:"running"`
	}

	StatusInfo struct {
		Mapping       RedirectMap `json:"mapping"`
		SpreadsheetId string      `json:"spreadsheetId"`
		LastUpdate    *time.Time  `json:"lastUpdate"`
	}

	// RedirectMap is a map of string keys and string values. The key is meant to be interpreted as the redirect path,
	// which has been provided by the user, while the value represents the redirect target (as in, where the redirect
	// should lead to).
	RedirectMap = map[string]string

	// RedirectMapHook A function that takes a RedirectMap, processes it and returns a new RedirectMap with
	// the processed result.
	RedirectMapHook = func(RedirectMap) RedirectMap
)

const (
	serverIdentifierHeader     = "go-short-link"
	cacheControlHeaderTemplate = "public, max-age=%d"
	defaultUpdatePeriod        = 300
	minimumUpdatePeriod        = 15
	infoRequestIdentifier      = "+"
	statusEndpoint             = "/_status/"
	etagLength                 = 8
)

var (
	appConfig            *AppConfig
	isProd               bool
	logger               *zap.SugaredLogger
	notFoundTemplate     *template.Template
	redirectInfoTemplate *template.Template
	quitUpdateJob        = make(chan bool)
	redirectState        = RedirectMapState{
		mapping: RedirectMap{},
		hooks:   make([]RedirectMapHook, 0),
	}
)

func CreateAppConfig() *AppConfig {
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
		IgnoreCaseInPath:      boolConfig("IGNORE_CASE_IN_PATH", true),
		ShowServerHeader:      boolConfig("SHOW_SERVER_HEADER", true),
		Port:                  uint16(port),
		UpdatePeriod:          uint32(updatePeriod),
		HttpCacheMaxAge:       uint32(httpCacheMaxAge),
		CacheControlHeader:    fmt.Sprintf(cacheControlHeaderTemplate, httpCacheMaxAge),
		StatusEndpointEnabled: !boolConfig("DISABLE_STATUS", false),
		AdminCredentials:      createAdminCredentials(),
		UseETag:               boolConfig("ENABLE_ETAG", true),
		UseRedirectBody:       boolConfig("ENABLE_REDIRECT_BODY", true),
	}

	return appConfig
}

func boolConfig(key string, defaultValue bool) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		value = defaultValue
	}
	return value
}

func createAdminCredentials() *AdminCredentials {
	user := os.Getenv("ADMIN_USER")
	pass := os.Getenv("ADMIN_PASS")

	if len(user) == 0 || len(pass) == 0 {
		return nil
	}

	userHash := sha256.Sum256([]byte(user))
	passHash := sha256.Sum256([]byte(pass))

	return &AdminCredentials{
		UserHash: userHash[:],
		PassHash: passHash[:],
	}
}

func Setup() {
	SetupEnvironment()
	SetupLogging()

	logger.Infof("Running in production mode: %s", strconv.FormatBool(isProd))

	CreateAppConfig()
	CreateSheetsConfig()

	var err error
	resourcePath := "./resources"
	baseTemplatePath := path.Join(resourcePath, "base.gohtml")
	notFoundTemplatePath := path.Join(resourcePath, "not-found.gohtml")
	redirectInfoTemplatePath := path.Join(resourcePath, "redirect-info.gohtml")

	notFoundTemplate = template.Must(template.ParseFiles(baseTemplatePath, notFoundTemplatePath))

	redirectInfoTemplate, err = template.ParseFiles(baseTemplatePath, redirectInfoTemplatePath)
	if err != nil {
		logger.Warnf("Could not load redirect-info template file %s: %v", redirectInfoTemplatePath, err)
	}

	addDefaultRedirectMapHooks()

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
	if r.Method != "GET" {
		http.Error(w, "Unsupported Method", http.StatusMethodNotAllowed)
		return
	}

	if appConfig.StatusEndpointEnabled && strings.HasPrefix(strings.TrimRight(r.URL.Path, "/"), statusEndpoint) {
		StatusEndpointHandler(w, r)
		return
	}

	redirectTarget, found, infoRequest := RedirectTargetForRequest(r)
	if !found {
		NotFoundHandler(w, r.URL.Path)
	} else if infoRequest && redirectInfoEndpointEnabled() {
		RedirectInfoHandler(w, r.URL.Path, redirectTarget)
	} else {
		responseHeader := w.Header()
		AddDefaultHeadersWithCache(responseHeader)

		if appConfig.UseETag {
			responseHeader.Set("ETag", etagFromData(redirectTarget))
		}

		if !appConfig.UseRedirectBody {
			responseHeader["Content-Type"] = nil
		}

		http.Redirect(w, r, redirectTarget, http.StatusTemporaryRedirect)
	}
}

func StatusEndpointHandler(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer

	pathParts := strings.Split(r.URL.Path, "/")
	endpoint := pathParts[len(pathParts)-1]

	h := w.Header()
	AddDefaultHeaders(h)

	var body any

	switch endpoint {
	case "health":
		body = StatusHealthcheck{
			MappingSize: redirectState.MappingSize(),
			Running:     true,
		}
		break
	case "info":
		if !checkBasicAuth(w, r) {
			return
		}
		body = StatusInfo{
			Mapping:       redirectState.CurrentMapping(),
			SpreadsheetId: config.SpreadsheetId,
			LastUpdate:    config.LastUpdate,
		}
		break
	}

	if body != nil {
		h.Set("Content-Type", "application/json")
	} else {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	err := json.NewEncoder(&buf).Encode(body)
	if err != nil {
		logger.Errorf("Error writing status data to buffer: %v", err)
		http.Error(w, "Unknown Error", http.StatusInternalServerError)
		return
	}

	h.Set("Content-Length", strconv.Itoa(buf.Len()))

	_, err = buf.WriteTo(w)
	if err != nil {
		logger.Errorf("Error writing status data to response body: %v", err)
	}
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

func RedirectTargetForRequest(r *http.Request) (string, bool, bool) {
	normalizedPath, infoRequest := normalizeRedirectPath(r.URL.Path)

	// Try to find target by hostname if Path is empty
	if len(normalizedPath) == 0 {
		normalizedPath, _ = normalizeRedirectPath(r.Host)
	}

	target, found := redirectState.GetTarget(normalizedPath)

	// Ignore infoRequest if there isn't a template loaded for it
	if redirectInfoTemplate == nil {
		infoRequest = false
	}

	return target, found, infoRequest
}

func normalizeRedirectPath(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if appConfig.IgnoreCaseInPath {
		path = strings.ToLower(path)
	}
	infoRequest := strings.HasSuffix(path, infoRequestIdentifier)
	if infoRequest {
		path = strings.Trim(path, infoRequestIdentifier)
	}
	return path, infoRequest
}

func normalizeRedirectPathKeepLeadingSlash(path string) (string, bool) {
	normalizedPath, infoRequest := normalizeRedirectPath(path)
	if strings.HasPrefix(path, "/") {
		normalizedPath = "/" + normalizedPath
	}
	return normalizedPath, infoRequest
}

func RedirectInfoHandler(w http.ResponseWriter, requestPath string, target string) {
	renderedBuf := new(bytes.Buffer)
	renderedBuf.Grow(2048)

	requestPath, _ = normalizeRedirectPathKeepLeadingSlash(requestPath)

	err := redirectInfoTemplate.Execute(renderedBuf, &RedirectInfoTemplateData{
		RedirectName: requestPath,
		Target:       target,
	})

	if err != nil {
		logger.Errorf("Could not render redirect-info template: %v", err)
	}

	etagData := requestPath + target

	htmlResponse(w, http.StatusOK, renderedBuf, etagData)
}

func NotFoundHandler(w http.ResponseWriter, requestPath string) {
	renderedBuf := new(bytes.Buffer)
	// Pre initialize to 2KiB, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf.Grow(2048)

	requestPath, _ = normalizeRedirectPathKeepLeadingSlash(requestPath)

	err := notFoundTemplate.Execute(renderedBuf, &NotFoundTemplateData{
		RedirectName: requestPath,
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	htmlResponse(w, http.StatusNotFound, renderedBuf, "")
}

func htmlResponse(w http.ResponseWriter, status int, buffer *bytes.Buffer, etagData string) {
	responseHeader := w.Header()

	responseHeader.Set("Content-Type", "text/html; charset=utf-8")
	responseHeader.Set("Content-Length", strconv.Itoa(buffer.Len()))
	AddDefaultHeadersWithCache(responseHeader)

	if appConfig.UseETag && len(etagData) > 0 {
		responseHeader.Set("ETag", etagFromData(etagData))
	}

	w.WriteHeader(status)

	_, err := buffer.WriteTo(w)
	if err != nil {
		logger.Errorf("Could not write response body: %v", err)
	}
}

func etagFromData(data string) string {
	hash := sha256.Sum256([]byte(data))
	return "\"" + hex.EncodeToString(hash[:etagLength]) + "\""
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

	fetchedMapping, err := FetchRedirectMapping()
	if err != nil {
		logger.Warnf("Did not update redirect mapping due to an error")
		return
	}

	var newMap = fetchedMapping

	for _, hook := range redirectState.Hooks() {
		newMap = hook(newMap)
	}

	target <- newMap
}

func AddDefaultHeaders(h http.Header) {
	if appConfig.ShowServerHeader {
		h.Set("Server", serverIdentifierHeader)
	}
}

func AddDefaultHeadersWithCache(h http.Header) {
	AddDefaultHeaders(h)
	h.Set("Cache-Control", appConfig.CacheControlHeader)
}

func redirectInfoEndpointEnabled() bool {
	return redirectInfoTemplate != nil
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

	if redirectInfoEndpointEnabled() {
		logger.Debug("Adding update hook to remove info-request suffix from redirect paths")
		redirectState.AddHook(func(originalMap RedirectMap) RedirectMap {
			// Edit map in place
			for key := range originalMap {
				modifyKey(originalMap, key, func(s string) string {
					return strings.TrimRight(s, infoRequestIdentifier)
				})
			}
			return originalMap
		})
	}

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
