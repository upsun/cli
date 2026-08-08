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

		Web struct {
			Commands struct {
				Start     string `yaml:"start,omitempty"`
				PostStart string `yaml:"post_start,omitempty"`
			} `yaml:"commands,omitempty"`

			Locations map[string]struct {
				Root  string         `yaml:"root,omitempty"`
				Rules map[string]any `yaml:"rules,omitempty"`
			} `yaml:"locations,omitempty"`
		} `yaml:"web,omitempty"`

		Relationships map[string]any `yaml:"relationships,omitempty"`

		Crons map[string]struct {
			Commands struct {
				Start string `yaml:"start,omitempty"`
				Stop  string `yaml:"stop,omitempty"`
			} `yaml:"commands,omitempty"`
		} `yaml:"crons,omitempty"`

		Workers map[string]struct {
			Type string `yaml:"type,omitempty"`

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
