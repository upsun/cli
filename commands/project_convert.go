package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/convert"
)

// newProjectConvertCommand creates the cobra command for converting config.
func newProjectConvertCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project:convert",
		Short:   "Generate an Upsun compatible configuration based on the configuration from another provider.",
		Aliases: []string{"convert"},
		Example: "  # Convert the Platform.sh project configuration files in your current directory\n" +
			"  " + cnf.Application.Executable + " convert --provider=platformsh",
		RunE: runProjectConvert,
	}

	cmd.Flags().StringP(
		"provider",
		"p",
		"platformsh",
		"The provider from which to convert the configuration. Currently, only 'platformsh' is supported.",
	)

	_ = viper.BindPFlag("provider", cmd.Flags().Lookup("provider"))
	return cmd
}

// runProjectConvert is the entry point for the convert config command.
func runProjectConvert(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}

	if viper.GetString("provider") == "platformsh" {
		return convert.PlatformshToUpsun(cwd, cmd.ErrOrStderr())
	}

	return fmt.Errorf("only the 'platformsh' provider is currently supported")
}
