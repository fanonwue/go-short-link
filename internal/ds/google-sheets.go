package ds

import (
	"bytes"
	"context"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
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
	// If enabled, the read key will be validated to make sure it is a PEM-formatted key
	validateKeyContent = false
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
	var keyData = readPrivateKeyData(true)

	if len(keyData) < 100 {
		util.Logger().Warnf("Keyfile smaller than 100 Bytes! This is probably unintended. Length: %d", len(keyData))
	}

	if validateKeyContent {
		util.Logger().Info("Key validation enabled, performing checks")
		keyString := string(keyData)

		keyPrefix := "-----BEGIN PRIVATE KEY-----"
		if !strings.HasPrefix(keyString, keyPrefix) {
			util.Logger().Panicf("Key does not start with expected prefix: %s", keyPrefix)
		}

		keySuffix := "-----END PRIVATE KEY-----"
		if !strings.HasSuffix(keyString, keySuffix) {
			util.Logger().Panicf("Key does not end with expected suffix: %s", keySuffix)
		}
	} else {
		util.Logger().Info("Key validation disabled, skipping checks")
	}

	return keyData
}

func readPrivateKeyData(trimWhitespace bool) []byte {
	var keyData []byte
	rawKey := os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY"))
	if len(rawKey) > 0 {
		key := strings.Replace(rawKey, "\\n", "\n", -1)
		keyData = []byte(key)
	} else {
		keyFile := os.Getenv(util.PrefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY_FILE"))
		if len(keyFile) == 0 {
			// Assume standard keyfile location
			util.Logger().Debugf("Keyfile location not set, assuming default location: %s", defaultKeyFilePath)
			keyFile = defaultKeyFilePath
		}

		keyFile, err := filepath.Abs(keyFile)
		if err != nil {
			util.Logger().Panicf("Could not create absolute path from path: %v", err)
		}

		util.Logger().Infof("Trying to read keyfile from path: %s", keyFile)
		fileContent, err := os.ReadFile(keyFile)
		if err != nil {
			util.Logger().Panicf("Error reading keyfile: %v", err)
		}
		util.Logger().Debugf("Read keyfile, length: %d bytes", len(fileContent))

		keyData = fileContent
	}

	if trimWhitespace {
		return bytes.TrimSpace(keyData)
	} else {
		return keyData
	}
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
		util.Logger().Infof("Using document available at: %s", fileWebLink)
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
			util.Logger().Panicf("Could not create drive service: %v", err)
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
			util.Logger().Panicf("Could not create sheets service: %v", err)
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
		util.Logger().Warnf("Could not determine webViewLink for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
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
	ds.lastModifiedMutex.Lock()
	defer ds.lastModifiedMutex.Unlock()
	service := ds.DriveService()
	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("modifiedTime").Do()
	if err != nil {
		util.Logger().Errorf("Could not determine modifiedTime for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
		return time.Time{}, err
	}

	modifiedTimeRaw := file.ModifiedTime
	modifiedTime, err := time.Parse(time.RFC3339, modifiedTimeRaw)
	if err != nil {
		util.Logger().Errorf("Could not parse RFC3339 timestamp %s: %v", modifiedTimeRaw, err)
		return time.Time{}, err
	}

	modifiedTimeUtc := modifiedTime.UTC()
	oldTime := ds.lastModified
	ds.lastModified = modifiedTimeUtc

	if oldTime.UnixMilli() != modifiedTimeUtc.UnixMilli() {
		util.Logger().Debugf("Updated last modified time to %v", modifiedTimeUtc)
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
		util.Logger().Errorf("Unable to retrieve data from sheet: %v", err)
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
