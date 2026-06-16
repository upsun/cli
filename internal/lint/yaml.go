package lint

import (
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// CheckYAMLSchema checks that YAML content matches a JSON schema.
func CheckYAMLSchema(content string, schema *gojsonschema.Schema) *Result {
	result := &Result{}

	var data = make(map[string]any)
	if err := yaml.Unmarshal([]byte(content), &data); err != nil {
		result.AddError("", interpretYAMLError(err))
		return result
	}

	schemaResult, err := schema.Validate(gojsonschema.NewGoLoader(data))
	if err != nil {
		result.AddError("", err.Error())
		return result
	}
	if !schemaResult.Valid() {
		for _, e := range schemaResult.Errors() {
			result.AddError(e.Field(), e.Description())
		}
	}

	return result
}

func interpretYAMLError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "unknown escape character") {
		return fmt.Sprintf("%s: perhaps use single quotes for complex strings", msg)
	}
	return msg
}
