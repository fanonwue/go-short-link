package ds

import (
	"encoding/csv"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
)

// CsvDataSource is a very simple implementation of the RedirectDataSource interface.
// It's not being used in the actual application, but it's here for testing and demonstration purposes.
type CsvDataSource struct {
	filePath              string
	lastUpdate            time.Time
	checkModificationTime bool
}

func (ds *CsvDataSource) LastUpdate() time.Time {
	return ds.lastUpdate
}

func (ds *CsvDataSource) LastModified() time.Time {
	fileInfo, err := withFile(ds, func(f fs.File) (fs.FileInfo, error) {
		return f.Stat()
	})
	if err != nil {
		util.Logger().Errorf("Could not get file info: %v", err)
		return time.Time{}
	}
	return fileInfo.ModTime().UTC()
}

func (ds *CsvDataSource) NeedsUpdate() bool {
	// If checkModificationTime has been disabled, always signal that an update is needed
	if !ds.checkModificationTime {
		return true
	}

	lastModifiedTime := ds.LastModified()
	// If the returned time is zero, an error occurred while getting the file info
	// In this case, we assume that an update is needed
	if lastModifiedTime.IsZero() {
		return true
	}
	// An update is needed if the file has been modified since the last update
	return lastModifiedTime.After(ds.lastUpdate)
}

func (ds *CsvDataSource) FetchRedirectMapping() (state.RedirectMap, error) {
	return withFile(ds, func(f fs.File) (state.RedirectMap, error) {
		return fetchRedirectMappingInternal(ds, f)
	})
}

func (ds *CsvDataSource) Id() string {
	return "CsvDataSource#" + ds.filePath
}

func fetchRedirectMappingInternal(ds *CsvDataSource, f fs.File) (state.RedirectMap, error) {
	redirectMap := state.RedirectMap{}
	updateTime := time.Now().UTC()

	reader := csv.NewReader(f)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// Invalid record
		if len(record) < 2 {
			continue
		}

		name := record[0]
		target := record[1]

		// Invalid record
		if len(name) == 0 || len(target) == 0 {
			continue
		}

		redirectMap[name] = target
	}
	ds.lastUpdate = updateTime
	return redirectMap, nil
}

func withFile[T any](ds *CsvDataSource, callback func(f fs.File) (T, error)) (T, error) {
	var result T
	f, err := os.Open(ds.filePath)
	if err != nil {
		return result, err
	}
	// Defer closing the file
	defer f.Close()

	result, err = callback(f)
	return result, err
}

func CreateCsvDataSource(filePath string, checkModificationTime bool) *CsvDataSource {
	return &CsvDataSource{filePath: filePath, lastUpdate: time.Time{}, checkModificationTime: checkModificationTime}
}
