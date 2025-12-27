package ds

import (
	"context"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/fanonwue/goutils/logging"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type GoogleAuthConfig struct {
	ProjectId           string
	ServiceAccountMail  string
	ServiceAccountKey   []byte
	ServiceAccountKeyId string
}

type GoogleSheetsConfig struct {
	SpreadsheetId string
	SkipFirstRow  bool
	ApiKey        string
	Auth          *GoogleAuthConfig
}

type GoogleSheetsDataSource struct {
	lastUpdate        time.Time
	lastUpdateMutex   sync.RWMutex
	lastModified      time.Time
	lastModifiedMutex sync.RWMutex
	config            GoogleSheetsConfig
	httpClient        *http.Client
	sheetsService     *sheets.Service
	driveService      *drive.Service
	ctx               context.Context
	ctxCancel         context.CancelFunc
}

const (
	defaultKeyFilePath = "secret/privateKey.pem"
	contextTimeout     = 15 * time.Second
	keyColumn          = 0
	targetColumn       = 1
	isActiveColumn     = 2
)

func createSheetsConfig() GoogleSheetsConfig {
	config := GoogleSheetsConfig{
		SpreadsheetId: os.Getenv(util.PrefixedEnvVar("SPREADSHEET_ID")),
		SkipFirstRow:  true,
	}

	apiKey := os.Getenv(util.PrefixedEnvVar("API_KEY"))

	if len(apiKey) == 0 {
		auth := GoogleAuthConfig{
			ProjectId:           os.Getenv(util.PrefixedEnvVar("PROJECT_ID")),
			ServiceAccountMail:  os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_CLIENT_EMAIL")),
			ServiceAccountKey:   getServiceAccountPrivateKey(),
			ServiceAccountKeyId: os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY_ID")),
		}
		config.Auth = &auth
	} else {
		config.ApiKey = apiKey
	}

	return config
}

func getServiceAccountPrivateKey() []byte {
	// Read private key from env or file, and trim whitespace just to be sure
	var keyData = readPrivateKeyData()
	var pemBlock, _ = pem.Decode(keyData)

	if pemBlock == nil || !strings.Contains(pemBlock.Type, "PRIVATE KEY") {
		logging.Panicf("Key is not a valid PEM-formatted private key")
		return nil
	}

	if len(pemBlock.Bytes) < 100 {
		logging.Warnf("Keyfile smaller than 100 Bytes! This is probably unintended. Length: %d", len(keyData))
	}

	return pemBlock.Bytes
}

func readPrivateKeyData() []byte {
	var keyData []byte
	rawKey := os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY"))
	if len(rawKey) > 0 {
		key := strings.Replace(rawKey, "\\n", "\n", -1)
		keyData = []byte(key)
	} else {
		keyFile := os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY_FILE"))
		if len(keyFile) == 0 {
			// Assume standard keyfile location
			logging.Debugf("Keyfile location not set, assuming default location: %s", defaultKeyFilePath)
			keyFile = defaultKeyFilePath
		}

		keyFile, err := filepath.Abs(keyFile)
		if err != nil {
			logging.Panicf("Could not create absolute path from path: %v", err)
		}

		logging.Infof("Trying to read keyfile from path: %s", keyFile)
		fileContent, err := os.ReadFile(keyFile)
		if err != nil {
			logging.Panicf("Error reading keyfile: %v", err)
		}
		logging.Debugf("Read keyfile, length: %d bytes", len(fileContent))

		keyData = fileContent
	}

	return keyData
}

func CreateSheetsDataSource(ctx context.Context) *GoogleSheetsDataSource {
	serviceCtx, cancelFunc := context.WithCancel(ctx)

	_ds := GoogleSheetsDataSource{
		config:    createSheetsConfig(),
		ctx:       serviceCtx,
		ctxCancel: cancelFunc,
	}

	_ds.postCreate()

	return &_ds
}

func (dsc *GoogleSheetsConfig) UseServiceAccount() bool {
	return dsc.Auth != nil
}

