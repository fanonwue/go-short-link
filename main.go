package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
		Favicon               string
		AllowRootRedirect     bool
		FallbackFile          string
	}

	NotFoundTemplateData struct {
		RedirectName string
	}

	RedirectInfoTemplateData struct {
		RedirectName string
		Target       string
	}

	ParsedRequest struct {
		Original       *http.Request
		Target         string
		NormalizedPath string
		Found          bool
		InfoRequest    bool
		NoBodyRequest  bool
	}

	StatusHealthcheck struct {
		MappingSize int  `json:"mappingSize"`
		Running     bool `json:"running"`
	}

	StatusInfo struct {
		Mapping       RedirectMap `json:"mapping"`
		SpreadsheetId string      `json:"spreadsheetId"`
		LastUpdate    *time.Time  `json:"lastUpdate"`
		LastModified  *time.Time  `json:"lastModified"`
	}

	FallbackFileEntry struct {
		Key    string `json:"key"`
		Target string `json:"target"`
	}
)

const (
	logResponseTimes           = false
	serverIdentifierHeader     = "go-short-link"
	cacheControlHeaderTemplate = "public, max-age=%d"
	defaultUpdatePeriod        = 300
	minimumUpdatePeriod        = 15
	infoRequestIdentifier      = "+"
	etagLength                 = 8
	envVarPrefix               = "APP_"
	rootRedirectPath           = "__root"
)

