package commands

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/legacy"
)

func TestExpandAbbreviation(t *testing.T) {
	root := &cobra.Command{Use: "upsun"}
	root.PersistentFlags().BoolP("verbose", "v", false, "")
	root.PersistentFlags().BoolP("yes", "y", false, "")
	root.PersistentFlags().String("context", "", "")
	root.AddCommand(
		&cobra.Command{Use: "init", Aliases: []string{"project:init", "ify"}},
		&cobra.Command{Use: "project:convert", Aliases: []string{"convert"}},
		&cobra.Command{Use: "app:config-validate", Aliases: []string{"validate", "lint"}},
		&cobra.Command{Use: "list"},
		&cobra.Command{Use: "version"},
		&cobra.Command{Use: "_complete", Hidden: true},
	)
	legacyCmds := []legacy.Command{
		{Name: "list"},
		{Name: "project:info", Aliases: []string{"pinfo"}},
		{Name: "project:create", Aliases: []string{"create"}},
		{Name: "project:curl", Hidden: true},
		{Name: "app:list", Aliases: []string{"apps"}},
		{Name: "app:config-get"},
		{Name: "integration:list"},
		{Name: "version:list", Aliases: []string{"versions"}, Hidden: true},
	}

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"namespace abbreviation", []string{"p:init"}, []string{"init"}},
		{"both parts abbreviated", []string{"pro:ini", "--yes"}, []string{"init", "--yes"}},
		{"case-insensitive fallback", []string{"P:Init"}, []string{"init"}},
		{"after flags", []string{"-v", "--yes", "-vy", "p:conv"}, []string{"-v", "--yes", "-vy", "project:convert"}},
		{"after a flag with a value", []string{"--context", "p:init", "init"}, nil},
		{"after a flag with a separate value", []string{"--context", "foo", "p:init"}, nil},
		{"after an unknown flag", []string{"--foo", "p:init"}, nil},
		{"after a shorthand with a value", []string{"-vc", "p:init"}, nil},
		{"multi-word part", []string{"a:config-v"}, []string{"app:config-validate"}},
		{"unique abbreviation", []string{"p:con"}, []string{"project:convert"}},
		{"ambiguous with a hidden legacy command", []string{"ver"}, nil},
		{"after help flag", []string{"--help", "p:init"}, []string{"--help", "init"}},
		{"ambiguous with a legacy command", []string{"p:c"}, nil},
		{"ambiguous between legacy commands", []string{"a:c"}, nil},
		{"prefix of a longer legacy name", []string{"in"}, nil},
		{"legacy command", []string{"p:info"}, nil},
		{"exact native command", []string{"init"}, nil},
		{"exact legacy command", []string{"pinfo"}, nil},
		{"unknown command", []string{"p:nope"}, nil},
		{"hidden native command", []string{"_comp"}, nil},
		{"no command", []string{"--version"}, nil},
		{"after double dash", []string{"--", "p:init"}, nil},
		{"empty", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok, err := expandAbbreviation(root, func() ([]legacy.Command, error) { return legacyCmds, nil }, c.args)
			assert.NoError(t, err)
			if c.want == nil {
				assert.False(t, ok)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, c.want, got)
		})
	}
}
