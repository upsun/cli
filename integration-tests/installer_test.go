package tests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGitHub serves the GitHub endpoints that installer.sh uses: the releases API,
// the latest release redirect, and release downloads.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	tag        string // The latest release tag.
	validToken string // The token that the API accepts.
	apiStatus  int    // If set, the status of the latest release API response.
	noRedirect bool   // If set, the latest release page does not redirect.

	mu       sync.Mutex
	requests []fakeGitHubRequest
}

type fakeGitHubRequest struct {
	path, auth string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{t: t, tag: "v1.2.3", validToken: "valid-token"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rate_limit", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+g.validToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"resources":{}}`))
	})
	mux.HandleFunc("GET /api/repos/upsun/cli/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		if g.apiStatus != 0 {
			w.WriteHeader(g.apiStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": g.tag, "name": g.tag})
	})
	mux.HandleFunc("GET /upsun/cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if g.noRedirect {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, g.srv.URL+"/upsun/cli/releases/tag/"+g.tag, http.StatusFound)
	})
	mux.HandleFunc("GET /upsun/cli/releases/download/{tag}/{file}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fakeCLIArchive(t, strings.TrimPrefix(r.PathValue("tag"), "v")))
	})
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.requests = append(g.requests, fakeGitHubRequest{path: r.URL.Path, auth: r.Header.Get("Authorization")})
		g.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// requestsTo returns the requests made to a path.
func (g *fakeGitHub) requestsTo(path string) []fakeGitHubRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	var found []fakeGitHubRequest
	for _, r := range g.requests {
		if r.path == path {
			found = append(found, r)
		}
	}
	return found
}

