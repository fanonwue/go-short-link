package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/fanonwue/go-short-link/internal/ds"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/tmpl"
	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
		UpdatePeriod          time.Duration
		HttpCacheMaxAge       uint32
		CacheControlHeader    string
		StatusEndpointEnabled bool
		UseETag               bool
		UseRedirectBody       bool
		AdminCredentials      *AdminCredentials
		Favicon               string
		AllowRootRedirect     bool
		FallbackFile          string
		ShowRepositoryLink    bool
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
		MappingSize int        `json:"mappingSize"`
		Running     bool       `json:"running"`
		Healthy     bool       `json:"healthy"`
		LastUpdate  *time.Time `json:"lastUpdate"`
	}

	StatusInfo struct {
		Mapping       state.RedirectMap `json:"mapping"`
		SpreadsheetId string            `json:"spreadsheetId"`
		LastUpdate    *time.Time        `json:"lastUpdate"`
		LastModified  *time.Time        `json:"lastModified"`
		LastError     string            `json:"lastError,omitempty"`
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
	rootRedirectPath           = "__root"
	defaultBufferSize          = 4096
)

var (
	server               *http.Server
	dataSource           ds.RedirectDataSource
	appConfig            *AppConfig
	isProd               bool
	logger               *zap.SugaredLogger
	notFoundTemplate     *template.Template
	redirectInfoTemplate *template.Template
	quitUpdateJob        = make(chan bool)
	redirectState        = state.NewState()
)

func (ac *AppConfig) UseFallbackFile() bool {
	return len(ac.FallbackFile) > 0
}

func CreateAppConfig() *AppConfig {
	port, err := strconv.ParseUint(os.Getenv(util.PrefixedEnvVar("PORT")), 0, 16)
	if err != nil {
		port = 3000
	}

	updatePeriod, err := strconv.ParseUint(os.Getenv(util.PrefixedEnvVar("UPDATE_PERIOD")), 0, 32)
	if err != nil {
		updatePeriod = defaultUpdatePeriod
	}
	if updatePeriod < minimumUpdatePeriod {
		slog.Warn("UPDATE_PERIOD set to less than minimum, setting it to default",
			slog.Uint64("current", updatePeriod),
			slog.Int("min", minimumUpdatePeriod),
			slog.Int("default", defaultUpdatePeriod))
		updatePeriod = defaultUpdatePeriod
	}

	httpCacheMaxAge, err := strconv.ParseUint(os.Getenv(util.PrefixedEnvVar("HTTP_CACHE_MAX_AGE")), 0, 32)
	if err != nil {
		httpCacheMaxAge = updatePeriod * 2
	}

	appConfig = &AppConfig{
		IgnoreCaseInPath:      boolConfig(util.PrefixedEnvVar("IGNORE_CASE_IN_PATH"), true),
		ShowServerHeader:      boolConfig(util.PrefixedEnvVar("SHOW_SERVER_HEADER"), true),
		Port:                  uint16(port),
		UpdatePeriod:          time.Duration(updatePeriod) * time.Second,
		HttpCacheMaxAge:       uint32(httpCacheMaxAge),
		CacheControlHeader:    fmt.Sprintf(cacheControlHeaderTemplate, httpCacheMaxAge),
		StatusEndpointEnabled: !boolConfig(util.PrefixedEnvVar("DISABLE_STATUS"), false),
		AdminCredentials:      createAdminCredentials(),
		UseETag:               boolConfig(util.PrefixedEnvVar("ENABLE_ETAG"), true),
		UseRedirectBody:       boolConfig(util.PrefixedEnvVar("ENABLE_REDIRECT_BODY"), true),
		AllowRootRedirect:     boolConfig(util.PrefixedEnvVar("ALLOW_ROOT_REDIRECT"), true),
		ShowRepositoryLink:    boolConfig(util.PrefixedEnvVar("SHOW_REPOSITORY_LINK"), false),
		Favicon:               os.Getenv(util.PrefixedEnvVar("FAVICON")),
		FallbackFile:          os.Getenv(util.PrefixedEnvVar("FALLBACK_FILE")),
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
	user := os.Getenv(util.PrefixedEnvVar("ADMIN_USER"))
	pass := os.Getenv(util.PrefixedEnvVar("ADMIN_PASS"))

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

	return cloned.ParseFS(tmpl.TemplateFS(), targetTemplatePath)
}

func templateFuncMap() template.FuncMap {
	currentTimeUtc := func() time.Time {
		return time.Now().UTC()
	}

	lastUpdateUtc := func() time.Time {
		return dataSource.LastUpdate().UTC()
	}

	serverName := strings.ToUpper(serverIdentifierHeader)

	return template.FuncMap{
		"showRepositoryLink": func() bool { return appConfig.ShowRepositoryLink },
		"showServerName":     func() bool { return appConfig.ShowServerHeader },
		"serverName":         func() string { return serverName },
		"currentTime":        currentTimeUtc,
		"lastUpdate":         lastUpdateUtc,
		"timestampFormat":    func() string { return time.RFC3339 },
	}
}

func Setup(appContext context.Context) {
	SetupEnvironment()
	SetupLogging()

	slog.Info("----- STARTING GO-SHORT-LINK SERVER -----")
	slog.Info("Mode", "production", strconv.FormatBool(isProd))

	CreateAppConfig()
	dataSource = ds.CreateSheetsDataSource()

	var err error

	baseTemplate := template.Must(
		template.New(tmpl.BaseTemplateName).Funcs(templateFuncMap()).ParseFS(tmpl.TemplateFS(), tmpl.TemplatePath(tmpl.BaseTemplateName)),
	)

	notFoundTemplatePath := tmpl.TemplatePath("not-found.gohtml")
	redirectInfoTemplatePath := tmpl.TemplatePath("redirect-info.gohtml")

	faviconTemplateString := ""
	if len(appConfig.Favicon) > 0 {
		faviconTemplateString = fmt.Sprintf("{{define \"icon\"}}%s{{end}}", appConfig.Favicon)
	}

	if len(faviconTemplateString) > 0 {
		baseTemplate = template.Must(baseTemplate.Parse(faviconTemplateString))
	}

	notFoundTemplate = template.Must(createTemplate(baseTemplate, notFoundTemplatePath))

	redirectInfoTemplate, err = createTemplate(baseTemplate, redirectInfoTemplatePath)
	if err != nil {
		slog.Warn("Could not load redirect-info template file", "path", redirectInfoTemplatePath, "err", err)
	}

	if appConfig.UseFallbackFile() {
		slog.Info("Fallback file enabled at path", "path", appConfig.FallbackFile)
	}

	addDefaultRedirectMapHooks()

	targetChannel := redirectState.ListenForUpdates()
	errorChannel := redirectState.ListenForUpdateErrors()

	UpdateRedirectMapping(targetChannel, errorChannel, true)
	go StartBackgroundUpdates(targetChannel, errorChannel, appContext)
}

func SetupEnvironment() {
	_ = godotenv.Load()
	prodEnvValues := []string{"prod", "production"}
	envValue := strings.ToLower(os.Getenv(util.PrefixedEnvVar("ENV")))
	isProd = slices.Contains(prodEnvValues, envValue)
}

func SetupLogging() {
	log.SetFlags(0)

	opts := slog.HandlerOptions{}

	slog.SetDefault(slog.New(slog.NewTextHandler(util.ConsoleWriter{}, &opts)))
}

func ServerHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	pr := RedirectTargetForRequest(r)
	if !pr.Found {
		NotFoundHandler(w, pr)
	} else if pr.InfoRequest && redirectInfoEndpointEnabled() {
		RedirectInfoHandler(w, pr)
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
		slog.Debug("Request evaluation time", "µs", duration.Microseconds())
	}
}

