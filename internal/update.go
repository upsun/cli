package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/symfony-cli/terminal"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/state"
	"github.com/upsun/cli/internal/version"
)

// ReleaseInfo stores information about a release
type ReleaseInfo struct {
	Version     string    `json:"tag_name"`
	URL         string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// updateRequestTimeout limits the request to GitHub for the latest release.
const updateRequestTimeout = 5 * time.Second

// UpdateCheck is a background check for a newer release.
type UpdateCheck struct {
	done chan struct{}
}

// StartUpdateCheck starts a background check for a newer release on GitHub,
// if one is due. The result is cached in the state, for PendingNotification
// to read in a later invocation. It returns nil if no check was started.
func StartUpdateCheck(cnf *config.Config) *UpdateCheck {
	if !shouldCheckForUpdate(cnf) {
		return nil
	}
	s, err := state.Load(cnf)
	if err == nil && time.Now().Unix()-s.Updates.LastChecked < int64(cnf.Updates.CheckInterval) {
		// Updates were already checked recently.
		return nil
	}
	// Record the attempt first, so a request that outlives the process is not retried on every run.
	// If the state cannot be saved, the result could not be cached either.
	if err := state.Update(cnf, func(s *state.State) {
		s.Updates.LastChecked = time.Now().Unix()
	}); err != nil {
		return nil
	}
	c := &UpdateCheck{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		bin := cnf.Embedded()
		releaseInfo, err := getLatestReleaseInfo(bin.Wrapper.GitHubRepo)
		if err != nil || releaseInfo.Version == "" {
			return
		}
		// A release reaches winget once Microsoft merges its manifest, hours or days later.
		if pkg := bin.Wrapper.WingetPackage; pkg != "" && IsWinget(cnf) {
			client := &http.Client{Timeout: updateRequestTimeout}
			if ok, err := inWinget(client, wingetManifestURL(wingetManifestsURL, pkg, releaseInfo.Version)); err != nil || !ok {
				return
			}
		}
		//nolint:errcheck // not being able to set the state should have no impact on the rest of the program
		state.Update(cnf, func(s *state.State) {
			s.Updates.KnownLatestVersion = releaseInfo.Version
		})
	}()
	return c
}

// Wait waits for the check to finish, up to the given timeout.
func (c *UpdateCheck) Wait(timeout time.Duration) {
	if c == nil {
		return
	}
	select {
	case <-c.done:
	case <-time.After(timeout):
	}
}

// notifyInterval is the minimum time between showing update messages.
const notifyInterval = 7 * 24 * 60 * 60 // one week, in seconds

// PendingNotification returns a release to tell the user about, based on the
// version cached by a previous background check. It returns nil when there is
// nothing to show, when the weekly throttle has not elapsed, or when the
// environment is not eligible. The caller should print the result and then call
// MarkNotified.
func PendingNotification(cnf *config.Config, currentVersion string) *ReleaseInfo {
	if !shouldCheckForUpdate(cnf) {
		return nil
	}
	s, err := state.Load(cnf)
	if err != nil {
		return nil
	}
	return notificationFromState(s, cnf.Embedded().Wrapper.GitHubRepo, currentVersion, time.Now().Unix())
}

// notificationFromState decides whether to notify about the cached latest
// version, given the current time. It is the pure core of PendingNotification.
func notificationFromState(s state.State, repo, currentVersion string, now int64) *ReleaseInfo {
	if s.Updates.KnownLatestVersion == "" {
		return nil
	}
	// A LastNotified in the future (clock skew) is treated as stale.
	if s.Updates.LastNotified != 0 && s.Updates.LastNotified <= now && now-s.Updates.LastNotified < notifyInterval {
		return nil
	}
	cmp, err := version.Compare(s.Updates.KnownLatestVersion, currentVersion)
	if err != nil || cmp <= 0 {
		return nil
	}
	return &ReleaseInfo{
		Version: s.Updates.KnownLatestVersion,
		URL:     fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, s.Updates.KnownLatestVersion),
	}
}

// MarkNotified records that an update message was just shown, so it is not
// repeated until the next notify interval.
func MarkNotified(cnf *config.Config) {
	//nolint:errcheck // failing to save state should not affect the rest of the program
	state.Update(cnf, func(s *state.State) {
		s.Updates.LastNotified = time.Now().Unix()
	})
}

// shouldCheckForUpdate checks updates are not disabled and the environment is a terminal.
// The release repository comes from the embedded config, as the binary is the same with CLI_CONFIG_FILE.
func shouldCheckForUpdate(cnf *config.Config) bool {
	return config.Version != "0.0.0" &&
		cnf.Embedded().Wrapper.GitHubRepo != "" &&
		cnf.Updates.Check &&
		os.Getenv(cnf.Application.EnvPrefix+"UPDATES_CHECK") != "0" &&
		!isCI() && !IsAutoUpdating(cnf) &&
		terminal.IsTerminal(os.Stdout) && terminal.IsTerminal(os.Stderr)
}

func isCI() bool {
	return os.Getenv("CI") != "" || // GitHub Actions, Travis CI, CircleCI, Cirrus CI, GitLab CI, AppVeyor, CodeShip, dsari
		os.Getenv("BUILD_NUMBER") != "" || // Jenkins, TeamCity
		os.Getenv("RUN_ID") != "" // TaskCluster, dsari
}

// getLatestReleaseInfo from GitHub
func getLatestReleaseInfo(repo string) (*ReleaseInfo, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), http.NoBody)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: updateRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var latestRelease ReleaseInfo
	if err := json.Unmarshal(body, &latestRelease); err != nil {
		return nil, err
	}

	return &latestRelease, nil
}

// wingetManifestsURL is the manifests directory of the winget-pkgs repository.
const wingetManifestsURL = "https://raw.githubusercontent.com/microsoft/winget-pkgs/master/manifests"

// wingetManifestURL returns the URL of a package version's manifest in winget-pkgs,
// e.g. manifests/u/Upsun/CLI/5.14.0/Upsun.CLI.yaml.
func wingetManifestURL(base, pkg, ver string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s.yaml",
		base, strings.ToLower(pkg[:1]), strings.ReplaceAll(pkg, ".", "/"), strings.TrimPrefix(ver, "v"), pkg)
}

// inWinget reports whether the manifest at url exists.
func inWinget(client *http.Client, url string) (bool, error) {
	req, err := http.NewRequest(http.MethodHead, url, http.NoBody)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("unexpected status: %s", resp.Status)
}