func (ds *GoogleSheetsDataSource) serviceContextWithCancel() (context.Context, context.CancelFunc) {
	return ds.ctx, ds.ctxCancel
}

func (ds *GoogleSheetsDataSource) serviceContext() context.Context {
	ctx, _ := ds.serviceContextWithCancel()
	return ctx
}

func (ds *GoogleSheetsDataSource) serviceContextWithTimeout(duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ds.serviceContext(), duration)
}

func (ds *GoogleSheetsDataSource) apiScopes() []string {
	return []string{
		drive.DriveMetadataReadonlyScope,
		sheets.SpreadsheetsReadonlyScope,
	}
}

func (ds *GoogleSheetsDataSource) postCreate() {
	fileWebLink, err := ds.SpreadsheetWebLink()
	if err == nil {
		logging.Infof("Using document available at: %s", fileWebLink)
	}
}

func (ds *GoogleSheetsDataSource) getClient() *http.Client {
	if ds.httpClient == nil {
		if ds.config.UseServiceAccount() {
			jwtConfig := &jwt.Config{
				Email:      ds.config.Auth.ServiceAccountMail,
				PrivateKey: ds.config.Auth.ServiceAccountKey,
				TokenURL:   google.JWTTokenURL,
				Scopes:     ds.apiScopes(),
			}

			keyId := ds.config.Auth.ServiceAccountKeyId
			if len(keyId) > 0 {
				jwtConfig.PrivateKeyID = keyId
			}

			ds.httpClient = jwtConfig.Client(ds.serviceContext())
		} else {
			ds.httpClient = &http.Client{}
		}

		ds.httpClient.Timeout = contextTimeout * 2
	}

	return ds.httpClient
}

func (ds *GoogleSheetsDataSource) serviceClientOpts() []option.ClientOption {
	var opts []option.ClientOption

	if ds.config.UseServiceAccount() {
		opts = append(opts, option.WithHTTPClient(ds.getClient()))
	} else {
		opts = append(opts,
			option.WithAPIKey(os.Getenv(util.PrefixedEnvVar("API_KEY"))),
			option.WithScopes(ds.apiScopes()...),
		)
	}

	return opts
}

func (ds *GoogleSheetsDataSource) DriveService() *drive.Service {
	if ds.driveService == nil {
		ctx := ds.serviceContext()
		newService, err := drive.NewService(ctx, ds.serviceClientOpts()...)
		if err != nil {
			logging.Panicf("Could not create drive service: %v", err)
		} else {
			ds.driveService = newService
		}
	}
	return ds.driveService
}

func (ds *GoogleSheetsDataSource) SheetsService() *sheets.Service {
	if ds.sheetsService == nil {
		newService, err := sheets.NewService(ds.serviceContext(), ds.serviceClientOpts()...)
		if err != nil {
			logging.Panicf("Could not create sheets service: %v", err)
		} else {
			ds.sheetsService = newService
		}
	}
	return ds.sheetsService
}

func (ds *GoogleSheetsDataSource) SpreadsheetId() string {
	return ds.config.SpreadsheetId
}

func (ds *GoogleSheetsDataSource) Id() string {
	return ds.SpreadsheetId()
}

func (ds *GoogleSheetsDataSource) LastUpdate() time.Time {
	ds.lastUpdateMutex.RLock()
	defer ds.lastUpdateMutex.RUnlock()
	return ds.lastUpdate
}