// fakeCLIArchive returns a release archive containing an "upsun" script that prints its version.
func fakeCLIArchive(t *testing.T, version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\necho \"fake upsun " + version + "\"\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "upsun", Mode: 0o755, Size: int64(len(script))}))
	_, err := tw.Write([]byte(script))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

type installerResult struct {
	output  string
	err     error
	binPath string
}

// runInstaller runs installer.sh with the raw install method, against the fake GitHub.
// The environment is built from scratch, so that the developer's or CI's tokens are not used.
// A fake gh CLI is first in the PATH: it is logged in if env has GH_FAKE_TOKEN.
func runInstaller(t *testing.T, g *fakeGitHub, env []string, homeFiles map[string]string) installerResult {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	binDir := filepath.Join(dir, "bin")
	fakeBin := filepath.Join(dir, "fake-bin")
	require.NoError(t, os.MkdirAll(home, 0o700))
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	for name, content := range homeFiles {
		p := filepath.Join(home, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	gh := `#!/bin/sh
[ -n "${GH_FAKE_TOKEN:-}" ] || exit 1
case "$1 $2" in
    "auth status") exit 0 ;;
    "auth token") echo "$GH_FAKE_TOKEN" ;;
    *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(gh), 0o755))

	installer, err := filepath.Abs("../installer.sh")
	require.NoError(t, err)
	cmd := exec.Command("sh", installer)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + dir,
		"CI=1", // Skips shell completion.
		"INSTALL_METHOD=raw",
		"INSTALL_DIR=" + binDir,
		"INSTALL_LOG=" + filepath.Join(dir, "install.log"),
		"RELEASES_URL=" + g.srv.URL + "/upsun/cli/releases",
		"RELEASES_API_URL=" + g.srv.URL + "/api/repos/upsun/cli/releases",
	}, env...)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	return installerResult{output: string(out), err: err, binPath: filepath.Join(binDir, "upsun")}
}

// assertInstalled checks that the installed binary has the version.
func assertInstalled(t *testing.T, r installerResult, version string) {
	require.NoError(t, r.err)
	out, err := exec.Command(r.binPath).Output()
	require.NoError(t, err)
	assert.Equal(t, "fake upsun "+version+"\n", string(out))
}

func TestInstallerLatestVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test due to -short flag")
	}
	if runtime.GOOS == "windows" {
		t.Skip("installer.sh does not run on Windows")
	}
	t.Parallel()

	const latestPath = "/api/repos/upsun/cli/releases/latest"
	composerAuth := `{
    "github-oauth": {
        "github.example.com": "other-host",
        "github.com": "valid-token"
    }
}`

	cases := []struct {
		name       string
		env        []string
		homeFiles  map[string]string
		apiStatus  int
		noRedirect bool

		wantVersion string // Empty if the install fails.
		wantAuth    string // The Authorization header sent to the releases API.
		wantOutput  []string
		dontOutput  []string
	}{
		{
			name:        "no token",
			wantVersion: "1.2.3",
			wantOutput:  []string{"No version specified, using latest (1.2.3)"},
			dontOutput:  []string{"Using GitHub auth"},
		},
		{
			name:        "pinned version",
			env:         []string{"VERSION=1.0.0"},
			wantVersion: "1.0.0",
			wantOutput:  []string{"Version 1.0.0 specified"},
		},
		{
			name:        "GITHUB_TOKEN",
			env:         []string{"GITHUB_TOKEN=valid-token", "COMPOSER_AUTH={}"},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from the GITHUB_TOKEN env variable"},
		},
		{
			name:        "gh CLI",
			env:         []string{"GH_FAKE_TOKEN=valid-token"},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from the gh CLI"},
		},
		{
			name:        "COMPOSER_AUTH",
			env:         []string{"COMPOSER_AUTH=" + composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from the COMPOSER_AUTH env variable"},
		},
		{
			name:        "project container auth.json",
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from ", ".global/auth.json"},
		},
		{
			name:        "COMPOSER_AUTH without a github.com token",
			env:         []string{"COMPOSER_AUTH={\"gitlab-oauth\":{\"gitlab.com\":\"x\"}}"},
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{".global/auth.json"},
		},
		{
			name:        "invalid token",
			env:         []string{"GITHUB_TOKEN=invalid-token"},
			wantVersion: "1.2.3",
			wantOutput:  []string{"Ignoring the GitHub token from the GITHUB_TOKEN env variable"},
			dontOutput:  []string{"Using GitHub auth"},
		},
		{
			name:        "rate limited",
			apiStatus:   http.StatusForbidden,
			wantVersion: "1.2.3",
			wantOutput: []string{
				"Could not get the latest release from the GitHub API",
				"error: 403",
				"set GITHUB_TOKEN to use a token",
				"No version specified, using latest (1.2.3)",
			},
		},
		{
			name:        "API error",
			apiStatus:   http.StatusInternalServerError,
			wantVersion: "1.2.3",
			wantOutput:  []string{"error: 500"},
			dontOutput:  []string{"set GITHUB_TOKEN"},
		},
		{
			name:       "no version found",
			apiStatus:  http.StatusForbidden,
			noRedirect: true,
			wantOutput: []string{
				"error: 403",
				"Could not get the latest release from github.com",
				"ERROR: Could not determine CLI version",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := newFakeGitHub(t)
			g.apiStatus = c.apiStatus
			g.noRedirect = c.noRedirect

			r := runInstaller(t, g, c.env, c.homeFiles)
			if c.wantVersion == "" {
				assert.Error(t, r.err)
				assert.NoFileExists(t, r.binPath)
			} else {
				assertInstalled(t, r, c.wantVersion)
			}
			for _, s := range c.wantOutput {
				assert.Contains(t, r.output, s)
			}
			for _, s := range c.dontOutput {
				assert.NotContains(t, r.output, s)
			}

			latest := g.requestsTo(latestPath)
			if slices.ContainsFunc(c.env, func(e string) bool { return strings.HasPrefix(e, "VERSION=") }) {
				assert.Empty(t, latest, "a pinned version should not need the API")
				return
			}
			require.Len(t, latest, 1)
			assert.Equal(t, c.wantAuth, latest[0].auth)
		})
	}
}
