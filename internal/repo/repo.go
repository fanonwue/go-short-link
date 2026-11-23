package repo

import (
	"context"

	"github.com/fanonwue/go-short-link/internal/conf"
	"github.com/fanonwue/go-short-link/internal/ds"
	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/goutils/logging"
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
