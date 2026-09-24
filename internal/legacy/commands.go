package legacy

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// commandIndex is the legacy CLI's "list --all --format=json" output, generated at build time with all commands enabled.
//
//go:embed archives/commands.json
var commandIndex []byte

// Command describes a legacy CLI command.
type Command struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	// HiddenAliases only work in full: they are not matched by abbreviations.
	HiddenAliases []string `json:"hidden_aliases"`
	Hidden        bool     `json:"hidden"`
}

// Commands returns every legacy command, regardless of whether it is enabled by config.
var Commands = sync.OnceValues(func() ([]Command, error) {
	var list struct {
		Commands map[string]Command `json:"commands"`
	}
	if err := json.Unmarshal(commandIndex, &list); err != nil {
		return nil, err
	}
	cmds := make([]Command, 0, len(list.Commands))
	for _, c := range list.Commands {
		cmds = append(cmds, c)
	}
	return cmds, nil
})
