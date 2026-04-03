package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Session holds OAuth2 tokens. JSON field names must match the PHP platformsh/client format.
type Session struct {
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType"`
	Expires      int64  `json:"expires"`
	RefreshToken string `json:"refreshToken"`
}

// Store abstracts session file I/O for testing.
type Store interface {
	Load(path string) (*Session, error)
	Save(path string, s *Session) error
	Delete(dir string) error
	List(baseDir string) ([]string, error)
}

// FileStore is the production Store backed by the filesystem.
type FileStore struct{}

func (fs *FileStore) Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (fs *FileStore) Save(path string, s *Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Delete removes the directory containing the session file.
func (fs *FileStore) Delete(dir string) error {
	return os.RemoveAll(dir)
}

// List scans baseDir for sess-cli-* directories and returns the session IDs.
// This matches PHP's listSessionIds() which globs sess-cli-* to discover sessions.
func (fs *FileStore) List(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "sess-cli-") {
			id := strings.TrimPrefix(e.Name(), "sess-cli-")
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// MemStore is an in-memory Store for tests. It is not safe for concurrent use.
type MemStore struct {
	sessions map[string]*Session
	tokens   map[string]string
	dirs     map[string]bool
}

func NewMemStore() *MemStore {
	return &MemStore{
		sessions: make(map[string]*Session),
		tokens:   make(map[string]string),
		dirs:     make(map[string]bool),
	}
}

func (m *MemStore) Load(path string) (*Session, error) {
	s := m.sessions[path]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *MemStore) Save(path string, s *Session) error {
	cp := *s
	m.sessions[path] = &cp
	m.dirs[filepath.Dir(path)] = true
	return nil
}

func (m *MemStore) Delete(dir string) error {
	for k := range m.sessions {
		if strings.HasPrefix(k, dir+"/") || k == dir {
			delete(m.sessions, k)
		}
	}
	delete(m.dirs, dir)
	return nil
}

func (m *MemStore) List(baseDir string) ([]string, error) {
	var ids []string
	for dir := range m.dirs {
		parent := filepath.Dir(dir)
		if parent == baseDir {
			base := filepath.Base(dir)
			if strings.HasPrefix(base, "sess-cli-") {
				ids = append(ids, strings.TrimPrefix(base, "sess-cli-"))
			}
		}
	}
	return ids, nil
}
