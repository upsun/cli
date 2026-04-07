// commands/auth/ssh.go
package auth

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
	"github.com/upsun/cli/internal/session"
)

// delegateSSHFinalization calls the legacy PHP CLI to run post-login SSH setup.
// This is best-effort — errors are ignored.
func delegateSSHFinalization(ctx context.Context, cfg *config.Config, cmd *cobra.Command) error {
	wrapper := &legacy.CLIWrapper{
		Config:             cfg,
		Version:            config.Version,
		DisableInteraction: true,
		Stdout:             io.Discard,
		Stderr:             cmd.ErrOrStderr(),
		Stdin:              cmd.InOrStdin(),
	}
	// Inject auth so PHP can authenticate without credential helper.
	if mgr, err := session.New(cfg); err == nil {
		if apiToken, err := mgr.GetAPIToken(); err == nil && apiToken != "" {
			wrapper.ExtraEnv = append(wrapper.ExtraEnv, cfg.Application.EnvPrefix+"TOKEN="+apiToken)
		} else if s, err := mgr.Load(); err == nil && s != nil && s.AccessToken != "" {
			wrapper.ExtraEnv = append(wrapper.ExtraEnv, cfg.Application.EnvPrefix+"API_TOKEN="+s.AccessToken)
		}
	}
	_ = wrapper.Exec(ctx, "ssh-cert:load", "--no-interaction")
	return nil
}
