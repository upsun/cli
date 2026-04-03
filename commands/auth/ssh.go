// commands/auth/ssh.go
package auth

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
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
	_ = wrapper.Exec(ctx, "ssh-cert:load", "--no-interaction")
	return nil
}
