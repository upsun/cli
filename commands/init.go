package commands

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/upsun/whatsun/pkg/digest"
	"github.com/upsun/whatsun/pkg/files"
	"gopkg.in/yaml.v3"

	"github.com/upsun/cli/internal/legacy"

	"github.com/upsun/cli/internal/api"
	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	_init "github.com/upsun/cli/internal/init"
)

func newInitCommand(cnf *config.Config) *cobra.Command {
	var (
		useAI       bool
		initOptions = &_init.Options{}
	)
	cmd := &cobra.Command{
		Use:     "init [flags]",
		Aliases: []string{"project:init", "ify"},
		Short:   "Generate configuration for a project",
		Long:    initCommandHelp(cnf),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitCommand(cmd, args, useAI, initOptions)
		},
	}

	cmd.Flags().BoolVar(&useAI, "ai", false, "Use AI configuration")
	_ = cmd.Flags().MarkDeprecated("ai", "AI is always used: use --yes to skip the confirmation")
	cmd.Flags().StringVar(&initOptions.ExtraContext, "context", "",
		"Add extra context for AI configuration")
	cmd.Flags().BoolVar(&initOptions.OnlyShowDigest, "digest", false,
		"Only show the repository digest (the AI configuration input), without sending it")

	cmd.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		internalCmd := innerProjectInitCommand(cnf)
		fmt.Println(internalCmd.HelpPage(cnf))
	})

	return cmd
}

func initCommandHelp(cnf *config.Config) string {
	var s strings.Builder

	bold := color.New(color.Bold)
	fmt.Fprintf(&s,
		"This command will generate a %s that you can build on.\n", bold.Sprint("starter configuration"))
	fmt.Fprintln(&s, "It sends a sanitized repository digest to OpenAI for automated analysis.")

	fmt.Fprintln(&s, "You can review the digest at any time by running:",
		color.GreenString(cnf.Application.Executable+" init --digest"))

	return s.String()
}

