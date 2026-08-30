package ds

import (
	"encoding/csv"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/goutils"
	"github.com/fanonwue/goutils/logging"
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
	fileInfo, err := ds.withFile(func(f fs.File) (fs.FileInfo, error) {
		return f.Stat()
	})
	if err != nil {
		logging.Errorf("Could not get file info: %v", err)
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
	return ds.withFile(func(f fs.File) (state.RedirectMap, error) {
		return ds.fetchRedirectMappingInternal(f)
	})
}

func (ds *CsvDataSource) Id() string {
	return "CsvDataSource#" + ds.filePath
}

func (ds *CsvDataSource) fetchRedirectMappingInternal(f fs.File) (state.RedirectMap, error) {
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

// withFile is a helper function that opens a file and calls a callback function with it. Internally, it uses [goutils.WithFile]
// to ensure that the file is closed after the callback has finished.
func (ds *CsvDataSource) withFile[T any](callback func(f fs.File) (T, error)) (T, error) {
	return goutils.WithFile(ds.filePath, func(file *os.File) (T, error) {
		return callback(file)
	})
}

func CreateCsvDataSource(filePath string, checkModificationTime bool) *CsvDataSource {
	return &CsvDataSource{filePath: filePath, lastUpdate: time.Time{}, checkModificationTime: checkModificationTime}
}
