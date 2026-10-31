package commands

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

// apiAuth authenticates a Go command's API requests, with Go auth or the legacy CLI.
// Run requests inside withLogin: any request can get a step-up authentication challenge.
type apiAuth struct {
	httpClient *http.Client
	// apiURL comes from the auth settings, which apply the user's config file and environment.
	apiURL string
	// ensureAuthenticated gets a token, offering a login with Go auth.
	ensureAuthenticated func() error
	// withLogin runs fn, and with Go auth, offers a login and runs it again if login is required.
	withLogin func(fn func() error) error
}

func newAPIAuth(cmd *cobra.Command, cnf *config.Config) (*apiAuth, error) {
	if !cnf.GoAuthEnabled() {
		settings, err := cnf.Auth()
		if err != nil {
			return nil, err
		}
		legacyCLIClient, err := auth.NewLegacyCLIClient(cmd.Context(),
			makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin()))
		if err != nil {
			return nil, err
		}
		return &apiAuth{
			httpClient:          legacyCLIClient.HTTPClient,
			apiURL:              settings.BaseURL,
			ensureAuthenticated: func() error { return legacyCLIClient.EnsureAuthenticated(cmd.Context()) },
			withLogin:           func(fn func() error) error { return fn() },
		}, nil
	}
	m, err := newAuthManager(cnf, cmd.ErrOrStderr())
	if err != nil {
		return nil, err
	}
	withLoginFn := func(fn func() error) error { return withLogin(cmd, cnf, m, fn) }
	ensureAuthenticated := func() error { return m.EnsureAuthenticated(cmd.Context()) }
	return &apiAuth{
		httpClient:          auth.NewClient(m, auth.NewHTTPClient(cnf, m.Settings).Transport),
		apiURL:              m.Settings.BaseURL,
		ensureAuthenticated: func() error { return withLoginFn(ensureAuthenticated) },
		withLogin:           withLoginFn,
	}, nil
}
