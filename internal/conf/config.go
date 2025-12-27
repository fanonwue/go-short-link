package conf

import (
	"fmt"

	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/fanonwue/goutils/logging"

	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type (
	FaviconType string

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
		Favicons              map[FaviconType]string
		AllowRootRedirect     bool
		FallbackFile          string
		ShowRepositoryLink    bool
		ApiEnabled            bool
	}

	FaviconEntry struct {
		Type  FaviconType
		Value string
	}
)

const (
	FaviconTypePng FaviconType = "png"
	FaviconTypeIco FaviconType = "ico"
)

const (
	LogResponseTimes           = false
	ServerIdentifierHeader     = "go-short-link"
	CacheControlHeaderTemplate = "public, max-age=%d"
	EtagLength                 = 8
	DefaultBufferSize          = 4096
	defaultUpdatePeriod        = 300
	minimumUpdatePeriod        = 15
)

var (
	currentConfig *AppConfig
	isProd        bool
)

func (ac *AppConfig) UseFallbackFile() bool {
	return len(ac.FallbackFile) > 0
}

func (ac *AppConfig) HasFavicons() bool {
	return len(ac.Favicons) > 0
}

func (ac *AppConfig) FaviconByType(t FaviconType) (string, bool) {
	val, ok := ac.Favicons[t]
	return val, ok
}

func (ac *AppConfig) FaviconEntries() []FaviconEntry {
	entries := make([]FaviconEntry, 0, len(ac.Favicons))
	for t, v := range ac.Favicons {
		entries = append(entries, FaviconEntry{t, v})
	}
	slices.SortFunc(entries, func(a, b FaviconEntry) int {
		return b.Type.Priority() - a.Type.Priority()
	})
	return entries
}

func (t FaviconType) String() string {
	return string(t)
}

func (t FaviconType) Mime() string {
	switch t {
	case FaviconTypeIco:
		return "image/x-icon"
	case FaviconTypePng:
		return "image/png"
	}

	logging.Panicf("Unknown favicon type: %s", t)
	return ""
}

func (t FaviconType) Priority() int {
	switch t {
	case FaviconTypePng:
		return 100
	default:
		return 0
	}
}

func init() {
	prodEnvValues := []string{"prod", "production"}
	envValue := strings.ToLower(os.Getenv(util.PrefixedEnvVar("ENV")))
	isProd = slices.Contains(prodEnvValues, envValue)
}

func IsProd() bool {
	return isProd
}

func Config() *AppConfig {
	if currentConfig == nil {
		CreateAppConfig()
	}
	return currentConfig
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
		logging.Warnf(
			"UPDATE_PERIOD set to less than %d seconds (minimum), setting it to %d seconds (default)",
			minimumUpdatePeriod, defaultUpdatePeriod)
		updatePeriod = defaultUpdatePeriod
	}

	httpCacheMaxAge, err := strconv.ParseUint(os.Getenv(util.PrefixedEnvVar("HTTP_CACHE_MAX_AGE")), 0, 32)
	if err != nil {
		httpCacheMaxAge = updatePeriod * 2
	}

	currentConfig = &AppConfig{
		IgnoreCaseInPath:      boolConfig(util.PrefixedEnvVar("IGNORE_CASE_IN_PATH"), true),
		ShowServerHeader:      boolConfig(util.PrefixedEnvVar("SHOW_SERVER_HEADER"), true),
		Port:                  uint16(port),
		UpdatePeriod:          time.Duration(updatePeriod) * time.Second,
		HttpCacheMaxAge:       uint32(httpCacheMaxAge),
		CacheControlHeader:    fmt.Sprintf(CacheControlHeaderTemplate, httpCacheMaxAge),
		StatusEndpointEnabled: !boolConfig(util.PrefixedEnvVar("DISABLE_STATUS"), false),
		AdminCredentials:      createAdminCredentials(),
		UseETag:               boolConfig(util.PrefixedEnvVar("ENABLE_ETAG"), true),
		UseRedirectBody:       boolConfig(util.PrefixedEnvVar("ENABLE_REDIRECT_BODY"), true),
		AllowRootRedirect:     boolConfig(util.PrefixedEnvVar("ALLOW_ROOT_REDIRECT"), true),
		ShowRepositoryLink:    boolConfig(util.PrefixedEnvVar("SHOW_REPOSITORY_LINK"), false),
		Favicons:              make(map[FaviconType]string),
		FallbackFile:          os.Getenv(util.PrefixedEnvVar("FALLBACK_FILE")),
		ApiEnabled:            boolConfig(util.PrefixedEnvVar("ENABLE_API"), false),
	}

	rawFavicons := os.Getenv(util.PrefixedEnvVar("FAVICON"))
	favicons := strings.Split(rawFavicons, ",")
	for _, favicon := range favicons {
		favicon = strings.TrimSpace(favicon)
		if favicon == "" {
			continue
		}
		faviconType := FaviconTypeIco
		if strings.HasSuffix(favicon, ".png") {
			faviconType = FaviconTypePng
		}
		currentConfig.Favicons[faviconType] = favicon
	}

	// Only allow API in dev environment for now
	currentConfig.ApiEnabled = currentConfig.ApiEnabled && !isProd

	return currentConfig
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

	userHash, err := util.HashPassword([]byte(user))
	if err != nil {
		logging.Panicf("Failed to hash admin credentials USER: %v", err)
	}

	passHash, err := util.HashPassword([]byte(pass))
	if err != nil {
		logging.Panicf("Failed to hash admin credentials PASS: %v", err)
	}

	return &AdminCredentials{
		UserHash: userHash,
		PassHash: passHash,
	}
}
