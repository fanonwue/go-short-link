package ds

import (
	"context"
	"encoding/pem"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
}

const (
	defaultKeyFilePath = "secret/privateKey.pem"
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
		slog.Error("Key is not a valid PEM-formatted private key")
		panic("invalid private key data")
		return nil
	}

	if len(pemBlock.Bytes) < 100 {
		slog.Warn("Keyfile smaller than 100 Bytes! This is probably unintended", slog.Int("length", len(pemBlock.Bytes)))
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
			slog.Debug("Keyfile location not set, assuming default location", "default", defaultKeyFilePath)
			keyFile = defaultKeyFilePath
		}

		keyFile, err := filepath.Abs(keyFile)
		if err != nil {
			slog.Error("Could not create absolute path from path", "err", err)
			panic(err)
		}

		slog.Info("Trying to read keyfile", "path", keyFile)
		fileContent, err := os.ReadFile(keyFile)
		if err != nil {
			slog.Error("Error reading keyfile", "err", err)
			panic(err)
		}
		slog.Debug("Read keyfile", slog.Int("lengthBytes", len(fileContent)))

		keyData = fileContent
	}

	return keyData
}

func CreateSheetsDataSource() *GoogleSheetsDataSource {
	_ds := GoogleSheetsDataSource{
		config: createSheetsConfig(),
	}

	_ds.postCreate()

	return &_ds
}

func (dsc *GoogleSheetsConfig) UseServiceAccount() bool {
	return dsc.Auth != nil
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
		slog.Info("Using document", "url", fileWebLink)
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

			ds.httpClient = jwtConfig.Client(context.Background())
		} else {
			ds.httpClient = &http.Client{}
		}
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
		newService, err := drive.NewService(context.Background(), ds.serviceClientOpts()...)
		if err != nil {
			slog.Error("Could not create drive service")
			panic(err)
		} else {
			ds.driveService = newService
		}
	}
	return ds.driveService
}

func (ds *GoogleSheetsDataSource) SheetsService() *sheets.Service {
	if ds.sheetsService == nil {
		newService, err := sheets.NewService(context.Background(), ds.serviceClientOpts()...)
		if err != nil {
			slog.Error("Could not create sheets service", "err", err)
			panic(err)
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
	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("webViewLink").Do()
	if err != nil {
		slog.Warn("Could not determine webViewLink for spreadsheet", "spreadsheet", ds.config.SpreadsheetId, "err", err)
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
	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("modifiedTime").Do()
	if err != nil {
		slog.Error("Could not determine modifiedTime for spreadsheet", "spreadsheet", ds.config.SpreadsheetId, "err", err)
		return time.Time{}, err
	}

	modifiedTimeRaw := file.ModifiedTime
	modifiedTime, err := time.Parse(time.RFC3339, modifiedTimeRaw)
	if err != nil {
		slog.Error("Could not parse RFC3339 timestamp", "timestamp", modifiedTimeRaw, "err", err)
		return time.Time{}, err
	}

	modifiedTimeUtc := modifiedTime.UTC()
	ds.lastModifiedMutex.Lock()
	defer ds.lastModifiedMutex.Unlock()
	oldTime := ds.lastModified
	ds.lastModified = modifiedTimeUtc

	if oldTime.UnixMilli() != modifiedTimeUtc.UnixMilli() {
		slog.Debug("Update last modified time", "time", modifiedTimeUtc)
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

	result, err := service.Spreadsheets.Values.Get(ds.config.SpreadsheetId, sheetsRange).
		ValueRenderOption("UNFORMATTED_VALUE").
		Do()

	if err != nil {
		slog.Error("Unable to retrieve data from sheet", "err", err)
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
