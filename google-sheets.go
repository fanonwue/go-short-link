package main

import (
	"bytes"
	"context"
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
	Auth          GoogleAuthConfig
}

type GoogleSheetsDataSource struct {
	lastUpdate    *time.Time
	config        GoogleSheetsConfig
	httpClient    *http.Client
	sheetsService *sheets.Service
	driveService  *drive.Service
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
		SpreadsheetId: os.Getenv(prefixedEnvVar("SPREADSHEET_ID")),
		SkipFirstRow:  true,
		Auth: GoogleAuthConfig{
			ProjectId:           os.Getenv(prefixedEnvVar("PROJECT_ID")),
			ServiceAccountMail:  os.Getenv(prefixedEnvVar("SERVICE_ACCOUNT_CLIENT_EMAIL")),
			ServiceAccountKey:   getServiceAccountPrivateKey(),
			ServiceAccountKeyId: os.Getenv(prefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY_ID")),
		},
	}
	return config
}

func getServiceAccountPrivateKey() []byte {
	// Read private key from env or file, and trim whitespace just to be sure
	var keyData = readPrivateKeyData(true)

	if len(keyData) < 100 {
		logger.Warnf("Keyfile smaller than 100 Bytes! This is probably unintended. Length: %d", len(keyData))
	}

	if validateKeyContent {
		logger.Info("Key validation enabled, performing checks")
		keyString := string(keyData)

		keyPrefix := "-----BEGIN PRIVATE KEY-----"
		if !strings.HasPrefix(keyString, keyPrefix) {
			logger.Panicf("Key does not start with expected prefix: %s", keyPrefix)
		}

		keySuffix := "-----END PRIVATE KEY-----"
		if !strings.HasSuffix(keyString, keySuffix) {
			logger.Panicf("Key does not end with expected suffix: %s", keySuffix)
		}
	} else {
		logger.Info("Key validation disabled, skipping checks")
	}

	return keyData
}

func readPrivateKeyData(trimWhitespace bool) []byte {
	var keyData []byte
	rawKey := os.Getenv(prefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY"))
	if len(rawKey) > 0 {
		key := strings.Replace(rawKey, "\\n", "\n", -1)
		keyData = []byte(key)
	} else {
		keyFile := os.Getenv(prefixedEnvVar("SERVICE_ACCOUNT_PRIVATE_KEY_FILE"))
		if len(keyFile) == 0 {
			// Assume standard keyfile location
			logger.Debugf("Keyfile location not set, assuming default location: %s", defaultKeyFilePath)
			keyFile = defaultKeyFilePath
		}

		keyFile, err := filepath.Abs(keyFile)
		if err != nil {
			logger.Panicf("Could not create absolute path from path: %v", err)
		}

		logger.Infof("Trying to read keyfile from path: %s", keyFile)
		fileContent, err := os.ReadFile(keyFile)
		if err != nil {
			logger.Panicf("Error reading keyfile: %v", err)
		}
		logger.Debugf("Read keyfile, length: %d bytes", len(fileContent))

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

func (ds *GoogleSheetsDataSource) postCreate() {
	fileWebLink, err := ds.SpreadsheetWebLink()
	if err == nil {
		logger.Infof("Using document available at: %s", fileWebLink)
	}
}

func (ds *GoogleSheetsDataSource) getClient() *http.Client {
	if ds.httpClient == nil {
		jwtConfig := &jwt.Config{
			Email:      ds.config.Auth.ServiceAccountMail,
			PrivateKey: ds.config.Auth.ServiceAccountKey,
			TokenURL:   google.JWTTokenURL,
			Scopes: []string{
				"https://www.googleapis.com/auth/drive.metadata.readonly",
				"https://www.googleapis.com/auth/spreadsheets.readonly",
			},
		}

		keyId := ds.config.Auth.ServiceAccountKeyId
		if len(keyId) > 0 {
			jwtConfig.PrivateKeyID = keyId
		}

		ds.httpClient = jwtConfig.Client(context.Background())
	}

	return ds.httpClient
}

func (ds *GoogleSheetsDataSource) DriveService() *drive.Service {
	if ds.driveService == nil {
		newService, err := drive.NewService(context.Background(), option.WithHTTPClient(ds.getClient()))
		if err != nil {
			logger.Panicf("Could not create drive service: %v", err)
		} else {
			ds.driveService = newService
		}
	}
	return ds.driveService
}

func (ds *GoogleSheetsDataSource) SheetsService() *sheets.Service {
	if ds.sheetsService == nil {
		newService, err := sheets.NewService(context.Background(), option.WithHTTPClient(ds.getClient()))
		if err != nil {
			logger.Panicf("Could not create sheets service: %v", err)
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

func (ds *GoogleSheetsDataSource) LastUpdate() *time.Time {
	return ds.lastUpdate
}

func (ds *GoogleSheetsDataSource) SpreadsheetWebLink() (string, error) {
	service := ds.DriveService()
	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("webViewLink").Do()
	if err != nil {
		logger.Warnf("Could not determine webViewLink for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
		return "", err
	}
	return file.WebViewLink, nil
}

func (ds *GoogleSheetsDataSource) NeedsUpdate() bool {
	if ds.lastUpdate == nil {
		return true
	}

	service := ds.DriveService()
	file, err := service.Files.Get(ds.config.SpreadsheetId).Fields("modifiedTime").Do()
	if err != nil {
		logger.Errorf("Could not determine modifiedTime for Spreadsheet '%s': %v", ds.config.SpreadsheetId, err)
		return true
	}

	modifiedTimeRaw := file.ModifiedTime
	modifiedTime, err := time.Parse(time.RFC3339, modifiedTimeRaw)
	if err != nil {
		logger.Errorf("Could not parse RFC3339 timestamp %s: %v", modifiedTimeRaw, err)
		return true
	}
	return modifiedTime.After(*ds.lastUpdate)
}

func (ds *GoogleSheetsDataSource) FetchRedirectMapping() (RedirectMap, error) {
	service := ds.SheetsService()

	sheetsRange := "A2:C"
	if !ds.config.SkipFirstRow {
		sheetsRange = "A:C"
	}

	mapping := RedirectMap{}
	updateTime := time.Now().UTC()

	result, err := service.Spreadsheets.Values.Get(ds.config.SpreadsheetId, sheetsRange).
		ValueRenderOption("UNFORMATTED_VALUE").
		Do()

	if err != nil {
		logger.Errorf("Unable to retrieve data from sheet: %v", err)
		return nil, err
	}

	ds.lastUpdate = &updateTime

	if len(result.Values) == 0 {
		return mapping, nil
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

	return mapping, nil
}
