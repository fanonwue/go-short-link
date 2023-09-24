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
	LastUpdate    *time.Time
	httpClient    *http.Client
	sheetsService *sheets.Service
	driveService  *drive.Service
}

const (
	// If enabled, the read key will be validated to make sure it is a PEM-formatted key
	validateKeyContent = false
	defaultKeyFilePath = "secret/privateKey.pem"
)

var (
	config *GoogleSheetsConfig
)

func (conf *GoogleSheetsConfig) getClient() *http.Client {
	if conf.httpClient == nil {
		jwtConfig := &jwt.Config{
			Email:      conf.Auth.ServiceAccountMail,
			PrivateKey: conf.Auth.ServiceAccountKey,
			TokenURL:   google.JWTTokenURL,
			Scopes: []string{
				"https://www.googleapis.com/auth/drive.metadata.readonly",
				"https://www.googleapis.com/auth/spreadsheets.readonly",
			},
		}

		keyId := conf.Auth.ServiceAccountKeyId
		if len(keyId) > 0 {
			jwtConfig.PrivateKeyID = keyId
		}

		conf.httpClient = jwtConfig.Client(context.Background())
	}

	return conf.httpClient
}

func (conf *GoogleSheetsConfig) DriveService() *drive.Service {
	if conf.driveService == nil {
		newService, err := drive.NewService(context.Background(), option.WithHTTPClient(conf.getClient()))
		if err != nil {
			logger.Panicf("Could not create drive service: %v", err)
		} else {
			conf.driveService = newService
		}
	}
	return conf.driveService
}

func (conf *GoogleSheetsConfig) SheetsService() *sheets.Service {
	if conf.sheetsService == nil {
		newService, err := sheets.NewService(context.Background(), option.WithHTTPClient(conf.getClient()))
		if err != nil {
			logger.Panicf("Could not create sheets service: %v", err)
		} else {
			conf.sheetsService = newService
		}
	}
	return conf.sheetsService
}

func CreateSheetsConfig() *GoogleSheetsConfig {
	config = &GoogleSheetsConfig{
		SpreadsheetId: os.Getenv("SPREADSHEET_ID"),
		SkipFirstRow:  true,
		Auth: GoogleAuthConfig{
			ProjectId:           os.Getenv("PROJECT_ID"),
			ServiceAccountMail:  os.Getenv("SERVICE_ACCOUNT_CLIENT_EMAIL"),
			ServiceAccountKey:   getServiceAccountPrivateKey(),
			ServiceAccountKeyId: os.Getenv("SERVICE_ACCOUNT_PRIVATE_KEY_ID"),
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
	rawKey := os.Getenv("SERVICE_ACCOUNT_PRIVATE_KEY")
	if len(rawKey) > 0 {
		key := strings.Replace(rawKey, "\\n", "\n", -1)
		keyData = []byte(key)
	} else {
		keyFile := os.Getenv("SERVICE_ACCOUNT_PRIVATE_KEY_FILE")
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

func SpreadsheetWebLink() (string, error) {
	service := config.DriveService()
	file, err := service.Files.Get(config.SpreadsheetId).Fields("webViewLink").Do()
	if err != nil {
		logger.Warnf("Could not determine webViewLink for Spreadsheet '%s': %v", config.SpreadsheetId, err)
		return "", err
	}
	return file.WebViewLink, nil
}

func NeedsUpdate() bool {
	if config.LastUpdate == nil {
		return true
	}

	service := config.DriveService()
	file, err := service.Files.Get(config.SpreadsheetId).Fields("modifiedTime").Do()
	if err != nil {
		logger.Errorf("Could not determine modifiedTime for Spreadsheet '%s': %v", config.SpreadsheetId, err)
		return true
	}

	modifiedTimeRaw := file.ModifiedTime
	modifiedTime, err := time.Parse(time.RFC3339, modifiedTimeRaw)
	if err != nil {
		logger.Errorf("Could not parse RFC3339 timestamp %s: %v", modifiedTimeRaw, err)
		return true
	}
	return modifiedTime.After(*config.LastUpdate)
}

func FetchRedirectMapping() map[string]string {
	service := config.SheetsService()

	sheetsRange := "A2:B"
	if !config.SkipFirstRow {
		sheetsRange = "A:B"
	}

	mapping := map[string]string{}
	updateTime := time.Now()

	result, err := service.Spreadsheets.Values.Get(config.SpreadsheetId, sheetsRange).Do()
	if err != nil {
		logger.Errorf("Unable to retrieve data from sheet: %v", err)
		return mapping
	}

	config.LastUpdate = &updateTime

	if len(result.Values) == 0 {
		return mapping
	}

	for _, row := range result.Values {
		if len(row) < 2 {
			continue
		}

		key, ok := row[0].(string)
		if !ok {
			// Check if the key is a number instead
			intKey, ok := row[0].(int)
			if ok {
				key = strconv.Itoa(intKey)
			} else {
				continue
			}
		}

		value, ok := row[1].(string)
		if !ok {
			continue
		}

		if len(value) == 0 || len(key) == 0 {
			continue
		}

		mapping[key] = value
	}

	return mapping
}