func (ds *GoogleSheetsDataSource) SpreadsheetWebLink() (string, error) {
	service := ds.DriveService()

	// Use a context with timeout
	ctx, cancel := ds.serviceContextWithTimeout(contextTimeout)
	defer cancel()

	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("webViewLink").
		Context(ctx).
		Do()

	if err != nil {
		logging.Warnf("Could not determine webViewLink for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
		return "", err
	}

	return file.WebViewLink, nil
}

func (ds *GoogleSheetsDataSource) updateLastUpdate(updateTime time.Time) time.Time {
	ds.lastUpdateMutex.Lock()
	defer ds.lastUpdateMutex.Unlock()
	if updateTime.IsZero() {
		updateTime = time.Now()
	}

	ds.lastUpdate = updateTime.UTC()
	return ds.lastUpdate
}

func (ds *GoogleSheetsDataSource) updateLastModified() (time.Time, error) {
	service := ds.DriveService()

	// Use a context with timeout
	ctx, cancel := ds.serviceContextWithTimeout(contextTimeout)
	defer cancel()

	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("modifiedTime").
		Context(ctx).
		Do()
	if err != nil {
		logging.Errorf("Could not determine modifiedTime for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
		return time.Time{}, err
	}

	modifiedTimeRaw := file.ModifiedTime
	modifiedTime, err := time.Parse(time.RFC3339, modifiedTimeRaw)
	if err != nil {
		logging.Errorf("Could not parse RFC3339 timestamp %s: %v", modifiedTimeRaw, err)
		return time.Time{}, err
	}

	modifiedTimeUtc := modifiedTime.UTC()
	ds.lastModifiedMutex.Lock()
	defer ds.lastModifiedMutex.Unlock()
	oldTime := ds.lastModified
	ds.lastModified = modifiedTimeUtc

	if oldTime.UnixMilli() != modifiedTimeUtc.UnixMilli() {
		logging.Debugf("Updated last modified time to %v", modifiedTimeUtc)
	}

	return ds.lastModified, nil
}

func (ds *GoogleSheetsDataSource) LastModified() time.Time {
	ds.lastModifiedMutex.RLock()
	if ds.lastModified.IsZero() {
		// Release read lock to allow upgrade to a write lock
		ds.lastModifiedMutex.RUnlock()
		modified, _ := ds.updateLastModified()
		return modified
	}

	defer ds.lastModifiedMutex.RUnlock()
	return ds.lastModified
}

func (ds *GoogleSheetsDataSource) NeedsUpdate() bool {
	if ds.lastUpdate.IsZero() {
		return true
	}

	modifiedTime, err := ds.updateLastModified()
	if err != nil {
		return true
	}

	return modifiedTime.After(ds.lastUpdate)
}

func (ds *GoogleSheetsDataSource) fetchRedirectMappingInternal() (state.RedirectMap, time.Time, error) {
	service := ds.SheetsService()

	sheetsRange := "A2:C"
	if !ds.config.SkipFirstRow {
		sheetsRange = "A:C"
	}

	mapping := state.RedirectMap{}
	updateTime := time.Now().UTC()

	// Use a context with timeout
	ctx, cancel := ds.serviceContextWithTimeout(contextTimeout)
	defer cancel()

	result, err := service.Spreadsheets.Values.Get(ds.config.SpreadsheetId, sheetsRange).
		Context(ctx).
		ValueRenderOption("UNFORMATTED_VALUE").
		Do()

	if err != nil {
		logging.Errorf("Unable to retrieve data from sheet: %v", err)
		return nil, time.Time{}, err
	}

	if len(result.Values) == 0 {
		return mapping, time.Time{}, nil
	}

	for _, row := range result.Values {
		if len(row) < 2 {
			continue
		}

		if len(row) > isActiveColumn {
			isActive, ok := row[isActiveColumn].(bool)

			if !ok {
				rawIsActive, ok := row[isActiveColumn].(string)
				if !ok {
					continue
				}

				isActive, err = strconv.ParseBool(rawIsActive)
			}

			if !isActive || err != nil {
				continue
			}
		}

		key, ok := row[keyColumn].(string)
		if !ok {
			// Check if the key is a number instead
			intKey, ok := row[keyColumn].(int)
			if ok {
				key = strconv.Itoa(intKey)
			} else {
				continue
			}
		}

		value, ok := row[targetColumn].(string)
		if !ok {
			continue
		}

		if len(value) == 0 || len(key) == 0 {
			continue
		}

		mapping[key] = value
	}

	return mapping, updateTime, nil
}

func (ds *GoogleSheetsDataSource) FetchRedirectMapping() (state.RedirectMap, error) {
	mapping, updateTime, err := ds.fetchRedirectMappingInternal()

	if err == nil {
		ds.updateLastUpdate(updateTime)
	}

	return mapping, err
}
