package lint

import (
	"context"
	"errors"
	"fmt"

	"github.com/upsun/cli/internal/lint/registry"
	"github.com/upsun/cli/internal/lint/schema"
)

var ErrEmptyContent = errors.New("empty content")

// CheckContent checks merged Flex-style configuration content and returns a Result.
func CheckContent(_ context.Context, content string) (*Result, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
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

	return runChecks(cfg, StyleFlex)
}

// runChecks runs the semantic checks over a decoded config, adapting some
// checks to the configuration style.
func runChecks(cfg *Config, style Style) (*Result, error) {
	reg, err := registry.Parsed()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	return Combine(
		CheckRelationships(cfg),
		CheckNames(cfg),
		CheckTypes(cfg, reg, style),
		CheckScripts(cfg),
		CheckWebConfig(cfg),
		CheckDependencies(cfg),
		CheckRoutes(cfg),
	), nil
}
