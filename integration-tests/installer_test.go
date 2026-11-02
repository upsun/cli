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

	tag             string   // The latest release tag.
	validTokens     []string // The tokens that the API accepts.
	rateLimitStatus int      // If set, the status of the rate limit API response.
	apiStatus       int      // If set, the status of the latest release API response.
	noRedirect      bool     // If set, the latest release page does not redirect.

	mu       sync.Mutex
	requests []fakeGitHubRequest
}

type fakeGitHubRequest struct {
	path, auth string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{t: t, tag: "v1.2.3", validTokens: []string{"valid-token", "gh-token"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rate_limit", func(w http.ResponseWriter, r *http.Request) {
		if g.rateLimitStatus != 0 {
			w.WriteHeader(g.rateLimitStatus)
			return
		}
		if !slices.Contains(g.validTokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
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
// If noJQ is set, jq is left out of the PATH.
func runInstaller(t *testing.T, g *fakeGitHub, env []string, homeFiles map[string]string, noJQ bool) installerResult {
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

	path := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	if noJQ {
		path = fakeBin + string(os.PathListSeparator) + pathWithout(t, filepath.Join(dir, "path-bin"), "jq")
	}

	installer, err := filepath.Abs("../installer.sh")
	require.NoError(t, err)
	cmd := exec.Command("sh", installer)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"PATH=" + path,
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

// pathWithout creates a directory of links to the executables in the PATH, leaving out one name, and returns it.
func pathWithout(t *testing.T, dir, exclude string) string {
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for _, pathDir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(pathDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			link := filepath.Join(dir, e.Name())
			if e.Name() == exclude || e.IsDir() {
				continue
			}
			if _, err := os.Lstat(link); err == nil {
				continue // An earlier PATH entry wins.
			}
			require.NoError(t, os.Symlink(filepath.Join(pathDir, e.Name()), link))
		}
	}
	return dir
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
		name            string
		env             []string
		homeFiles       map[string]string
		rateLimitStatus int
		apiStatus       int
		noRedirect      bool
		needsJQ         bool // The case reads Composer auth JSON.
		noJQ            bool // Leave jq out of the PATH.

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
			env:         []string{"GH_FAKE_TOKEN=gh-token"},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer gh-token",
			wantOutput:  []string{"Using GitHub auth from the gh CLI"},
		},
		{
			name:        "GITHUB_TOKEN before the gh CLI",
			env:         []string{"GITHUB_TOKEN=valid-token", "GH_FAKE_TOKEN=gh-token"},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			dontOutput:  []string{"gh CLI"},
		},
		{
			name:        "rejected GITHUB_TOKEN falls through to the gh CLI",
			env:         []string{"GITHUB_TOKEN=invalid-token", "GH_FAKE_TOKEN=gh-token"},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer gh-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the GITHUB_TOKEN env variable, as GitHub rejected it",
				"Using GitHub auth from the gh CLI",
			},
		},
		{
			name:        "rejected gh CLI token falls through to COMPOSER_AUTH",
			needsJQ:     true,
			env:         []string{"GH_FAKE_TOKEN=invalid-token", "COMPOSER_AUTH=" + composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the gh CLI",
				"Using GitHub auth from the COMPOSER_AUTH env variable",
			},
		},
		{
			name:        "rejected COMPOSER_AUTH token falls through to auth.json",
			needsJQ:     true,
			env:         []string{`COMPOSER_AUTH={"github-oauth":{"github.com":"invalid-token"}}`},
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the COMPOSER_AUTH env variable",
				"Using GitHub auth from ",
			},
		},
		{
			name:            "token check error",
			env:             []string{"GITHUB_TOKEN=valid-token"},
			rateLimitStatus: http.StatusBadGateway,
			wantVersion:     "1.2.3",
			wantAuth:        "Bearer valid-token",
			wantOutput: []string{
				"Could not check the GitHub token from the GITHUB_TOKEN env variable (HTTP 502): using it anyway",
			},
		},
		{
			name:        "COMPOSER_AUTH",
			needsJQ:     true,
			env:         []string{"COMPOSER_AUTH=" + composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from the COMPOSER_AUTH env variable"},
		},
		{
			name:        "project container auth.json",
			needsJQ:     true,
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{"Using GitHub auth from ", ".global/auth.json"},
		},
		{
			name:        "COMPOSER_AUTH without a github.com token",
			needsJQ:     true,
			env:         []string{"COMPOSER_AUTH={\"gitlab-oauth\":{\"gitlab.com\":\"x\"}}"},
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			wantVersion: "1.2.3",
			wantAuth:    "Bearer valid-token",
			wantOutput:  []string{".global/auth.json"},
		},
		{
			name:        "Composer auth JSON is skipped without jq",
			env:         []string{"COMPOSER_AUTH=" + composerAuth},
			homeFiles:   map[string]string{".global/auth.json": composerAuth},
			noJQ:        true,
			wantVersion: "1.2.3",
			wantOutput:  []string{"Composer auth was found, but jq is not installed to read a GitHub token from it"},
			dontOutput:  []string{"Using GitHub auth"},
		},
		{
			name:        "invalid token",
			env:         []string{"GITHUB_TOKEN=invalid-token"},
			wantVersion: "1.2.3",
			wantOutput:  []string{"Ignoring the GitHub token from the GITHUB_TOKEN env variable, as GitHub rejected it"},
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
			if c.needsJQ {
				if _, err := exec.LookPath("jq"); err != nil {
					t.Skip("jq is not installed")
				}
			}
			g := newFakeGitHub(t)
			g.rateLimitStatus = c.rateLimitStatus
			g.apiStatus = c.apiStatus
			g.noRedirect = c.noRedirect

			r := runInstaller(t, g, c.env, c.homeFiles, c.noJQ)
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
