package tests

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAuthRefresh_Concurrent runs several processes that load the same expired session.
// Only one should use the refresh token: the others must use the token it saved.
func TestAuthRefresh_Concurrent(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	// Keep the first refresh in progress while the other processes start.
	authServer.SetRefreshDelay(2 * time.Second)
	authServer.AddRefreshToken("initial-refresh-token")

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	// Initialize before running commands concurrently.
	f.dir = t.TempDir()
	getCommandName(t)
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "initial-refresh-token",
	})

	const processes = 3
	type result struct {
		stdout, stderr string
		err            error
	}
	results := make([]result, processes)
	var wg sync.WaitGroup
	for i := range processes {
		wg.Go(func() {
			stdout, stderr, err := f.RunCombinedOutput("auth:token", "--no-warn")
			results[i] = result{stdout, stderr, err}
		})
	}
	wg.Wait()

	for i, r := range results {
		if assert.NoError(t, r.err, "process %d stderr: %s", i, r.stderr) {
			assert.Equal(t, "access-token-1", strings.TrimSpace(r.stdout), "process %d", i)
		}
	}
	assert.False(t, authServer.ReuseDetected(), "a rotated refresh token was reused")
}