func noBodyRequest(r *http.Request) bool {
	return r.Method == http.MethodHead
}

func statusResponse(w http.ResponseWriter, r *http.Request, body any, status int) error {
	var buf bytes.Buffer
	h := w.Header()
	AddDefaultHeaders(h)

	err := json.NewEncoder(&buf).Encode(body)

	if err != nil {
		slog.Error("Error writing status data to buffer", "err", err)
		http.Error(w, "Unknown Error", http.StatusInternalServerError)
		return err
	}

	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))

	w.WriteHeader(status)

	if !noBodyRequest(r) {
		_, err = buf.WriteTo(w)
		if err != nil {
			slog.Error("Error writing status data to response body", "err", err)
		}
		return err
	}
	return nil
}

func statusResponseTimeMapper(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func StatusHealthHandler(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK

	healthy := redirectState.LastError() == nil
	if !healthy {
		status = http.StatusInternalServerError
	}

	_ = statusResponse(w, r, StatusHealthcheck{
		MappingSize: redirectState.MappingSize(),
		Running:     server != nil,
		Healthy:     healthy,
		LastUpdate:  statusResponseTimeMapper(dataSource.LastUpdate()),
	}, status)
}

func StatusInfoHandler(w http.ResponseWriter, r *http.Request) {
	if !checkBasicAuth(w, r) {
		return
	}

	lastError := redirectState.LastError()
	errorString := ""
	if lastError != nil {
		errorString = lastError.Error()
	}

	_ = statusResponse(w, r, StatusInfo{
		Mapping:       redirectState.CurrentMapping(),
		SpreadsheetId: dataSource.Id(),
		LastUpdate:    statusResponseTimeMapper(dataSource.LastUpdate()),
		LastModified:  statusResponseTimeMapper(dataSource.LastModified()),
		LastError:     errorString,
	}, http.StatusOK)
}

func RedirectTargetForRequest(r *http.Request) *ParsedRequest {
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

	return &pr
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
	// Pre initialize to the specified buffer size, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf := util.NewBuffer(defaultBufferSize)

	err := redirectInfoTemplate.Execute(renderedBuf, &RedirectInfoTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
		Target:       pr.Target,
	})

	if err != nil {
		slog.Warn("Could not render redirect-info template", "err", err)
	}

	etagData := redirectEtag(pr.NormalizedPath, pr.Target, "info")

	htmlResponse(w, pr, http.StatusOK, renderedBuf, etagData)
}

