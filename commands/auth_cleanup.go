package commands

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/upsun/cli/internal/config"
)

// clearLegacySessionFiles deletes the legacy CLI's files for sessions that were logged out: the API cache, which can
// hold the previous account's data, each session's SSH certificate and config, and the current session's SSH include.
// With all set, the whole legacy session directory is deleted.
func clearLegacySessionFiles(cnf *config.Config, current string, ids []string, all bool) error {
	dir, err := cnf.WritableUserDir() //nolint:staticcheck // the legacy CLI's files are in the user dir
	if err != nil {
		return err
	}
	sessionDir := filepath.Join(dir, ".session")
	paths := []string{filepath.Join(dir, "cache")}
	for _, id := range ids {
		paths = append(paths, filepath.Join(sessionDir, "sess-cli-"+id))
	}
	if all || slices.Contains(ids, current) {
		paths = append(paths, filepath.Join(dir, "ssh", "session.config"))
	}
	if all {
		paths = append(paths, sessionDir)
	}
	var errs []error
	for _, p := range paths {
		errs = append(errs, os.RemoveAll(p))
	}
	return errors.Join(errs...)
}
