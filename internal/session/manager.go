package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/upsun/cli/internal/config"
)

// Manager is the single entry point for all session operations.
type Manager struct {
	cfg   *config.Config
	store Store
	id    string // cached resolved session ID
}

// New creates a Manager backed by the filesystem.
func New(cfg *config.Config) (*Manager, error) {
	id, err := ResolveSessionID(cfg)
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, store: &FileStore{}, id: id}, nil
}

// NewWithStore creates a Manager with an injected Store (for testing).
func NewWithStore(cfg *config.Config, store Store) *Manager {
	id, err := ResolveSessionID(cfg)
	if err != nil {
		// WritableUserDir is misconfigured; fall back to "default" and warn.
		fmt.Fprintf(os.Stderr, "session: could not resolve session ID: %v\n", err)
		id = "default"
	}
	return &Manager{cfg: cfg, store: store, id: id}
}

// NewWithID creates a Manager for a specific session ID (used by logout --other).
func NewWithID(cfg *config.Config, id string) *Manager {
	return &Manager{cfg: cfg, store: &FileStore{}, id: id}
}

// SessionID returns the resolved session ID.
func (m *Manager) SessionID() string { return m.id }

func (m *Manager) sessionBaseDir() (string, error) {
	writableDir, err := m.cfg.WritableUserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(writableDir, ".session"), nil
}

// sessionPath returns the path to the OAuth session JSON file.
// Pattern: <writableDir>/.session/sess-<id>/sess-<id>.json
// Matches the PHP platformsh/client File storage format.
func (m *Manager) sessionPath() (string, error) {
	base, err := m.sessionBaseDir()
	if err != nil {
		return "", err
	}
	slug := sessionDirName(m.id)
	return filepath.Join(base, slug, slug+".json"), nil
}

// cliDir returns the path to the CLI artifact directory for this session.
// Pattern: <writableDir>/.session/sess-cli-<id>/
// Used for API token storage and as a session existence marker.
func (m *Manager) cliDir() (string, error) {
	base, err := m.sessionBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, cliDirName(m.id)), nil
}

func (m *Manager) tokenPath() (string, error) {
	dir, err := m.cliDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "api-token"), nil
}

// Load reads the current session from disk. Returns (nil, nil) if no session exists.
func (m *Manager) Load() (*Session, error) {
	path, err := m.sessionPath()
	if err != nil {
		return nil, err
	}
	return m.store.Load(path)
}

// Save writes the session to disk and creates the sess-cli-<id>/ marker directory.
func (m *Manager) Save(s *Session) error {
	path, err := m.sessionPath()
	if err != nil {
		return err
	}
	if err := m.store.Save(path, s); err != nil {
		return err
	}
	// Create the sess-cli-<id>/ marker so List() can discover this session.
	dir, err := m.cliDir()
	if err != nil {
		return err
	}
	return m.store.MkdirAll(dir)
}

// Delete removes the current session (both OAuth file and CLI artifact dir).
func (m *Manager) Delete() error {
	path, err := m.sessionPath()
	if err != nil {
		return err
	}
	if err := m.store.Delete(filepath.Dir(path)); err != nil {
		return err
	}
	dir, err := m.cliDir()
	if err != nil {
		return err
	}
	return m.store.Delete(dir)
}

// DeleteAll removes all sessions.
func (m *Manager) DeleteAll() error {
	base, err := m.sessionBaseDir()
	if err != nil {
		return err
	}
	ids, err := m.List()
	if err != nil {
		return err
	}
	for _, id := range ids {
		sub := &Manager{cfg: m.cfg, store: m.store, id: id}
		if err := sub.Delete(); err != nil {
			return fmt.Errorf("delete session %q: %w", id, err)
		}
	}
	// Also remove any sess-<id> dirs (oauth session dirs not covered by List).
	entries, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "sess-") && !strings.HasPrefix(e.Name(), "sess-cli-") {
			if err := os.RemoveAll(filepath.Join(base, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// List returns all session IDs discovered via sess-cli-* directories.
func (m *Manager) List() ([]string, error) {
	base, err := m.sessionBaseDir()
	if err != nil {
		return nil, err
	}
	ids, err := m.store.List(base)
	if err != nil {
		return nil, err
	}
	// Exclude api-token-specific session IDs (PHP convention).
	var filtered []string
	for _, id := range ids {
		if !strings.HasPrefix(id, "api-token-") {
			filtered = append(filtered, id)
		}
	}
	return filtered, nil
}

// GetAPIToken reads the stored API token for the current session.
// Returns ("", nil) if no token is stored.
func (m *Manager) GetAPIToken() (string, error) {
	path, err := m.tokenPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SetAPIToken writes an API token to disk.
func (m *Manager) SetAPIToken(token string) error {
	path, err := m.tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// DeleteAPIToken removes the stored API token.
func (m *Manager) DeleteAPIToken() error {
	path, err := m.tokenPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// SetActiveSessionID writes the session ID to the session-id file,
// persisting the active session across invocations.
func (m *Manager) SetActiveSessionID(id string) error {
	writableDir, err := m.cfg.WritableUserDir()
	if err != nil {
		return err
	}
	path := filepath.Join(writableDir, "session-id")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(id), 0600)
}
