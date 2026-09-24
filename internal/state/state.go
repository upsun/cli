package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/upsun/cli/internal/config"
)

type State struct {
	Updates struct {
		LastChecked        int64  `json:"last_checked"`
		LastNotified       int64  `json:"last_notified,omitempty"`
		KnownLatestVersion string `json:"known_latest_version,omitempty"`
	} `json:"updates,omitempty"`

	ConfigUpdates struct {
		LastChecked int64 `json:"last_checked"`
	} `json:"config_updates,omitempty"`
}

// Load reads state from the filesystem.
func Load(cnf *config.Config) (state State, err error) {
	statePath, err := getPath(cnf)
	if err != nil {
		return
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}
	err = json.Unmarshal(data, &state)
	return
}

// Save writes state to the filesystem.
func Save(state State, cnf *config.Config) error {
	statePath, err := getPath(cnf)
	if err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// Write to a temporary file and rename it, so that other processes never
	// read a partially written file. The rename can fail on Windows while
	// another process has the file open, so fall back to a direct write.
	if err := writeAndRename(statePath, data); err != nil {
		return os.WriteFile(statePath, data, 0o600)
	}
	return nil
}

func writeAndRename(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

var mu sync.Mutex

// Update loads the state, applies fn and saves it. Reloading under a lock stops
// concurrent updates in one process from dropping each other's fields. An
// unreadable state file is replaced, so a corrupt file does not persist.
func Update(cnf *config.Config, fn func(*State)) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := Load(cnf)
	if err != nil {
		s = State{}
	}
	fn(&s)
	return Save(s, cnf)
}

// getPath determines the path to the state JSON file depending on config.
func getPath(cnf *config.Config) (string, error) {
	writableDir, err := cnf.WritableUserDir() //nolint:staticcheck // backwards compatibility is needed for state files
	if err != nil {
		return "", err
	}

	return filepath.Join(writableDir, cnf.Application.UserStateFile), nil
}
