package internal

import (
	"context"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/repo"
	"github.com/fanonwue/go-short-link/internal/srv"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/tmpl"
	"github.com/fanonwue/go-short-link/internal/tmpl/minify"
	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type (
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
)

const (
	infoRequestIdentifier = "+"
	rootRedirectPath      = "__root"
)

var (
	server               *http.Server
	logger               *zap.SugaredLogger
	notFoundTemplate     *template.Template
	redirectInfoTemplate *template.Template
	quitUpdateJob        = make(chan bool)
)

func templateFuncMap() template.FuncMap {
	currentTimeUtc := func() time.Time {
		return time.Now().UTC()
	}

	lastUpdateUtc := func() time.Time {
		return repo.DataSource().LastUpdate().UTC()
	}

	serverName := strings.ToUpper(conf.ServerIdentifierHeader)

	return template.FuncMap{
		"showRepositoryLink": func() bool { return conf.Config().ShowRepositoryLink },
		"showServerName":     func() bool { return conf.Config().ShowServerHeader },
		"serverName":         func() string { return serverName },
		"currentTime":        currentTimeUtc,
		"lastUpdate":         lastUpdateUtc,
		"timestampFormat":    func() string { return time.RFC3339 },
		"favicons":           func() []conf.FaviconEntry { return conf.Config().FaviconEntries() },
	}
}

func Setup(appContext context.Context) {
	SetupEnvironment()
	SetupLogging()

	util.Logger().Infof("----- STARTING GO-SHORT-LINK SERVER -----")
	util.Logger().Infof("Running in production mode: %s", strconv.FormatBool(conf.IsProd()))

	conf.CreateAppConfig()
	repo.Setup(appContext)

	var err error

	tpc := tmpl.NewTemplateParserContext()

	tpc.SetFuncMap(templateFuncMap())

	template.Must(
		tpc.ParseBaseTemplateFile(tmpl.BaseTemplateName),
	)

	if conf.Config().HasFavicons() {
		template.Must(tpc.ParseBaseTemplateFile(tmpl.TemplatePath("favicons.gohtml")))
	}

	notFoundTemplatePath := tmpl.TemplatePath("not-found.gohtml")
	redirectInfoTemplatePath := tmpl.TemplatePath("redirect-info.gohtml")

	notFoundTemplate = template.Must(tpc.ParseTemplateFile(notFoundTemplatePath))

	redirectInfoTemplate, err = tpc.ParseTemplateFile(redirectInfoTemplatePath)
	if err != nil {
		util.Logger().Warnf("Could not load redirect-info template file %s: %v", redirectInfoTemplatePath, err)
	}

	if conf.Config().UseFallbackFile() {
		util.Logger().Infof("Fallback file enabled at path: %s", conf.Config().FallbackFile)
	}

	if minify.EnableMinification {
		util.Logger().Info("Response minification enabled")
	}

	addDefaultRedirectMapHooks(repo.RedirectState())

	_, _ = repo.UpdateRedirectMappingDefault(false)
	go StartBackgroundUpdates(appContext)
}

func SetupEnvironment() {
	_ = godotenv.Load()
}

func SetupLogging() {
	logConfig := zap.NewDevelopmentConfig()
	logConfig.OutputPaths = []string{"stdout"}
	if conf.IsProd() {
		logConfig.Development = false
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	baseLogger, _ := logConfig.Build()
	// Make sure to flush logger to avoid mangled output
	defer baseLogger.Sync()
	logger = baseLogger.Sugar()

	// Set zap's globals
	zap.ReplaceGlobals(baseLogger)
	util.SetLogger(logger)

	// Set global logger as well
	_, err := zap.RedirectStdLogAt(baseLogger, logConfig.Level.Level())
	if err != nil {
		util.Logger().Errorf("Could not set global logger: %v", err)
	}
}

func ServerHandler(w http.ResponseWriter, r *http.Request) {
	var startTime time.Time
	if conf.LogResponseTimes {
		startTime = time.Now()
	}

	pr := RedirectTargetForRequest(r)
	if !pr.Found {
		NotFoundHandler(w, pr)
	} else if pr.InfoRequest && redirectInfoEndpointEnabled() {
		RedirectInfoHandler(w, pr)
	} else {
		responseHeader := w.Header()
		srv.AddDefaultHeadersWithCache(responseHeader)

		if conf.Config().UseETag {
			etagData := redirectEtag(pr.NormalizedPath, pr.Target, "redirect")
			responseHeader.Set("ETag", srv.EtagFromData(etagData))
		}

		if !conf.Config().UseRedirectBody || srv.NoBodyRequest(r) {
			responseHeader["Content-Type"] = nil
		}

		http.Redirect(w, r, pr.Target, http.StatusTemporaryRedirect)
	}
	if conf.LogResponseTimes {
		endTime := time.Now()
		duration := endTime.Sub(startTime)
		util.Logger().Debugf("Request evaluation took %dµs", duration.Microseconds())
	}
}

func FaviconHandler(w http.ResponseWriter, r *http.Request, favicon string) {
	http.Redirect(w, r, favicon, http.StatusTemporaryRedirect)
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

	target, found := repo.RedirectState().GetTarget(normalizedPath)

	// Assume it's a domain alias when the target does not start with "http"
	if found && !strings.HasPrefix(target, "http") {
		normalizedPath, _ = normalizeRedirectPath(target)
		target, found = repo.RedirectState().GetTarget(target)
	}

	// Ignore infoRequest if there isn't a template loaded for it
	if redirectInfoTemplate == nil {
		infoRequest = false
	}

	// If there's no entry based on hostname, try to use the special root redirect key
	if !found && pathEmpty && conf.Config().AllowRootRedirect {
		target, found = repo.RedirectState().GetTarget(rootRedirectPath)
	}

	pr.NormalizedPath = normalizedPath
	pr.InfoRequest = infoRequest
	pr.Found = found
	pr.Target = target
	pr.NoBodyRequest = srv.NoBodyRequest(r)

	return &pr
}

func normalizeRedirectPath(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if conf.Config().IgnoreCaseInPath {
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
	renderedBuf := util.NewBuffer(conf.DefaultBufferSize)

	err := redirectInfoTemplate.Execute(renderedBuf, &RedirectInfoTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
		Target:       pr.Target,
	})

	if err != nil {
		util.Logger().Errorf("Could not render redirect-info template: %v", err)
	}

	etagData := redirectEtag(pr.NormalizedPath, pr.Target, "info")

	srv.HtmlResponse(w, !pr.NoBodyRequest, http.StatusOK, renderedBuf, etagData)
}

func NotFoundHandler(w http.ResponseWriter, pr *ParsedRequest) {
	if strings.HasPrefix(pr.NormalizedPath, "favicon.") {
		srv.AddDefaultHeaders(w.Header())
		w.WriteHeader(404)
		return
	}

	// Pre initialize to the specified buffer size, as the response will be bigger than 1KiB due to the size of the template
	renderedBuf := util.NewBuffer(conf.DefaultBufferSize)

	err := notFoundTemplate.Execute(renderedBuf, &NotFoundTemplateData{
		RedirectName: addLeadingSlash(pr.NormalizedPath),
	})

	if err != nil {
		util.Logger().Errorf("Could not render not-found template: %v", err)
	}

	srv.HtmlResponse(w, !pr.NoBodyRequest, http.StatusNotFound, renderedBuf, "")
}

func redirectEtag(requestPath string, target string, suffix string) string {
	var builder strings.Builder

	// Pre-allocate capacity to avoid reallocations
	// Estimate: len(requestPath) + len(target) + len(suffix) + 2 (for "#" characters)
	builder.Grow(len(requestPath) + len(target) + len(suffix) + 2)

	builder.WriteString(requestPath)
	builder.WriteByte('#')
	builder.WriteString(target)
	builder.WriteByte('#')
	builder.WriteString(suffix)

	return builder.String()

}

func StartBackgroundUpdates(ctx context.Context) {
	util.Logger().Infof("Starting background updates at an interval of %.0f seconds", conf.Config().UpdatePeriod.Seconds())
	ticker := time.NewTicker(conf.Config().UpdatePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			util.Logger().Info("Update context cancelled")
			return
		case <-ticker.C:
			repo.UpdateRedirectMappingChannels(nil, nil, false)
		}
	}
}

func updateMapping(newMap state.RedirectMap, target chan<- state.RedirectMap) {
	for _, hook := range repo.RedirectState().Hooks() {
		newMap = hook(newMap)
	}
	target <- newMap
}

func redirectInfoEndpointEnabled() bool {
	return redirectInfoTemplate != nil
}

func addDefaultRedirectMapHooks(mapState *state.RedirectMapState) {
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

	util.Logger().Debug("Adding update hook to strip leading and trailing slashes from redirect paths")
	mapState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
		for key := range originalMap {
			modifyKey(originalMap, key, func(s string) string {
				return strings.Trim(s, "/")
			})
		}
		return originalMap
	})

	if redirectInfoEndpointEnabled() {
		util.Logger().Debug("Adding update hook to remove info-request suffix from redirect paths")
		mapState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
			// Edit map in place
			for key := range originalMap {
				modifyKey(originalMap, key, func(s string) string {
					return strings.TrimRight(s, infoRequestIdentifier)
				})
			}
			return originalMap
		})
	}

	if conf.Config().IgnoreCaseInPath {
		util.Logger().Debug("Adding update hook to make redirect paths lowercase")
		mapState.AddHook(func(originalMap state.RedirectMap) state.RedirectMap {
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

func OnExit(messages ...string) {
	defer util.Logger().Sync()
	if len(messages) == 0 {
		messages = append(messages, "Server stopped")
	}
	for i := range messages {
		util.Logger().Infof(messages[i])
	}
}

func Run(ctx context.Context) error {
	Setup(ctx)
	// Flush log buffer before exiting
	defer util.Logger().Sync()

	shutdownChan := make(chan error)
	server = CreateHttpServer(shutdownChan, ctx)

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		util.Logger().Infof("Shutting down HTTP server")
		err := server.Shutdown(shutdownContext)
		if err != nil {
			return err
		}
	}

	return <-shutdownChan
}