// runInitCommand handles the main logic for the init command.
func runInitCommand(
	cmd *cobra.Command, args []string, useAI bool, initOptions *_init.Options,
) error {
	var path = "."
	if len(args) > 0 {
		path = args[0]
	}
	gitRoot, err := findGitRoot(path)
	if err != nil {
		return err
	}
	if gitRoot == "" {
		// TODO suggest creating a repository
		return fmt.Errorf("this command can only be run inside a Git repository")
	}

	if initOptions.OnlyShowDigest {
		dg, err := generateDigest(cmd.Context(), gitRoot)
		if err != nil {
			return err
		}
		return yaml.NewEncoder(cmd.OutOrStdout()).Encode(dg)
	}

	if cmd.Flags().Changed("ai") && !useAI {
		return fmt.Errorf("configuration without AI is no longer available")
	}

	cnf := config.FromContext(cmd.Context())
	if !cnf.API.EnableOrganizations {
		return fmt.Errorf("this command requires organizations to be enabled")
	}

	ia, err := newInitAuth(cmd, cnf)
	if err != nil {
		return err
	}

	var stderr = cmd.ErrOrStderr()

	fmt.Fprintln(stderr, color.CyanString("Creating %s configuration", cnf.Service.Name))
	fmt.Fprintln(stderr)

	var interactive = isInteractive(cmd)

	debugLogf("Checking selected organization")
	var org *api.Organization
	if err := ia.withLogin(func() (err error) {
		org, err = handleOrganizations(cmd.Context(), cnf, ia.apiURL, ia.httpClient, ia.ensureAuthenticated, initOptions)
		return err
	}); err != nil {
		return err
	}
	if org != nil {
		debugLogf("Selected organization: %s (%s)", org.Label, org.ID)
		if initOptions.ProjectID != "" {
			debugLogf("Selected project: %s", initOptions.ProjectID)
		}
	}

	if org != nil && org.Type != api.OrgTypeFlexible {
		fmt.Fprintf(stderr, "Selected organization: %s (%s)\n", color.YellowString(org.Label), color.YellowString(org.ID))
		fmt.Fprintln(stderr, color.YellowString(
			"Note: AI configuration is only compatible with `%s` organizations\n", api.OrgTypeFlexible))
	}

	// Ask for consent to send the digest, unless given by --yes (or the deprecated --ai).
	if !useAI && !viper.GetBool("yes") {
		if !interactive {
			return fmt.Errorf("use --yes to confirm in non-interactive mode")
		}
		fmt.Fprintln(stderr, initCommandHelp(cnf))
		ok, err := confirm(cmd, "Do you want to continue?", false)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		fmt.Fprintln(stderr)
	}

	if err := ia.ensureAuthenticated(); err != nil {
		return err
	}

	debugLogf("Analyzing repository")
	dg, err := generateDigest(cmd.Context(), gitRoot)
	if err != nil {
		return err
	}

	initOptions.HTTPClient = ia.httpClient
	initOptions.APIURL = ia.apiURL
	initOptions.UserAgent = cnf.UserAgent()
	initOptions.IsInteractive = interactive
	initOptions.Yes = viper.GetBool("yes")
	initOptions.IsDebug = viper.GetBool("debug")
	initOptions.DebugLogFunc = debugLogf

	return _init.RunAIConfig(cmd.Context(), cnf, dg, gitRoot, initOptions, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

// initAuth provides authentication for init, using Go auth or the legacy CLI.
type initAuth struct {
	httpClient          *http.Client
	apiURL              string
	ensureAuthenticated func() error
	// withLogin runs a function, and with Go auth, offers a login and runs it again if login is required.
	withLogin func(fn func() error) error
}

func newInitAuth(cmd *cobra.Command, cnf *config.Config) (*initAuth, error) {
	if !cnf.GoAuthEnabled() {
		legacyCLIClient, err := auth.NewLegacyCLIClient(cmd.Context(),
			makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin()))
		if err != nil {
			return nil, err
		}
		return &initAuth{
			httpClient:          legacyCLIClient.HTTPClient,
			apiURL:              cnf.API.BaseURL,
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
	return &initAuth{
		httpClient:          auth.NewClient(m, auth.NewHTTPClient(cnf, m.Settings).Transport),
		apiURL:              m.Settings.BaseURL,
		ensureAuthenticated: func() error { return withLoginFn(ensureAuthenticated) },
		withLogin:           withLoginFn,
	}, nil
}

// handleOrganizations manages organization selection and validation.
// It modifies initOptions.OrganizationID and initOptions.ProjectID.
func handleOrganizations(
	ctx context.Context,
	cnf *config.Config,
	apiURL string,
	httpClient *http.Client,
	ensureAuthenticated func() error,
	initOptions *_init.Options,
) (*api.Organization, error) {
	if !cnf.API.EnableOrganizations {
		return nil, nil
	}

	apiClient, err := api.NewClient(apiURL, httpClient)
	if err != nil {
		return nil, err
	}

	silentLegacyWrapper := makeLegacyCLIWrapper(cnf, nil, nil, nil)
	currentOrgID, currentProjectID := getCurrentOrganizationAndProjectID(ctx, silentLegacyWrapper)

	if currentOrgID == "" {
		return nil, nil
	}

	if err := ensureAuthenticated(); err != nil {
		return nil, err
	}

	org, err := apiClient.GetOrganization(ctx, currentOrgID)
	if err != nil {
		return nil, err
	}

	initOptions.OrganizationID = currentOrgID
	initOptions.ProjectID = currentProjectID
	return org, nil
}

// findGitRoot finds the closest parent directory containing a .git folder.
// Returns an absolute path to the git root, or empty string if not in a git repo.
func findGitRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the root directory.
			return "", nil
		}
		dir = parent
	}
}

// generateDigest creates a digest of the project files.
func generateDigest(ctx context.Context, path string) (*digest.Digest, error) {
	fsys, err := files.LocalFS(path)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	digestConfig, err := _init.DefaultDigestConfig()
	if err != nil {
		return nil, err
	}

	digester, err := digest.NewDigester(fsys, digestConfig)
	if err != nil {
		return nil, err
	}

	return digester.GetDigest(ctx)
}

func getCurrentOrganizationAndProjectID(ctx context.Context, wrapper *legacy.CLIWrapper) (orgID, projectID string) {
	var buf = &bytes.Buffer{}
	wrapper.Stderr = nil
	wrapper.Stdout = buf
	wrapper.Stdin = nil

	if err := wrapper.Exec(ctx, "project:info", "id", "--no-interaction"); err != nil {
		return
	}
	projectID = strings.TrimSpace(buf.String())
	buf.Reset()

	if err := wrapper.Exec(ctx, "organization:info", "id", "--no-interaction"); err != nil {
		return
	}
	orgID = strings.TrimSpace(buf.String())

	return
}
