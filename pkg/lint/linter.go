package lint

import (
	"errors"
	"fmt"

	"github.com/upsun/cli/internal/schema"
	"github.com/upsun/cli/pkg/registry"
)

// ErrEmptyContent is returned by CheckContent when the content is empty.
var ErrEmptyContent = errors.New("empty content")

// CheckContent lints Flex configuration given as a single YAML document, i.e.
// the .upsun/*.yaml files merged. Configuration problems are reported in the
// Result; the error is for content that cannot be checked at all.
func CheckContent(content string) (*Result, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}

	yamlSchema, err := schema.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load schema: %w", err)
	}

	// Check YAML validity and schema compliance.
	if result := checkYAMLSchema(content, yamlSchema); result.HasErrors() {
		return result, nil
	}

	cfg, err := decodeConfig(content)
	if err != nil {
		return nil, err
	}

	return runChecks(cfg)
}

// runChecks runs the semantic checks over a decoded config.
func runChecks(cfg *config) (*Result, error) {
	reg, err := registry.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	return combine(
		checkRelationships(cfg),
		checkNames(cfg),
		checkTypes(cfg, reg),
		checkScripts(cfg),
		checkWebConfig(cfg),
		checkDependencies(cfg),
		checkRoutes(cfg),
		checkTasks(cfg),
	), nil
}
