package lint

import (
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// CheckYAMLSchema checks that YAML content matches a JSON schema.
func CheckYAMLSchema(content string, schema *gojsonschema.Schema) *Result {
	var data = make(map[string]any)
	if err := yaml.Unmarshal([]byte(content), &data); err != nil {
		result := &Result{}
		result.AddError("", interpretYAMLError(err))
		return result
	}
	return CheckSchemaAt(data, schema, "")
}

// CheckSchemaAt validates already-parsed data against a JSON schema, prefixing
// each issue's path with pathPrefix (e.g. "applications.app").
func CheckSchemaAt(data any, schema *gojsonschema.Schema, pathPrefix string) *Result {
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
		// A non-empty stack list ("maxItems: 0" in the schema) is reported by
		// checkStackList, so that the semantic checks still run.
		if e.Type() == "array_max_items" && isStackField(e.Field()) {
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

// isStackField reports whether a schema error's field is a stack.
func isStackField(field string) bool {
	return field == "stack" || strings.HasSuffix(field, ".stack")
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
