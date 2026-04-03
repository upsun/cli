package session

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/upsun/cli/internal/config"
)

// ResolveSessionID returns the current session ID by checking, in order:
//  1. {APP_ENV_PREFIX}SESSION_ID environment variable
//  2. <writableUserDir>/session-id file (written by session:switch)
//  3. config.API.SessionID field
//  4. "default"
func ResolveSessionID(cfg *config.Config) (string, error) {
	if id := os.Getenv(cfg.Application.EnvPrefix + "SESSION_ID"); id != "" {
		return id, nil
	}
	writableDir, err := cfg.WritableUserDir()
	if err != nil {
		return "", err
	}
	idFile := filepath.Join(writableDir, "session-id")
	if data, err := os.ReadFile(idFile); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	if cfg.API.SessionID != "" {
		return cfg.API.SessionID, nil
	}
	return "default", nil
}

// sanitiseID replaces runs of characters not in [a-zA-Z0-9_-] with a single hyphen,
// matching PHP's preg_replace('/[^\w\-]+/', '-', $id).
func sanitiseID(id string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	return b.String()
}

// sessionDirName returns the directory name for the OAuth session (e.g. "sess-default").
func sessionDirName(id string) string {
	return "sess-" + sanitiseID(id)
}

// cliDirName returns the directory name for CLI artifacts (e.g. "sess-cli-default").
func cliDirName(id string) string {
	return "sess-cli-" + sanitiseID(id)
}
