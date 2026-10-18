package lint

import (
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// checkYAMLSchema checks that YAML content matches a JSON schema.
func checkYAMLSchema(content string, schema *gojsonschema.Schema) *Result {
	var data = make(map[string]any)
	if err := yaml.Unmarshal([]byte(content), &data); err != nil {
		result := &Result{}
		result.AddError("", interpretYAMLError(err))
		return result
	}
	return checkSchemaAt(data, schema, "")
}

// checkSchemaAt validates already-parsed data against a JSON schema, prefixing
// each issue's path with pathPrefix (e.g. "applications.app").
func checkSchemaAt(data any, schema *gojsonschema.Schema, pathPrefix string) *Result {
	result := &Result{}

	schemaResult, err := schema.Validate(gojsonschema.NewGoLoader(data))
	if err != nil {
		result.AddError(pathPrefix, err.Error())
		return result
	}
	errs := schemaResult.Errors()
	for _, e := range errs {
		if (e.Type() == "number_any_of" || e.Type() == "number_all_of") && hasDetailedError(errs, e) {
			continue
		}
		field := e.Field()
		if field == "(root)" {
			field = ""
		}
		// Some descriptions repeat the field, which is already in the path.
		msg := strings.TrimPrefix(e.Description(), field+" ")
		switch {
		case pathPrefix == "":
		case field == "":
			field = pathPrefix
		default:
			field = pathPrefix + "." + field
		}
		result.AddError(field, msg)
	}

	return result
}

// hasDetailedError reports whether another error is at or below the field of an
// anyOf or allOf error, as gojsonschema also reports the errors of the branches.
func hasDetailedError(errs []gojsonschema.ResultError, summary gojsonschema.ResultError) bool {
	field := summary.Field()
	for _, e := range errs {
		if e == summary {
			continue
		}
		if f := e.Field(); f == field || field == "(root)" || strings.HasPrefix(f, field+".") {
			return true
		}
	}
	return false
}

func interpretYAMLError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "unknown escape character") {
		return fmt.Sprintf("%s: perhaps use single quotes for complex strings", msg)
	}
	return msg
}
