package telemetry

// trackedCommands defines which commands send telemetry.
var trackedCommands = map[string]bool{
	// Native Go commands
	"init":         true,
	"project:init": true,

	// Important legacy commands
	"project:create":     true,
	"environment:branch": true,
	"project:delete":     true,
	"environment:delete": true,
	"mount:upload":       true,
	"mount:download":     true,

	// Testing commands, to be removed
	"project:list": true,
}

// IsTracked returns true if the command should send telemetry.
func IsTracked(command string) bool {
	return trackedCommands[command]
}

// ExtractCommand extracts the command name from arguments.
func ExtractCommand(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	// Return first arg (command name, no flags)
	return args[0]
}

