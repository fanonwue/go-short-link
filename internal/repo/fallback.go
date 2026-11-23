package repo

import (
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"

	"github.com/fanonwue/go-short-link/internal/state"
	"github.com/fanonwue/go-short-link/internal/util"
	"github.com/fanonwue/goutils/logging"
)

type (
	FallbackFileEntry struct {
		Key    string `json:"key"`
		Target string `json:"target"`
	}
)

func writeFallbackFile(path string, newMapping state.RedirectMap) error {
	if len(path) == 0 {
		logging.Debugf("Fallback file path is empty, skipping write")
		return nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	jsonEntries := make([]FallbackFileEntry, 0, len(newMapping))

	for key, target := range newMapping {
		jsonEntries = append(jsonEntries, FallbackFileEntry{
			Key:    key,
			Target: target,
		})
	}

	return writeFallbackContent(path, func(writer io.Writer) error {
		return json.MarshalWrite(writer, jsonEntries)
	})
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
		logging.Warnf("Could not read fallback file %s: %v", path, fallbackErr)
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
