package main

import (
	"context"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"net/http"
	"os"
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

var config *GoogleSheetsConfig

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

func CreateSheetsConfig() {
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
}

func getServiceAccountPrivateKey() []byte {
	rawKey := os.Getenv("SERVICE_ACCOUNT_PRIVATE_KEY")
	key := strings.Replace(rawKey, "\\n", "\n", -1)
	return []byte(key)
}

func GetConfig() *GoogleSheetsConfig {
	if config == nil {
		CreateSheetsConfig()
	}
	return config
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

func GetRedirectMapping() map[string]string {
	conf := GetConfig()
	service := config.SheetsService()

	sheetsRange := "A2:B"
	if !conf.SkipFirstRow {
		sheetsRange = "A:B"
	}

	mapping := map[string]string{}
	updateTime := time.Now()

	result, err := service.Spreadsheets.Values.Get(conf.SpreadsheetId, sheetsRange).Do()
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

		if appConfig.IgnoreCaseInPath {
			key = strings.ToLower(key)
		}

		mapping[key] = value
	}

	return mapping
}
