package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/upsun/cli/internal/auth/store"
	"github.com/upsun/cli/internal/config"
)

// Migrator imports sessions from the legacy CLI's storage, once.
type Migrator struct {
	// Export runs the legacy CLI's hidden auth:export-sessions command and returns its output.
	// With del set, it runs "auth:export-sessions --delete" instead, which deletes the exported copies.
	Export func(ctx context.Context, del bool) ([]byte, error)

	// DebugLog logs a debug message. It may be nil.
	DebugLog func(format string, args ...any)
}

// migrationMarker is the content of the marker file.
type migrationMarker struct {
	// DeletePending records that the legacy CLI's copies have not been deleted yet.
	DeletePending bool `json:"delete_pending,omitempty"`
}

// exportedSession is a session as exported by the legacy CLI.
type exportedSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Expires      int64  `json:"expires"`
	APIToken     string `json:"api_token"`
}

// Run migrates sessions if that has not been done yet.
//
// Sessions are never imported twice, because a second import would overwrite fresh tokens with rotated ones.
func (mg *Migrator) Run(ctx context.Context, m *Manager) error {
	markerPath := filepath.Join(m.Store.Dir, store.MigrationMarker)
	if mk, err := readMarker(markerPath); err != nil || mk != nil {
		if err == nil && mk.DeletePending {
			mg.deleteExported(ctx, markerPath)
		}
		return err
	}

	if !m.Settings.DisableLocks {
		unlock, err := m.fileLock(ctx, filepath.Join(m.Store.Dir, ".migrate.lock"))
		if err != nil {
			return err
		}
		defer unlock()
		if mk, err := readMarker(markerPath); err != nil || mk != nil {
			return err
		}
	}

	out, err := mg.Export(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to export credentials from the legacy CLI: %w", err)
	}
	var sessions map[string]exportedSession
	if err := json.Unmarshal(out, &sessions); err != nil {
		return fmt.Errorf("failed to parse credentials exported from the legacy CLI: %w", err)
	}
	for id, s := range sessions {
		// API token sessions are skipped, as the token is exchanged again.
		if strings.HasPrefix(id, "api-token-") || config.ValidateSessionID(id) != nil {
			continue
		}
		if s.AccessToken == "" && s.RefreshToken == "" && s.APIToken == "" {
			continue
		}
		if existing, err := m.Store.Load(id); err != nil {
			return err
		} else if existing != nil {
			continue
		}
		entry := &store.Entry{
			AccessToken:  s.AccessToken,
			RefreshToken: s.RefreshToken,
			TokenType:    s.TokenType,
			Expires:      s.Expires,
			APIToken:     s.APIToken,
		}
		if err := m.Store.Save(id, entry); err != nil {
			return err
		}
		mg.debugf("Migrated session: %s", id)
	}

	if err := writeMarker(markerPath, &migrationMarker{DeletePending: true}); err != nil {
		return err
	}
	mg.deleteExported(ctx, markerPath)
	return nil
}

// deleteExported deletes the legacy CLI's copies of the exported sessions. On failure it is retried on a later run.
func (mg *Migrator) deleteExported(ctx context.Context, markerPath string) {
	if _, err := mg.Export(ctx, true); err != nil {
		mg.debugf("Failed to delete the legacy CLI's credentials: %s", err)
		return
	}
	if err := writeMarker(markerPath, &migrationMarker{}); err != nil {
		mg.debugf("Failed to write %s: %s", markerPath, err)
	}
}

func (mg *Migrator) debugf(format string, args ...any) {
	if mg.DebugLog != nil {
		mg.DebugLog(format, args...)
	}
}

func readMarker(path string) (*migrationMarker, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	mk := &migrationMarker{}
	if len(b) > 0 {
		if err := json.Unmarshal(b, mk); err != nil {
			return nil, fmt.Errorf("invalid file %s: %w", path, err)
		}
	}
	return mk, nil
}

func writeMarker(path string, mk *migrationMarker) error {
	b, err := json.Marshal(mk)
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(path, b)
}
