// Package alt manages instances of alternative CLI configurations.
package alt

import (
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Alt struct {
	executablePath string
	comment        string
	targetName     string
	targetPath     string

	configPath string
	configNode *yaml.Node
}

// New prepares an alt CLI instance. Its executable runs targetPath if that exists, or targetName from the PATH.
func New(execPath, comment, targetName, targetPath, configPath string, configNode *yaml.Node) *Alt {
	return &Alt{
		executablePath: execPath,
		comment:        comment,
		targetName:     targetName,
		targetPath:     targetPath,
		configPath:     configPath,
		configNode:     configNode,
	}
}

// GenerateAndSave creates and saves the executable and config files needed for the alt CLI instance.
func (a *Alt) GenerateAndSave() error {
	configContent, err := yaml.Marshal(a.configNode)
	if err != nil {
		return err
	}
	if err := writeFile(a.configPath, configContent, 0o755, 0o644); err != nil {
		return err
	}
	executableContent := a.generateExecutable(runtime.GOOS)
	return writeFile(a.executablePath, []byte(executableContent), 0o755, 0o755)
}

// generateExecutable returns a script that runs the target with the config file.
// Paths are absolute, so the script works where HOME or XDG_CONFIG_HOME differ (e.g. cron or CI).
func (a *Alt) generateExecutable(goos string) string {
	if goos == "windows" {
		s := ":: " + a.comment + "\r\n" +
			"@echo off\r\n" +
			"setlocal\r\n" +
			`set "CLI_CONFIG_FILE=` + batchEscape(a.configPath) + `"` + "\r\n"
		if a.targetPath != "" {
			target := `"` + batchEscape(a.targetPath) + `"`
			s += "if not exist " + target + " goto fallback\r\n" +
				target + " %*\r\n" +
				"exit /b %ERRORLEVEL%\r\n" +
				":fallback\r\n"
		}
		return s + a.targetName + " %*\r\n"
	}

	s := "#!/bin/sh\n" +
		"# " + a.comment + "\n" +
		"export CLI_CONFIG_FILE=" + shellQuote(a.configPath) + "\n"
	if a.targetPath != "" {
		target := shellQuote(a.targetPath)
		s += "[ -x " + target + " ] && exec " + target + ` "$@"` + "\n"
	}
	return s + "exec " + shellQuote(a.targetName) + ` "$@"` + "\n"
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// shellQuote quotes s for a POSIX shell, if needed.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// batchEscape escapes s for use inside double quotes in a batch file.
func batchEscape(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

func GetExecutableFileExtension() string {
	if runtime.GOOS == "windows" {
		return ".bat"
	}
	return ""
}
