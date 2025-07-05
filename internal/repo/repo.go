package repo

import (
	"encoding/json"
	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/ds"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
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

func Setup() {
	dataSource = ds.CreateSheetsDataSource()
	RedirectState().ListenForUpdates()
	RedirectState().ListenForUpdateErrors()
}

func DataSource() ds.RedirectDataSource {
	if dataSource == nil {
		util.Logger().Panic("Redirect data source not set up")
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
		util.Logger().Debugf("File has not changed since last update, skipping update")
		return nil, nil
	}

	if target == nil {
		target = RedirectState().MappingChannel()
	}

	fetchedMapping, fetchErr := DataSource().FetchRedirectMapping()
	if fetchErr != nil {
		util.Logger().Warnf("Error fetching new redirect mapping: %s", fetchErr)
		if conf.Config().UseFallbackFile() {
			fallbackMap, err := readFallbackFileLog(conf.Config().FallbackFile)
			if err != nil {
				return nil, err
			}
			fetchedMapping = fallbackMap
			util.Logger().Infof("Read from fallback file")
		} else {
			util.Logger().Warnf("Fallback file disabled")
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
		util.Logger().Debugf("Fallback file path is empty, skipping write")
		return nil
	}

	jsonEntries := make([]FallbackFileEntry, len(newMapping))

	i := 0
	for key, target := range newMapping {
		jsonEntries[i] = FallbackFileEntry{
			Key:    key,
			Target: target,
		}
		i++
	}

	jsonBytes, err := json.Marshal(&jsonEntries)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	err = os.WriteFile(path, jsonBytes, 0644)
	if err != nil {
		return err
	}

	return nil
}

func writeFallbackFileLog(path string, newMapping state.RedirectMap) error {
	err := writeFallbackFile(path, newMapping)
	if err != nil {
		util.Logger().Warnf("Error writing fallback file: %v", err)
	}
	return err
}

func readFallbackFile(path string) (state.RedirectMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		util.Logger().Warnf("Error reading fallback file: %v", err)
		return nil, err
	}

	var entries []FallbackFileEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		util.Logger().Warnf("Error unmarshaling fallback file: %v", err)
		return nil, err
	}

	mapping := make(state.RedirectMap, len(entries))

	for _, entry := range entries {
		mapping[entry.Key] = entry.Target
	}

	return mapping, nil
}

func readFallbackFileLog(path string) (state.RedirectMap, error) {
	util.Logger().Infof("Reading fallback file")
	fallbackMap, fallbackErr := readFallbackFile(path)
	if fallbackErr != nil {
		util.Logger().Warnf("Could not read fallback file %s: %v", conf.Config().FallbackFile, fallbackErr)
	}
	return fallbackMap, fallbackErr
}
