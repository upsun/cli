package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// CheckForUpdate checks whether this software has had a newer release on GitHub
func CheckForUpdate(cnf *config.Config, currentVersion string) (*ReleaseInfo, error) {
	if !shouldCheckForUpdate(cnf) {
		return nil, nil
	}

	s, err := state.Load(cnf)
	if err == nil && time.Now().Unix()-s.Updates.LastChecked < int64(cnf.Updates.CheckInterval) {
		// Updates were already checked recently.
		return nil, nil
	}

	defer func() {
		// After checking, save the last check time.
		s.Updates.LastChecked = time.Now().Unix()
		//nolint:errcheck // not being able to set the state should have no impact on the rest of the program
		state.Save(s, cnf)
	}()

	releaseInfo, err := getLatestReleaseInfo(cnf.Wrapper.GitHubRepo)
	if err != nil {
		return nil, fmt.Errorf("could not determine latest release: %w", err)
	}

	// Cache the latest known version so the next invocation can show a message
	// before its command runs, without blocking on the network.
	s.Updates.KnownLatestVersion = releaseInfo.Version

	cmp, err := version.Compare(releaseInfo.Version, currentVersion)
	if err != nil {
		return nil, fmt.Errorf("could not compare versions: %w", err)
	}
	if cmp > 0 {
		return releaseInfo, nil
	}

	return nil, nil
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
	return notificationFromState(s, cnf.Wrapper.GitHubRepo, currentVersion, time.Now().Unix())
}

// notificationFromState decides whether to notify about the cached latest
// version, given the current time. It is the pure core of PendingNotification.
func notificationFromState(s state.State, repo, currentVersion string, now int64) *ReleaseInfo {
	if s.Updates.KnownLatestVersion == "" {
		return nil
	}
	if s.Updates.LastNotified != 0 && now-s.Updates.LastNotified < notifyInterval {
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
	s, err := state.Load(cnf)
	if err != nil {
		return
	}
	s.Updates.LastNotified = time.Now().Unix()
	//nolint:errcheck // failing to save state should not affect the rest of the program
	state.Save(s, cnf)
}

// shouldCheckForUpdate checks updates are not disabled and the environment is a terminal
func shouldCheckForUpdate(cnf *config.Config) bool {
	return config.Version != "0.0.0" &&
		cnf.Wrapper.GitHubRepo != "" &&
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

	resp, err := http.DefaultClient.Do(req)
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
