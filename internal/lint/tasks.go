package lint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// authorizationActions lists the actions allowed for each authorization type.
var authorizationActions = map[string]string{
	"task": "operate",
	"env":  "view",
}

// CheckTasks checks task definitions and the authorizations of applications,
// their web and worker containers, and tasks.
func CheckTasks(cfg *Config) *Result {
	result := &Result{}

	for name := range cfg.Tasks {
		task := cfg.Tasks[name]
		path := "tasks." + name
		if task.Base != "" {
			if len(task.fields) > 1 {
				result.AddError(path+".base", "when 'base' is set, no other property is allowed")
			}
			continue
		}
		if task.Run.Command == "" {
			result.AddError(path+".run.command", "a run command is required")
		}
		for mountName, mount := range task.Mounts {
			mountPath := path + ".mounts." + mountName
			switch {
			case mount.Source == "storage" && mount.Service == "":
				result.AddError(mountPath, "a task has no storage of its own, so a 'storage' mount must "+
					"name an application in 'service'")
			case mount.Source == "storage":
				if _, ok := cfg.Applications[mount.Service]; !ok {
					result.AddError(mountPath+".service", fmt.Sprintf("application '%s' is not found", mount.Service))
				}
			case mount.Source == "service":
				if _, ok := cfg.Services[mount.Service]; !ok {
					result.AddError(mountPath+".service", fmt.Sprintf("service '%s' is not found", mount.Service))
				}
			}
		}
	}

	checkAuthorizations := func(path string, authorizations []Authorization) {
		for i, a := range authorizations {
			itemPath := path + ".authorizations." + strconv.Itoa(i)
			if want, ok := authorizationActions[a.Type]; ok && a.Action != want {
				result.AddError(itemPath+".action",
					fmt.Sprintf("authorization type '%s' only allows the action '%s'", a.Type, want))
			}
			if a.Type != "task" {
				continue
			}
			if a.Resource == "" {
				result.AddError(itemPath+".resource", "a task authorization requires a resource (the task name)")
			} else if _, ok := cfg.Tasks[a.Resource]; !ok {
				result.AddError(itemPath+".resource", fmt.Sprintf("task '%s' is not found%s",
					a.Resource, availableHint(cfg.Tasks)))
			}
		}
	}
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		path := "applications." + appName
		checkAuthorizations(path, app.Authorizations)
		checkAuthorizations(path+".web", app.Web.Authorizations)
		for workerName, worker := range app.Workers {
			checkAuthorizations(path+".workers."+workerName, worker.Authorizations)
		}
	}
	for name := range cfg.Tasks {
		checkAuthorizations("tasks."+name, cfg.Tasks[name].Authorizations)
	}

	return result
}

// availableHint lists the defined task names for an error message.
func availableHint(tasks map[string]Task) string {
	if len(tasks) == 0 {
		return " (no tasks are defined)"
	}
	return " (defined tasks: " + strings.Join(slices.Sorted(maps.Keys(tasks)), ", ") + ")"
}
