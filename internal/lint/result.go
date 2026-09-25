package lint

import (
	"fmt"
	"sort"
	"strings"
)

// Result contains the results of linting, including both errors and warnings.
type Result struct {
	Errors   []Issue
	Warnings []Issue
}

// Issue represents a single linting problem with its location in the content.
type Issue struct {
	File    string `json:"file,omitempty"` // The file, relative to the project root, if known.
	Line    int    `json:"line,omitempty"` // The line in the file, if known.
	Path    string `json:"path"`           // e.g., "applications.foo.type", "services.database"
	Message string `json:"message"`
}

// Location returns the file and line of the issue, e.g. ".upsun/config.yaml:12".
func (i Issue) Location() string {
	if i.Line > 0 {
		return fmt.Sprintf("%s:%d", i.File, i.Line)
	}
	return i.File
}

// AddError adds an error to the linter result.
func (r *Result) AddError(path, message string) {
	r.Errors = append(r.Errors, Issue{
		Path:    path,
		Message: message,
	})
}

// AddWarning adds a warning to the linter result.
func (r *Result) AddWarning(path, message string) {
	r.Warnings = append(r.Warnings, Issue{
		Path:    path,
		Message: message,
	})
}

// HasErrors returns true if the validation result contains any errors.
func (r *Result) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings returns true if the validation result contains any warnings.
func (r *Result) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// issueLines formats a list of validation issues into sorted lines, without a
// heading. Callers that want a heading prepend their own (see formatIssues).
func issueLines(issues []Issue) []string {
	if len(issues) == 0 {
		return nil
	}

	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		var parts []string
		for _, part := range []string{issue.Location(), issue.Path, issue.Message} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		messages = append(messages, "  - "+strings.Join(parts, ": "))
	}

	// Sort the issue messages for consistent ordering.
	sort.Strings(messages)
	return messages
}

// ErrorLines returns the formatted, sorted error lines without a heading.
func (r *Result) ErrorLines() []string { return issueLines(r.Errors) }

// WarningLines returns the formatted, sorted warning lines without a heading.
func (r *Result) WarningLines() []string { return issueLines(r.Warnings) }

// formatIssues formats a list of validation issues with a given prefix.
func formatIssues(issues []Issue, prefix string) []string {
	lines := issueLines(issues)
	if lines == nil {
		return nil
	}
	return append([]string{prefix}, lines...)
}

// formatResult formats all lint issues with appropriate capitalization.
func (r *Result) formatResult(capitalize bool) string {
	var messages []string

	errorPrefix := "linter errors:"
	warningPrefix := "linter warnings:"
	if capitalize {
		errorPrefix = "Linter errors:"
		warningPrefix = "Linter warnings:"
	}

	if errorMessages := formatIssues(r.Errors, errorPrefix); errorMessages != nil {
		messages = append(messages, errorMessages...)
	}

	if warningMessages := formatIssues(r.Warnings, warningPrefix); warningMessages != nil {
		messages = append(messages, warningMessages...)
	}

	return strings.Join(messages, "\n")
}

// Error returns a formatted string representation of all validation issues.
func (r *Result) Error() string {
	return r.formatResult(false)
}

// String returns a formatted string representation of all validation issues.
func (r *Result) String() string {
	return r.formatResult(true)
}

// Merge appends another result's errors and warnings into this one.
func (r *Result) Merge(other *Result) {
	if other == nil {
		return
	}
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// Combine combines a list of validation results.
func Combine(results ...*Result) *Result {
	result := &Result{}
	for _, r := range results {
		if r != nil {
			result.Errors = append(result.Errors, r.Errors...)
			result.Warnings = append(result.Warnings, r.Warnings...)
		}
	}
	return result
}
