package tests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
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

// fakeGitHub serves the GitHub endpoints that installer.sh uses: the latest release page (which redirects to
// the release), the releases API, and release downloads.
type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	tag            string   // The latest release tag.
	validTokens    []string // The tokens that the API accepts.
	redirectStatus int      // If set, the status of the latest release page, instead of a redirect.
	apiStatus      int      // If set, the status of the latest release API response (for an accepted or no token).

	mu       sync.Mutex
	requests []fakeGitHubRequest
}

type fakeGitHubRequest struct {
	path, auth string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{t: t, tag: "v1.2.3", validTokens: []string{"valid-token", "gh-token"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /upsun/cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if g.redirectStatus != 0 {
			w.WriteHeader(g.redirectStatus)
			return
		}
		http.Redirect(w, r, g.srv.URL+"/upsun/cli/releases/tag/"+g.tag, http.StatusFound)
	})
	mux.HandleFunc("GET /api/repos/upsun/cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" &&
			!slices.Contains(g.validTokens, strings.TrimPrefix(auth, "Bearer ")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if g.apiStatus != 0 {
			w.WriteHeader(g.apiStatus)
			return
		}
		// The API's JSON is pretty-printed.
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\n  \"name\": %q,\n  \"tag_name\": %q\n}\n", g.tag, g.tag)
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

// authHeaders returns the Authorization headers of the requests to paths with a prefix, in order.
func (g *fakeGitHub) authHeaders(pathPrefix string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	found := []string{}
	for _, r := range g.requests {
		if strings.HasPrefix(r.path, pathPrefix) {
			found = append(found, r.auth)
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

	const (
		redirectPath = "/upsun/cli/releases/latest"
		apiPath      = "/api/repos/upsun/cli/releases/latest"
		downloadPath = "/upsun/cli/releases/download/"
	)
	composerAuth := `{
    "github-oauth": {
        "github.example.com": "other-host",
        "github.com": "valid-token"
    }
}`
	noRedirect := "Could not get the latest release from github.com: no redirect"

	cases := []struct {
		name           string
		env            []string
		homeFiles      map[string]string
		redirectStatus int
		apiStatus      int
		needsJQ        bool // The case reads Composer auth JSON.
		noJQ           bool // Leave jq out of the PATH.

		wantVersion      string   // Empty if the install fails.
		wantRedirects    int      // The number of requests to the latest release page.
		wantAPIAuth      []string // The Authorization headers sent to the releases API, in order.
		wantDownloadAuth string   // The Authorization header sent with the download.
		wantOutput       []string
		dontOutput       []string
	}{
		{
			name:        "pinned version",
			env:         []string{"VERSION=1.0.0", "GITHUB_TOKEN=valid-token"},
			wantVersion: "1.0.0",
			wantAPIAuth: []string{},
			wantOutput:  []string{"Version 1.0.0 specified"},
			dontOutput:  []string{"GitHub auth"},
		},
		{
			name:          "redirect",
			wantVersion:   "1.2.3",
			wantRedirects: 1,
			wantAPIAuth:   []string{},
			wantOutput:    []string{"No version specified, using latest (1.2.3)"},
			dontOutput:    []string{"Could not", "GitHub auth"},
		},
		{
			name:          "redirect without using tokens",
			env:           []string{"GITHUB_TOKEN=valid-token", "GH_FAKE_TOKEN=gh-token"},
			homeFiles:     map[string]string{".global/auth.json": composerAuth},
			wantVersion:   "1.2.3",
			wantRedirects: 1,
			wantAPIAuth:   []string{},
			dontOutput:    []string{"GitHub auth", "GitHub token"},
		},
		{
			name:           "API without a token",
			redirectStatus: http.StatusNotFound,
			wantVersion:    "1.2.3",
			wantRedirects:  1,
			wantAPIAuth:    []string{""},
			wantOutput: []string{
				"Could not get the latest release from github.com: curl: (22) The requested URL returned error: 404",
				"No version specified, using latest (1.2.3)",
			},
			dontOutput: []string{"GitHub API"},
		},
		{
			name:           "API when the page does not redirect",
			redirectStatus: http.StatusOK,
			wantVersion:    "1.2.3",
			wantRedirects:  1,
			wantAPIAuth:    []string{""},
			wantOutput:     []string{noRedirect},
		},
		{
			name:             "GITHUB_TOKEN",
			env:              []string{"GITHUB_TOKEN=valid-token"},
			redirectStatus:   http.StatusOK,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput:       []string{"Using GitHub auth from the GITHUB_TOKEN env variable"},
		},
		{
			name:             "gh CLI",
			env:              []string{"GH_FAKE_TOKEN=gh-token"},
			redirectStatus:   http.StatusOK,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer gh-token"},
			wantDownloadAuth: "Bearer gh-token",
			wantOutput:       []string{"Using GitHub auth from the gh CLI"},
		},
		{
			name:             "COMPOSER_AUTH",
			env:              []string{"COMPOSER_AUTH=" + composerAuth},
			redirectStatus:   http.StatusOK,
			needsJQ:          true,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput:       []string{"Using GitHub auth from the COMPOSER_AUTH env variable"},
		},
		{
			name:             "project container auth.json",
			homeFiles:        map[string]string{".global/auth.json": composerAuth},
			redirectStatus:   http.StatusOK,
			needsJQ:          true,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput:       []string{"Using GitHub auth from ", ".global/auth.json"},
		},
		{
			name:             "COMPOSER_AUTH without a github.com token",
			env:              []string{`COMPOSER_AUTH={"gitlab-oauth":{"gitlab.com":"x"}}`},
			homeFiles:        map[string]string{".global/auth.json": composerAuth},
			redirectStatus:   http.StatusOK,
			needsJQ:          true,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput:       []string{".global/auth.json"},
		},
		{
			name:             "GITHUB_TOKEN before the gh CLI",
			env:              []string{"GITHUB_TOKEN=valid-token", "GH_FAKE_TOKEN=gh-token"},
			redirectStatus:   http.StatusOK,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			dontOutput:       []string{"gh CLI"},
		},
		{
			name:             "rejected GITHUB_TOKEN falls through to the gh CLI",
			env:              []string{"GITHUB_TOKEN=invalid-token", "GH_FAKE_TOKEN=gh-token"},
			redirectStatus:   http.StatusOK,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer invalid-token", "Bearer gh-token"},
			wantDownloadAuth: "Bearer gh-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the GITHUB_TOKEN env variable, as GitHub rejected it",
				"Using GitHub auth from the gh CLI",
			},
		},
		{
			name:             "rejected gh CLI token falls through to COMPOSER_AUTH",
			env:              []string{"GH_FAKE_TOKEN=invalid-token", "COMPOSER_AUTH=" + composerAuth},
			redirectStatus:   http.StatusOK,
			needsJQ:          true,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer invalid-token", "Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the gh CLI",
				"Using GitHub auth from the COMPOSER_AUTH env variable",
			},
		},
		{
			name:             "rejected COMPOSER_AUTH token falls through to auth.json",
			env:              []string{`COMPOSER_AUTH={"github-oauth":{"github.com":"invalid-token"}}`},
			homeFiles:        map[string]string{".global/auth.json": composerAuth},
			redirectStatus:   http.StatusOK,
			needsJQ:          true,
			wantVersion:      "1.2.3",
			wantRedirects:    1,
			wantAPIAuth:      []string{"Bearer invalid-token", "Bearer valid-token"},
			wantDownloadAuth: "Bearer valid-token",
			wantOutput: []string{
				"Ignoring the GitHub token from the COMPOSER_AUTH env variable",
				"Using GitHub auth from ",
			},
		},
		{
			name:           "rejected token falls through to no token",
			env:            []string{"GITHUB_TOKEN=invalid-token"},
			redirectStatus: http.StatusOK,
			wantVersion:    "1.2.3",
			wantRedirects:  1,
			wantAPIAuth:    []string{"Bearer invalid-token", ""},
			wantOutput:     []string{"Ignoring the GitHub token from the GITHUB_TOKEN env variable, as GitHub rejected it"},
			dontOutput:     []string{"Using GitHub auth"},
		},
		{
			name:           "Composer auth JSON is skipped without jq",
			env:            []string{"COMPOSER_AUTH=" + composerAuth},
			homeFiles:      map[string]string{".global/auth.json": composerAuth},
			redirectStatus: http.StatusOK,
			noJQ:           true,
			wantVersion:    "1.2.3",
			wantRedirects:  1,
			wantAPIAuth:    []string{""},
			wantOutput:     []string{"Composer auth was found, but jq is not installed to read a GitHub token from it"},
			dontOutput:     []string{"Using GitHub auth"},
		},
		{
			name:           "rate limited",
			redirectStatus: http.StatusOK,
			apiStatus:      http.StatusForbidden,
			wantRedirects:  1,
			wantAPIAuth:    []string{""},
			wantOutput: []string{
				noRedirect,
				"Could not get the latest release from the GitHub API: HTTP 403",
				"set GITHUB_TOKEN to use a token",
				"ERROR: Could not determine CLI version",
			},
		},
		{
			name:           "API error",
			redirectStatus: http.StatusOK,
			apiStatus:      http.StatusInternalServerError,
			wantRedirects:  1,
			wantAPIAuth:    []string{""},
			wantOutput: []string{
				"Could not get the latest release from the GitHub API: HTTP 500",
				"ERROR: Could not determine CLI version",
			},
			dontOutput: []string{"set GITHUB_TOKEN"},
		},
		{
			name:           "API error with a token is not retried",
			env:            []string{"GITHUB_TOKEN=valid-token", "GH_FAKE_TOKEN=gh-token"},
			redirectStatus: http.StatusOK,
			apiStatus:      http.StatusBadGateway,
			wantRedirects:  1,
			wantAPIAuth:    []string{"Bearer valid-token"},
			wantOutput: []string{
				"Could not get the latest release from the GitHub API: HTTP 502",
				"ERROR: Could not determine CLI version",
			},
			dontOutput: []string{"set GITHUB_TOKEN"},
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
			g.redirectStatus = c.redirectStatus
			g.apiStatus = c.apiStatus

			r := runInstaller(t, g, c.env, c.homeFiles, c.noJQ)
			if c.wantVersion == "" {
				assert.Error(t, r.err)
				assert.NoFileExists(t, r.binPath)
				assert.Empty(t, g.authHeaders(downloadPath))
			} else {
				assertInstalled(t, r, c.wantVersion)
				assert.Equal(t, []string{c.wantDownloadAuth}, g.authHeaders(downloadPath), "download")
			}
			for _, s := range c.wantOutput {
				assert.Contains(t, r.output, s)
			}
			for _, s := range c.dontOutput {
				assert.NotContains(t, r.output, s)
			}
			redirectAuth := g.authHeaders(redirectPath)
			assert.Len(t, redirectAuth, c.wantRedirects, "latest release page requests")
			for _, auth := range redirectAuth {
				assert.Empty(t, auth, "the latest release page is requested without a token")
			}
			assert.Equal(t, c.wantAPIAuth, g.authHeaders(apiPath), "API requests")
		})
	}
}