func NotFoundHandler(w http.ResponseWriter, pr *ParsedRequest) {
	if strings.HasPrefix(pr.NormalizedPath, "favicon.") {
		AddDefaultHeaders(w.Header())
		w.WriteHeader(404)
		return
	}

	// Pre initialize to the specified buffer size, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf := util.NewBuffer(defaultBufferSize)

	err := notFoundTemplate.Execute(renderedBuf, &NotFoundTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
	})

	if err != nil {
		slog.Error("Could not render not-found template", "err", err)
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
			slog.Error("Could not write response body", "err", err)
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

func StartBackgroundUpdates(targetChannel chan<- state.RedirectMap, lastErrorChannel chan<- error, ctx context.Context) {
	slog.Info("Starting background updates", slog.Float64("interval", appConfig.UpdatePeriod.Seconds()))
	ticker := time.NewTicker(appConfig.UpdatePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Update context cancelled")
			return
		case <-ticker.C:
			UpdateRedirectMapping(targetChannel, lastErrorChannel, false)
		}
	}
}

func UpdateRedirectMapping(target chan<- state.RedirectMap, lastError chan<- error, force bool) {
	if !force && !dataSource.NeedsUpdate() && redirectState.LastError() == nil {
		slog.Debug("File has not changed since last update, skipping update")
		return
	}

	fetchedMapping, fetchErr := dataSource.FetchRedirectMapping()
	if fetchErr != nil {
		lastError <- fetchErr
		slog.Warn("Error fetching new redirect mapping", "err", fetchErr)
		if appConfig.UseFallbackFile() {
			fallbackMap, err := readFallbackFileLog(appConfig.FallbackFile)
			if err != nil {
				return
			}
			fetchedMapping = fallbackMap
			slog.Info("Read from fallback file")
		} else {
			slog.Warn("Fallback file disabled")
			return
		}
	} else {
		lastError <- nil
	}

	updateMapping(fetchedMapping, target)

	if fetchErr == nil && appConfig.UseFallbackFile() {
		_ = writeFallbackFileLog(appConfig.FallbackFile, fetchedMapping)
	}
}

func updateMapping(newMap state.RedirectMap, target chan<- state.RedirectMap) {
	for _, hook := range redirectState.Hooks() {
		newMap = hook(newMap)
	}
	target <- newMap
}

func writeFallbackFile(path string, newMapping state.RedirectMap) error {
	if len(path) == 0 {
		slog.Debug("Fallback file path is empty, skipping write")
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
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	err = os.WriteFile(path, jsonBytes, 0644)
	if err != nil {
		return err
	}

	return nil
}

func writeFallbackFileLog(path string, newMapping state.RedirectMap) error {
	err := writeFallbackFile(path, newMapping)
	if err != nil {
		slog.Warn("Error writing fallback file", "err", err)
	}
	return err
}

func readFallbackFile(path string) (state.RedirectMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("Error reading fallback file", "err", err)
		return nil, err
	}

	var entries []FallbackFileEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		slog.Warn("Error unmarshalling fallback file", "err", err)
		return nil, err
	}

	mapping := make(state.RedirectMap, len(entries))

	for _, entry := range entries {
		mapping[entry.Key] = entry.Target
	}

	return mapping, nil
}

func readFallbackFileLog(path string) (state.RedirectMap, error) {
	slog.Info("Reading fallback file")
	fallbackMap, fallbackErr := readFallbackFile(path)
	if fallbackErr != nil {
		slog.Warn("Could not read fallback file", "file", appConfig.FallbackFile, "err", fallbackErr)
	}
	return fallbackMap, fallbackErr
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
	modifyKey := func(redirectMap state.RedirectMap, key string, keyModifierFunc func(string) string) {
		newKey := keyModifierFunc(key)
		if key != newKey {
			value := redirectMap[key]
			delete(redirectMap, key)
			redirectMap[newKey] = value
		}
	}

	slog.Debug("Adding update hook to strip leading and trailing slashes from redirect paths")
	redirectState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
		for key := range originalMap {
			modifyKey(originalMap, key, func(s string) string {
				return strings.Trim(s, "/")
			})
		}
		return originalMap
	})

	if redirectInfoEndpointEnabled() {
		slog.Debug("Adding update hook to remove info-request suffix from redirect paths")
		redirectState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
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
		slog.Debug("Adding update hook to make redirect paths lowercase")
		redirectState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
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

func Run(ctx context.Context) error {
	Setup(ctx)

	shutdownChan := make(chan error)
	server = CreateHttpServer(shutdownChan)

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.Info("Shutting down HTTP server")
		err := server.Shutdown(shutdownContext)
		if err != nil {
			return err
		}
	}

	return <-shutdownChan
}