var (
	server               *http.Server
	ds                   RedirectDataSource
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

func (ac *AppConfig) UseFallbackFile() bool {
	return len(ac.FallbackFile) > 0
}

func prefixedEnvVar(envVar string) string {
	return envVarPrefix + envVar
}

func CreateAppConfig() *AppConfig {
	port, err := strconv.ParseUint(os.Getenv(prefixedEnvVar("PORT")), 0, 16)
	if err != nil {
		port = 3000
	}

	updatePeriod, err := strconv.ParseUint(os.Getenv(prefixedEnvVar("UPDATE_PERIOD")), 0, 32)
	if err != nil {
		updatePeriod = defaultUpdatePeriod
	}
	if updatePeriod < minimumUpdatePeriod {
		logger.Warnf(
			"UPDATE_PERIOD set to less than %d seconds (minimum), setting it to %d seconds (default)",
			minimumUpdatePeriod, defaultUpdatePeriod)
		updatePeriod = defaultUpdatePeriod
	}

	httpCacheMaxAge, err := strconv.ParseUint(os.Getenv(prefixedEnvVar("HTTP_CACHE_MAX_AGE")), 0, 32)
	if err != nil {
		httpCacheMaxAge = updatePeriod * 2
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath:      boolConfig(prefixedEnvVar("IGNORE_CASE_IN_PATH"), true),
		ShowServerHeader:      boolConfig(prefixedEnvVar("SHOW_SERVER_HEADER"), true),
		Port:                  uint16(port),
		UpdatePeriod:          uint32(updatePeriod),
		HttpCacheMaxAge:       uint32(httpCacheMaxAge),
		CacheControlHeader:    fmt.Sprintf(cacheControlHeaderTemplate, httpCacheMaxAge),
		StatusEndpointEnabled: !boolConfig(prefixedEnvVar("DISABLE_STATUS"), false),
		AdminCredentials:      createAdminCredentials(),
		UseETag:               boolConfig(prefixedEnvVar("ENABLE_ETAG"), true),
		UseRedirectBody:       boolConfig(prefixedEnvVar("ENABLE_REDIRECT_BODY"), true),
		AllowRootRedirect:     boolConfig(prefixedEnvVar("ALLOW_ROOT_REDIRECT"), true),
		Favicon:               os.Getenv(prefixedEnvVar("FAVICON")),
		FallbackFile:          os.Getenv(prefixedEnvVar("FALLBACK_FILE")),
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
	user := os.Getenv(prefixedEnvVar("ADMIN_USER"))
	pass := os.Getenv(prefixedEnvVar("ADMIN_PASS"))

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

func createTemplate(baseTemplate *template.Template, targetTemplatePath string) (*template.Template, error) {
	cloned, err := baseTemplate.Clone()
	if err != nil {
		return nil, err
	}

	return cloned.ParseFiles(targetTemplatePath)
}

func Setup() {
	SetupEnvironment()
	SetupLogging()

	go osSignalHandler()

	logger.Infof("----- STARTING GO-SHORT-LINK SERVER -----")
	logger.Infof("Running in production mode: %s", strconv.FormatBool(isProd))

	CreateAppConfig()
	ds = CreateSheetsDataSource()

	var err error
	resourcePath := "./resources"
	baseTemplate := template.Must(template.ParseFiles(path.Join(resourcePath, "base.gohtml")))
	notFoundTemplatePath := path.Join(resourcePath, "not-found.gohtml")
	redirectInfoTemplatePath := path.Join(resourcePath, "redirect-info.gohtml")

	faviconTemplateString := ""
	if len(appConfig.Favicon) > 0 {
		faviconTemplateString = fmt.Sprintf("{{define \"icon\"}}%s{{end}}", appConfig.Favicon)
	}

	notFoundTemplate = template.Must(createTemplate(baseTemplate, notFoundTemplatePath))

	redirectInfoTemplate, err = createTemplate(baseTemplate, redirectInfoTemplatePath)
	if err != nil {
		logger.Warnf("Could not load redirect-info template file %s: %v", redirectInfoTemplatePath, err)
	}

	if len(faviconTemplateString) > 0 {
		notFoundTemplate = template.Must(notFoundTemplate.Parse(faviconTemplateString))
		if redirectInfoTemplate != nil {
			redirectInfoTemplate = template.Must(redirectInfoTemplate.Parse(faviconTemplateString))
		}
	}

	if appConfig.UseFallbackFile() {
		logger.Infof("Fallback file enabled at path: %s", appConfig.FallbackFile)
	}

	addDefaultRedirectMapHooks()

	targetChannel := redirectState.ListenForUpdates()

	UpdateRedirectMapping(targetChannel, true)
	go StartBackgroundUpdates(targetChannel, quitUpdateJob)
}

func SetupEnvironment() {
	_ = godotenv.Load()
	prodEnvValues := []string{"prod", "production"}
	envValue := strings.ToLower(os.Getenv(prefixedEnvVar("ENV")))
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
	startTime := time.Now()
	pr := RedirectTargetForRequest(r)
	if !pr.Found {
		NotFoundHandler(w, &pr)
	} else if pr.InfoRequest && redirectInfoEndpointEnabled() {
		RedirectInfoHandler(w, &pr)
	} else {
		responseHeader := w.Header()
		AddDefaultHeadersWithCache(responseHeader)

		if appConfig.UseETag {
			etagData := redirectEtag(pr.NormalizedPath, pr.Target, "redirect")
			responseHeader.Set("ETag", etagFromData(etagData))
		}

		if !appConfig.UseRedirectBody || noBodyRequest(r) {
			responseHeader["Content-Type"] = nil
		}

		http.Redirect(w, r, pr.Target, http.StatusTemporaryRedirect)
	}
	if logResponseTimes {
		endTime := time.Now()
		duration := endTime.Sub(startTime)
		logger.Debugf("Request evaluation took %dµs", duration.Microseconds())
	}
}

func noBodyRequest(r *http.Request) bool {
	return r.Method == http.MethodHead
}

func OptionsHandler(w http.ResponseWriter) {
	h := w.Header()
	AddDefaultHeadersWithCache(h)
	h.Set("Allow", "OPTIONS, GET, HEAD")
	w.WriteHeader(http.StatusOK)
}

func statusResponse(w http.ResponseWriter, r *http.Request, body any) error {
	var buf bytes.Buffer
	h := w.Header()
	AddDefaultHeaders(h)

	err := json.NewEncoder(&buf).Encode(body)

	if err != nil {
		logger.Errorf("Error writing status data to buffer: %v", err)
		http.Error(w, "Unknown Error", http.StatusInternalServerError)
		return err
	}

	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))

	if !noBodyRequest(r) {
		_, err = buf.WriteTo(w)
		if err != nil {
			logger.Errorf("Error writing status data to response body: %v", err)
		}
		return err
	}
	return nil
}

func StatusHealthHandler(w http.ResponseWriter, r *http.Request) {
	_ = statusResponse(w, r, StatusHealthcheck{
		MappingSize: redirectState.MappingSize(),
		Running:     true,
	})
}

func StatusInfoHandler(w http.ResponseWriter, r *http.Request) {
	if !checkBasicAuth(w, r) {
		return
	}

	_ = statusResponse(w, r, StatusInfo{
		Mapping:       redirectState.CurrentMapping(),
		SpreadsheetId: ds.Id(),
		LastUpdate:    ds.LastUpdate(),
		LastModified:  ds.LastModified(),
	})
}

func RedirectTargetForRequest(r *http.Request) ParsedRequest {
	pr := ParsedRequest{
		Original: r,
	}

	normalizedPath, infoRequest := normalizeRedirectPath(r.URL.Path)

	pathEmpty := len(normalizedPath) == 0

	// Try to find target by hostname if Path is empty
	if pathEmpty {
		normalizedPath, _ = normalizeRedirectPath(r.Host)
	}

	target, found := redirectState.GetTarget(normalizedPath)

	// Assume it's a domain alias when the target does not start with "http"
	if found && !strings.HasPrefix(target, "http") {
		normalizedPath, _ = normalizeRedirectPath(target)
		target, found = redirectState.GetTarget(target)
	}

	// Ignore infoRequest if there isn't a template loaded for it
	if redirectInfoTemplate == nil {
		infoRequest = false
	}

	// If there's no entry based on hostname, try to use the special root redirect key
	if !found && pathEmpty && appConfig.AllowRootRedirect {
		target, found = redirectState.GetTarget(rootRedirectPath)
	}

	pr.NormalizedPath = normalizedPath
	pr.InfoRequest = infoRequest
	pr.Found = found
	pr.Target = target
	pr.NoBodyRequest = noBodyRequest(r)

	return pr
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

func addLeadingSlash(s string) string {
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func RedirectInfoHandler(w http.ResponseWriter, pr *ParsedRequest) {
	renderedBuf := new(bytes.Buffer)
	renderedBuf.Grow(2048)

	err := redirectInfoTemplate.Execute(renderedBuf, &RedirectInfoTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
		Target:       pr.Target,
	})

	if err != nil {
		logger.Errorf("Could not render redirect-info template: %v", err)
	}

	etagData := redirectEtag(pr.NormalizedPath, pr.Target, "info")

	htmlResponse(w, pr, http.StatusOK, renderedBuf, etagData)
}

func NotFoundHandler(w http.ResponseWriter, pr *ParsedRequest) {
	renderedBuf := new(bytes.Buffer)
	// Pre initialize to 2KiB, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf.Grow(2048)

	err := notFoundTemplate.Execute(renderedBuf, &NotFoundTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
	})

	if err != nil {
		logger.Errorf("Could not render not-found template: %v", err)
	}

	htmlResponse(w, pr, http.StatusNotFound, renderedBuf, "")
}

func htmlResponse(w http.ResponseWriter, pr *ParsedRequest, status int, buffer *bytes.Buffer, etagData string) {
	responseHeader := w.Header()

	AddDefaultHeadersWithCache(responseHeader)

	responseHeader.Set("Content-Type", "text/html; charset=utf-8")
	responseHeader.Set("Content-Length", strconv.Itoa(buffer.Len()))

	if appConfig.UseETag && len(etagData) > 0 {
		responseHeader.Set("ETag", etagFromData(etagData))
	}

	w.WriteHeader(status)

	if !pr.NoBodyRequest {
		_, err := buffer.WriteTo(w)
		if err != nil {
			logger.Errorf("Could not write response body: %v", err)
		}
	}
}

func redirectEtag(requestPath string, target string, suffix string) string {
	return requestPath + "#" + target + "#" + suffix
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
	if !force && !ds.NeedsUpdate() {
		logger.Debugf("File has not changed since last update, skipping update")
		return
	}

	fetchedMapping, fetchErr := ds.FetchRedirectMapping()
	if fetchErr != nil {
		logger.Warnf("Error fetching new redirect mapping: %v", fetchErr)
		if appConfig.UseFallbackFile() {
			fallbackMap, err := readFallbackFile(appConfig.FallbackFile)
			if err != nil {
				logger.Errorf("Redirect map fetch failed, and reading the fallback file failed due to: %v", err)
				return
			}
			fetchedMapping = fallbackMap
			logger.Infof("Read from fallback file")
		} else {
			logger.Warnf("Fallback file disabled")
			return
		}
	}

	var newMap = fetchedMapping

	for _, hook := range redirectState.Hooks() {
		newMap = hook(newMap)
	}

	if fetchErr == nil && appConfig.UseFallbackFile() {
		err := writeFallbackFile(appConfig.FallbackFile, newMap)
		if err != nil {
			logger.Warnf("Error writing fallback file: %v", err)
		}
	}

	target <- newMap
}

func writeFallbackFile(path string, newMapping RedirectMap) error {
	if len(path) == 0 {
		logger.Debugf("Fallback file path is empty, skipping write")
		return nil
	}

	jsonEntries := make([]FallbackFileEntry, len(newMapping))

	i := 0
	for key, target := range newMapping {
		jsonEntries[i] = FallbackFileEntry{
			Key:    key,
			Target: target,
		}
		i++
	}

	jsonBytes, err := json.Marshal(&jsonEntries)
	if err != nil {
		logger.Warnf("Error marshaling fallback file entries to JSON: %v", err)
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		logger.Warnf("Error creating fallback file directory: %v", err)
		return err
	}

	err = os.WriteFile(path, jsonBytes, 0644)
	if err != nil {
		logger.Warnf("Error writing fallback file: %v", err)
		return err
	}

	return nil
}

func readFallbackFile(path string) (RedirectMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Warnf("Error reading fallback file: %v", err)
		return nil, err
	}

	var entries []FallbackFileEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		logger.Warnf("Error unmarshaling fallback file: %v", err)
		return nil, err
	}

	mapping := make(RedirectMap, len(entries))

	for _, entry := range entries {
		mapping[entry.Key] = entry.Target
	}

	return mapping, nil
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

func osSignalHandler() {
	osSignals := make(chan os.Signal, 1)
	capturedSignals := []os.Signal{
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
	}
	signal.Notify(osSignals, capturedSignals...)
	sig := <-osSignals
	logger.Debugf("Received termination signal \"%s\"", sig)
	if server != nil {
		if err := server.Shutdown(context.Background()); err != nil {
			logger.Panic(err)
		}
	} else {
		onExit("Shutdown before server was started")
		os.Exit(0)
	}
}

func onExit(messages ...string) {
	defer logger.Sync()
	if len(messages) == 0 {
		messages = append(messages, "Server stopped")
	}
	for i := range messages {
		logger.Infof(messages[i])
	}
}

func run() error {
	Setup()
	// Flush log buffer before exiting
	defer logger.Sync()

	shutdownChan := make(chan error)
	server = CreateHttpServer(shutdownChan)

	return <-shutdownChan
}

func main() {
	err := run()
	onExit()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unrecoverable Error: %v", err)
		os.Exit(1)
	}
}
