package lint

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func CheckScripts(cfg *Config) *Result {
	result := &Result{}

	var scripts = make(map[string]string)
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		keyPrefix := "applications." + appName + "."

		// Group all scripts for shell syntax checking.
		scripts[keyPrefix+"hooks.build"] = app.Hooks.Build
		scripts[keyPrefix+"hooks.deploy"] = app.Hooks.Deploy
		scripts[keyPrefix+"hooks.post_deploy"] = app.Hooks.PostDeploy
		scripts[keyPrefix+"web.commands.start"] = app.Web.Commands.Start
		scripts[keyPrefix+"web.commands.post_start"] = app.Web.Commands.PostStart
		for cronName, cron := range app.Crons {
			cronPrefix := keyPrefix + "crons." + cronName + "."
			scripts[cronPrefix+"start"] = cron.Commands.Start
			scripts[cronPrefix+"stop"] = cron.Commands.Stop
		}
		for workerName, worker := range app.Workers {
			workerPrefix := keyPrefix + "workers." + workerName + ".commands."
			scripts[workerPrefix+"pre_start"] = worker.Commands.PreStart
			scripts[workerPrefix+"start"] = worker.Commands.Start
			scripts[workerPrefix+"post_start"] = worker.Commands.PostStart
		}
	}

	for taskName := range cfg.Tasks {
		task := cfg.Tasks[taskName]
		keyPrefix := "tasks." + taskName + "."
		scripts[keyPrefix+"run.command"] = task.Run.Command
		scripts[keyPrefix+"hooks.build"] = task.Hooks.Build
		scripts[keyPrefix+"hooks.deploy"] = task.Hooks.Deploy
	}

	for k, v := range scripts {
		if v == "" {
			continue
		}
		r := strings.NewReader(v)
		if _, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, ""); err != nil {
			result.AddError(k, fmt.Sprintf("invalid syntax: %s", err))
		}
	}

	return result
}
