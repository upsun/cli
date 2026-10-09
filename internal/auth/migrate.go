package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

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
	// ExportPending records that the export failed, and DeletePending that the legacy CLI's copies were not deleted.
	ExportPending bool `json:"export_pending,omitempty"`
	DeletePending bool `json:"delete_pending,omitempty"`
	// RetryAfter is when a failed step may be retried, as a Unix timestamp.
	RetryAfter int64 `json:"retry_after,omitempty"`
}

// migrationRetryDelay is how long to wait before retrying a failed migration step.
const migrationRetryDelay = time.Hour

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
// A failure does not block authentication: it is reported, and retried after migrationRetryDelay.
func (mg *Migrator) Run(ctx context.Context, m *Manager) error {
	markerPath := filepath.Join(m.Store.Dir, store.MigrationMarker)
	if mk, err := readMarker(markerPath); err != nil || !mg.due(m, mk) {
		return err
	}
	if !m.Settings.DisableLocks {
		unlock, err := m.fileLock(ctx, filepath.Join(m.Store.Dir, ".migrate.lock"))
		if err != nil {
			return err
		}
		defer unlock()
	}
	mk, err := readMarker(markerPath)
	if err != nil || !mg.due(m, mk) {
		return err
	}

	if mk == nil || mk.ExportPending {
		switch legacy := mg.legacyState(m); {
		case legacy == legacyEmpty:
			return writeMarker(markerPath, &migrationMarker{})
		case mk != nil && legacy == legacyImported:
			// Only the deletion is left.
		default:
			if err := mg.importSessions(ctx, m); err != nil {
				if m.Stderr != nil {
					fmt.Fprintf(m.Stderr, "Warning: failed to migrate credentials from the legacy CLI: %s\n", err)
				}
				return writeMarker(markerPath, mg.retryLater(&migrationMarker{ExportPending: true}))
			}
		}
	}

	// The legacy CLI's copies are deleted to avoid having two sources of truth.
	if _, err := mg.Export(ctx, true); err != nil {
		mg.debugf("Failed to delete the legacy CLI's credentials: %s", err)
		return writeMarker(markerPath, mg.retryLater(&migrationMarker{DeletePending: true}))
	}
	return writeMarker(markerPath, &migrationMarker{})
}

// due reports whether a migration step should run, given the marker (nil if there is none).
//
// A failed export is retried early if it is no longer needed, e.g. because the user logged in again.
func (mg *Migrator) due(m *Manager, mk *migrationMarker) bool {
	switch {
	case mk == nil:
		return true
	case mk.ExportPending && mg.legacyState(m) != legacyUnknown:
		return true
	default:
		return (mk.ExportPending || mk.DeletePending) && time.Now().Unix() >= mk.RetryAfter
	}
}

type legacyStorageState int

const (
	legacyUnknown  legacyStorageState = iota // Sessions may exist that are not in the Go store.
	legacyImported                           // Every legacy session is already in the Go store.
	legacyEmpty                              // The legacy storage holds no sessions.
)

// legacyState checks the legacy CLI's session files by their names, without reading them.
//
// Sessions in the keychain can only be listed by the legacy credential helper, so they count as unknown.
func (mg *Migrator) legacyState(m *Manager) legacyStorageState {
	writableDir := filepath.Dir(m.Store.Dir)
	helper := filepath.Join(writableDir, "credential-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if _, err := os.Stat(helper); err == nil {
		return legacyUnknown
	}
	sessionDir := filepath.Join(writableDir, ".session")
	var ids []string
	files, _ := filepath.Glob(filepath.Join(sessionDir, "sess-*", "sess-*.json"))
	for _, f := range files {
		id := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(f), ".json"), "sess-")
		if filepath.Base(filepath.Dir(f)) == "sess-"+id {
			ids = append(ids, id)
		}
	}
	tokenFiles, _ := filepath.Glob(filepath.Join(sessionDir, "sess-cli-*", "api-token"))
	for _, f := range tokenFiles {
		ids = append(ids, strings.TrimPrefix(filepath.Base(filepath.Dir(f)), "sess-cli-"))
	}
	if len(ids) == 0 {
		return legacyEmpty
	}
	stored, err := m.Store.List()
	if err != nil {
		return legacyUnknown
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "api-token-") && config.ValidateSessionID(id) == nil && !slices.Contains(stored, id) {
			return legacyUnknown
		}
	}
	return legacyImported
}

func (mg *Migrator) retryLater(mk *migrationMarker) *migrationMarker {
	mk.RetryAfter = time.Now().Add(migrationRetryDelay).Unix()
	return mk
}

// importSessions exports sessions from the legacy CLI and saves the ones that are not already stored.
func (mg *Migrator) importSessions(ctx context.Context, m *Manager) error {
	out, err := mg.Export(ctx, false)
	if err != nil {
		return err
	}
	var sessions map[string]exportedSession
	if err := json.Unmarshal(out, &sessions); err != nil {
		return fmt.Errorf("invalid export: %w", err)
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
	return nil
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
