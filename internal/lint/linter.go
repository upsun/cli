package lint

import (
	"context"
	"errors"
	"fmt"

	"github.com/upsun/cli/internal/lint/registry"
	"github.com/upsun/cli/internal/lint/schema"
)

var ErrEmptyContent = errors.New("empty content")

// Lint checks generated configuration and returns a Result.
func Lint(_ context.Context, content string) (*Result, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}

	reg, err := registry.Parsed()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	yamlSchema, err := schema.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load schema: %w", err)
	}

	// Check YAML validity and schema compliance.
	if result := CheckYAMLSchema(content, yamlSchema); result.HasErrors() {
		return result, nil
	}

	cfg, err := DecodeConfig(content)
	if err != nil {
		return nil, err
	}

	// Run all other validation checks.
	return Combine(
		CheckRelationships(cfg),
		CheckNames(cfg),
		CheckTypes(cfg, reg),
		CheckScripts(cfg),
		CheckWebConfig(cfg),
		CheckDependencies(cfg),
		CheckRoutes(cfg),
	), nil
}
