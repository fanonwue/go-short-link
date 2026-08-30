package repo

import (
	"context"
	"encoding/json/v2"

	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/ds"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/fanonwue/goutils/logging"

	"os"
	"path/filepath"
)

type (
	FallbackFileEntry struct {
		Key    string `json:"key"`
		Target string `json:"target"`
	}
)

var (
	dataSource    ds.RedirectDataSource
	redirectState = state.NewState()
)

func Setup(ctx context.Context) {
	dataSource = ds.CreateSheetsDataSource(ctx)
	RedirectState().ListenForUpdates()
	RedirectState().ListenForUpdateErrors()
}

func DataSource() ds.RedirectDataSource {
	if dataSource == nil {
		logging.Panic("Redirect data source not set up")
	}
	return dataSource
}

func RedirectState() *state.RedirectMapState {
	return &redirectState
}

func UpdateRedirectMappingDefault(force bool) (state.RedirectMap, error) {
	return UpdateRedirectMapping(nil, force)
}
func UpdateRedirectMapping(target chan<- state.RedirectMap, force bool) (state.RedirectMap, error) {
	if !force && !DataSource().NeedsUpdate() && RedirectState().LastError() == nil {
		logging.Debugf("File has not changed since last update, skipping update")
		return nil, nil
	}

	if target == nil {
		target = RedirectState().MappingChannel()
	}

	fetchedMapping, fetchErr := DataSource().FetchRedirectMapping()
	if fetchErr != nil {
		logging.Warnf("Error fetching new redirect mapping: %s", fetchErr)
		if conf.Config().UseFallbackFile() {
			fallbackMap, err := readFallbackMapLog(conf.Config().FallbackFile)
			if err != nil {
				return nil, err
			}
			fetchedMapping = fallbackMap
			logging.Infof("Read from fallback file")
		} else {
			logging.Warnf("Fallback file disabled")
			return nil, fetchErr
		}
	}

	applyHooks(fetchedMapping)

	if conf.Config().UseFallbackFile() {
		_ = writeFallbackFileLog(conf.Config().FallbackFile, fetchedMapping)
	}

	target <- fetchedMapping

	return fetchedMapping, nil
}

func UpdateRedirectMappingChannels(target chan<- state.RedirectMap, lastError chan<- error, force bool) {
	_, fetchErr := UpdateRedirectMapping(target, force)

	if lastError == nil {
		lastError = RedirectState().ErrorChannel()
	}

	lastError <- fetchErr
}

func applyHooks(newMap state.RedirectMap) state.RedirectMap {
	for _, hook := range RedirectState().Hooks() {
		newMap = hook(newMap)
	}
	return newMap
}

func writeFallbackFile(path string, newMapping state.RedirectMap) error {
	if len(path) == 0 {
		logging.Debugf("Fallback file path is empty, skipping write")
		return nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		util.FileCloseErrorHandler(file.Close())
	}()

	jsonEntries := make([]FallbackFileEntry, 0, len(newMapping))

	for key, target := range newMapping {
		jsonEntries = append(jsonEntries, FallbackFileEntry{
			Key:    key,
			Target: target,
		})
	}

	err = json.MarshalWrite(file, jsonEntries)
	return err
}

func writeFallbackFileLog(path string, newMapping state.RedirectMap) error {
	err := writeFallbackFile(path, newMapping)
	if err != nil {
		logging.Warnf("Error writing fallback file: %v", err)
	}
	return err
}

func readFallbackMap(path string) (state.RedirectMap, error) {
	entries, err := readFallbackFile(path)
	if err != nil {
		return nil, err
	}

	mapping := make(state.RedirectMap, len(entries))

	for _, entry := range entries {
		mapping[entry.Key] = entry.Target
	}

	return mapping, nil
}

func readFallbackMapLog(path string) (state.RedirectMap, error) {
	logging.Infof("Reading fallback file")
	fallbackMap, fallbackErr := readFallbackMap(path)
	if fallbackErr != nil {
		logging.Warnf("Could not read fallback file %s: %v", conf.Config().FallbackFile, fallbackErr)
	}
	return fallbackMap, fallbackErr
}

func readFallbackFile(path string) ([]FallbackFileEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		logging.Warnf("Error opening fallback file: %v", err)
		return nil, err
	}
	defer func() {
		util.FileCloseErrorHandler(file.Close())
	}()

	var entries []FallbackFileEntry
	err = json.UnmarshalRead(file, &entries)
	if err != nil {
		logging.Warnf("Error unmarshaling fallback file: %v", err)
		return nil, err
	}
	return entries, nil
}
