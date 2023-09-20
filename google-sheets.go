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
	"strings"
	"time"
)

type GoogleAuthConfig struct {
	ProjectId          string
	ServiceAccountMail string
	ServiceAccountKey  []byte
	KeyId              string
}

type GoogleSheetsConfig struct {
	SpreadsheetId string
	SkipFirstRow  bool
	Auth          GoogleAuthConfig
	SheetsService *sheets.Service
	DriveService  *drive.Service
	LastUpdate    time.Time
}

var config *GoogleSheetsConfig

func CreateSheetsConfig() {
	config = &GoogleSheetsConfig{
		SpreadsheetId: os.Getenv("SPREADSHEET_ID"),
		SkipFirstRow:  true,
		LastUpdate:    time.Now(),
		Auth: GoogleAuthConfig{
			ProjectId:          os.Getenv("PROJECT_ID"),
			ServiceAccountMail: os.Getenv("SERVICE_ACCOUNT_CLIENT_EMAIL"),
			ServiceAccountKey:  GetServiceAccountPrivateKey(),
		},
	}
}

func GetConfig() *GoogleSheetsConfig {
	if config == nil {
		CreateSheetsConfig()
	}
	return config
}

func GetClient() *http.Client {
	conf := GetConfig().Auth
	jwtConfig := &jwt.Config{
		Email:      conf.ServiceAccountMail,
		PrivateKey: conf.ServiceAccountKey,
		TokenURL:   google.JWTTokenURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/drive.metadata.readonly",
			"https://www.googleapis.com/auth/spreadsheets.readonly",
		},
	}

	return jwtConfig.Client(context.Background())
}

func DriveService() *drive.Service {
	if config.DriveService == nil {
		newService, err := drive.NewService(context.Background(), option.WithHTTPClient(GetClient()))
		if err != nil {
			logger.Panicf("Could not create drive service: %v", err)
		} else {
			config.DriveService = newService
		}
	}
	return config.DriveService
}

func SheetsService() *sheets.Service {
	if config.SheetsService == nil {
		newService, err := sheets.NewService(context.Background(), option.WithHTTPClient(GetClient()))
		if err != nil {
			logger.Panicf("Could not create sheets service: %v", err)
		} else {
			config.SheetsService = newService
		}
	}
	return config.SheetsService
}

func SpreadsheetWebLink() (string, error) {
	service := DriveService()
	file, err := service.Files.Get(config.SpreadsheetId).Fields("webViewLink").Do()
	if err != nil {
		logger.Warnf("Could not determine webViewLink for Spreadsheet '%s': %v", config.SpreadsheetId, err)
		return "", err
	}
	return file.WebViewLink, nil
}

func NeedsUpdate() bool {
	service := DriveService()
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
	return modifiedTime.After(config.LastUpdate)
}

func GetRedirectMapping() map[string]string {
	conf := GetConfig()
	service := SheetsService()

	sheetsRange := "A2:B"
	if !conf.SkipFirstRow {
		sheetsRange = "A:B"
	}

	mapping := map[string]string{}

	result, err := service.Spreadsheets.Values.Get(conf.SpreadsheetId, sheetsRange).Do()
	if err != nil {
		logger.Errorf("Unable to retrieve data from sheet: %v", err)
		return mapping
	}

	config.LastUpdate = time.Now()

	if len(result.Values) == 0 {
		return mapping
	}

	for _, row := range result.Values {
		if len(row) == 0 {
			continue
		}

		key := row[0]
		value := row[1]

		if key == nil || value == nil {
			continue
		}

		if appConfig.IgnoreCaseInPath {
			key = strings.ToLower(key.(string))
		}

		mapping[key.(string)] = value.(string)
	}

	return mapping
}

func GetServiceAccountPrivateKey() []byte {
	rawKey := os.Getenv("SERVICE_ACCOUNT_PRIVATE_KEY")
	key := strings.Replace(rawKey, "\\n", "\n", -1)
	return []byte(key)
}
