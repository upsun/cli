package lint

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Applications map[string]struct {
		Type  string `yaml:"type"`
		Stack any    `yaml:"stack,omitempty"` // The stack can be an object, string, or array.

		Hooks struct {
			Build      string `yaml:"build,omitempty"`
			Deploy     string `yaml:"deploy,omitempty"`
			PostDeploy string `yaml:"post_deploy,omitempty"`
		} `yaml:"hooks,omitempty"`

		Authorizations []Authorization `yaml:"authorizations,omitempty"`

		Web struct {
			Authorizations []Authorization `yaml:"authorizations,omitempty"`

			Commands struct {
				Start     string `yaml:"start,omitempty"`
				PostStart string `yaml:"post_start,omitempty"`
			} `yaml:"commands,omitempty"`

			Locations map[string]WebLocation `yaml:"locations,omitempty"`
		} `yaml:"web,omitempty"`

		Relationships map[string]any   `yaml:"relationships,omitempty"`
		Mounts        map[string]Mount `yaml:"mounts,omitempty"`

		Crons map[string]struct {
			Commands struct {
				Start string `yaml:"start,omitempty"`
				Stop  string `yaml:"stop,omitempty"`
			} `yaml:"commands,omitempty"`
		} `yaml:"crons,omitempty"`

		Workers map[string]struct {
			Type           string           `yaml:"type,omitempty"`
			Authorizations []Authorization  `yaml:"authorizations,omitempty"`
			Mounts         map[string]Mount `yaml:"mounts,omitempty"`

			Commands struct {
				PreStart  string `yaml:"pre_start,omitempty"`
				Start     string `yaml:"start,omitempty"`
				PostStart string `yaml:"post_start,omitempty"` // Flex only.
			} `yaml:"commands,omitempty"`
		} `yaml:"workers,omitempty"`

		Dependencies map[string]map[string]any `yaml:"dependencies,omitempty"`
	} `yaml:"applications"`

	Services map[string]struct {
		Type string `yaml:"type,omitempty"`
	} `yaml:"services,omitempty"`

	Routes map[string]struct {
		Type     string `yaml:"type,omitempty"`
		Upstream string `yaml:"upstream,omitempty"`
		To       string `yaml:"to,omitempty"`
	} `yaml:"routes,omitempty"`

	Tasks map[string]Task `yaml:"tasks,omitempty"`
}

// WebLocation configures how requests under a web location are served.
type WebLocation struct {
	Root  string         `yaml:"root,omitempty"`
	Rules map[string]any `yaml:"rules,omitempty"`
}

// Task is an on-demand, run-to-completion workload (Flex only).
type Task struct {
	Type  string `yaml:"type,omitempty"`
	Base  string `yaml:"base,omitempty"`
	Stack any    `yaml:"stack,omitempty"`

	Run struct {
		Command string `yaml:"command,omitempty"`
	} `yaml:"run,omitempty"`

	Hooks struct {
		Build  string `yaml:"build,omitempty"`
		Deploy string `yaml:"deploy,omitempty"`
	} `yaml:"hooks,omitempty"`

	Relationships  map[string]any   `yaml:"relationships,omitempty"`
	Mounts         map[string]Mount `yaml:"mounts,omitempty"`
	Authorizations []Authorization  `yaml:"authorizations,omitempty"`

	// fields lists the keys set on the task, to check that "base" is used alone.
	fields []string
}

// UnmarshalYAML decodes a task and records which keys it sets.
func (t *Task) UnmarshalYAML(node *yaml.Node) error {
	type plain Task
	if err := node.Decode((*plain)(t)); err != nil {
		return err
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		t.fields = append(t.fields, node.Content[i].Value)
	}
	return nil
}

// Mount is a writable directory in a container.
type Mount struct {
	Source  string `yaml:"source,omitempty"`
	Service string `yaml:"service,omitempty"`
}

// Authorization grants a workload access to the Upsun API at runtime.
type Authorization struct {
	Type     string `yaml:"type"`
	Action   string `yaml:"action"`
	Resource string `yaml:"resource,omitempty"`
}

func DecodeConfig(content string) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal([]byte(content), &c); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return &c, nil
}

// isStackEmpty checks if the stack field is empty, handling all possible types.
func isStackEmpty(stack any) bool {
	if stack == nil {
		return true
	}

	v := reflect.ValueOf(stack)
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}
