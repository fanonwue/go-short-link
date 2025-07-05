package conf

import (
	"crypto/sha256"
	"fmt"
	"github.com/fanonwue/go-short-link/internal/util"
	"os"
	"strconv"
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
		ApiEnabled            bool
	}
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
)

func (ac *AppConfig) UseFallbackFile() bool {
	return len(ac.FallbackFile) > 0
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
		util.Logger().Warnf(
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
		Favicon:               os.Getenv(util.PrefixedEnvVar("FAVICON")),
		FallbackFile:          os.Getenv(util.PrefixedEnvVar("FALLBACK_FILE")),
		ApiEnabled:            boolConfig(util.PrefixedEnvVar("ENABLE_API"), true),
	}

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

	userHash := sha256.Sum256([]byte(user))
	passHash := sha256.Sum256([]byte(pass))

	return &AdminCredentials{
		UserHash: userHash[:],
		PassHash: passHash[:],
	}
}
