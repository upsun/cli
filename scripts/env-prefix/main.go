// Command env-prefix prints the application's environment variable prefix from a CLI config file.
package main

import (
	"fmt"
	"os"

	"github.com/upsun/cli/internal/config"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: env-prefix <config-file>")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1]) //nolint:gosec // The path is the build's own config file.
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cnf, err := config.FromYAML(b)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(cnf.Application.EnvPrefix)
}
