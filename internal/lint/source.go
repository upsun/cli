package lint

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// sourceError is a problem found while loading a YAML file, at a line.
type sourceError struct {
	file string
	line int
	msg  string
}

func (e *sourceError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.file, e.line, e.msg)
	}
	return e.file + ": " + e.msg
}

// issue converts the error to a lint issue.
func (e *sourceError) issue() Issue {
	return Issue{File: e.file, Line: e.line, Message: e.msg}
}

// loadYAML reads and parses the YAML file name in fsys, resolving the !include,
// !file and !archive tags as the platform does. It returns the document's
// content node, or nil for an empty document.
func loadYAML(fsys fs.FS, name string) (*yaml.Node, error) {
	return loadYAMLIncluding(fsys, name, nil)
}

func loadYAMLIncluding(fsys fs.FS, name string, including []string) (*yaml.Node, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, &sourceError{file: name, msg: err.Error()}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, &sourceError{file: name, msg: interpretYAMLError(err)}
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	r := &tagResolver{fsys: fsys, file: name, including: append(including, name)}
	if err := r.resolve(root); err != nil {
		return nil, err
	}
	return root, nil
}

// tagResolver resolves the repository tags in a file. Paths are relative to the
// file's directory and may not leave the project.
type tagResolver struct {
	fsys      fs.FS
	file      string
	including []string
}

func (r *tagResolver) resolve(node *yaml.Node) error {
	switch node.Tag {
	case "!file", "!archive":
		return r.resolveReference(node)
	case "!include":
		return r.resolveInclude(node)
	}
	for _, child := range node.Content {
		if err := r.resolve(child); err != nil {
			return err
		}
	}
	return nil
}

// resolveReference checks a !file or !archive tag, which refers to a file or a
// directory, and replaces it with the path.
func (r *tagResolver) resolveReference(node *yaml.Node) error {
	wantDir := node.Tag == "!archive"
	if node.Kind != yaml.ScalarNode {
		return r.errorf(node, "the `%s` tag value should be a path", node.Tag)
	}
	p, isDir, err := r.lookup(node, node.Value)
	if err != nil {
		return err
	}
	if wantDir && !isDir {
		return r.errorf(node, "'%s' doesn't point to a directory", node.Value)
	} else if !wantDir && isDir {
		return r.errorf(node, "'%s' doesn't point to a file", node.Value)
	}
	setScalar(node, p)
	return nil
}

// resolveInclude replaces an !include tag with the content it refers to.
func (r *tagResolver) resolveInclude(node *yaml.Node) error {
	opts := map[string]string{}
	switch node.Kind {
	case yaml.ScalarNode:
		opts["path"], opts["type"] = node.Value, "yaml"
	case yaml.MappingNode:
		if err := node.Decode(&opts); err != nil {
			return r.errorf(node, "invalid `!include` tag: %s", err)
		}
	default:
		return r.errorf(node, "the `!include` tag value should be a scalar or a mapping")
	}
	rel, ok := opts["path"]
	if !ok {
		return r.errorf(node, "the `!include` tag must specify a `path`")
	}
	p, isDir, err := r.lookup(node, rel)
	if err != nil {
		return err
	}
	includeType, ok := opts["type"]
	if !ok {
		return r.errorf(node, "the `!include` tag must have a `type` specified")
	}
	types := []string{"archive", "binary", "string", "yaml"}
	if !slices.Contains(types, includeType) {
		return r.errorf(node, "`!include` type must be one of %s", strings.Join(types, ", "))
	}
	if includeType == "archive" {
		if !isDir {
			return r.errorf(node, "`%s` doesn't point to a directory", rel)
		}
		setScalar(node, p)
		return nil
	}
	if isDir {
		return r.errorf(node, "`%s` doesn't point to a file", rel)
	}
	switch includeType {
	case "yaml":
		if slices.Contains(r.including, p) {
			return r.errorf(node, "`%s` is included recursively", rel)
		}
		included, err := loadYAMLIncluding(r.fsys, p, r.including)
		if err != nil {
			return err
		}
		if included == nil {
			setScalar(node, "")
			node.Tag = "!!null"
			return nil
		}
		// Issues within the included content are reported at the tag.
		line, column := node.Line, node.Column
		*node = *included
		setPosition(node, line, column)
	case "binary":
		b, err := fs.ReadFile(r.fsys, p)
		if err != nil {
			return r.errorf(node, "cannot read file `%s`: %s", rel, err)
		}
		setScalar(node, "data:;base64,"+base64.StdEncoding.EncodeToString(b))
	default: // string
		b, err := fs.ReadFile(r.fsys, p)
		if err != nil {
			return r.errorf(node, "cannot read file `%s`: %s", rel, err)
		}
		setScalar(node, string(b))
	}
	return nil
}

// lookup resolves a tag's path relative to the current file, returning its
// path in the project and whether it is a directory.
func (r *tagResolver) lookup(node *yaml.Node, rel string) (p string, isDir bool, err error) {
	p = path.Join(path.Dir(r.file), rel)
	if !fs.ValidPath(p) || strings.HasPrefix(rel, "/") {
		return "", false, r.errorf(node, "'%s' is outside the project", rel)
	}
	fi, err := fs.Stat(r.fsys, p)
	if err != nil {
		return "", false, r.errorf(node, "'%s' doesn't exist in the repository", rel)
	}
	return p, fi.IsDir(), nil
}

func (r *tagResolver) errorf(node *yaml.Node, format string, args ...any) error {
	return &sourceError{file: r.file, line: node.Line, msg: fmt.Sprintf(format, args...)}
}

// setScalar turns node into a plain string scalar, keeping its position.
func setScalar(node *yaml.Node, value string) {
	node.Kind, node.Tag, node.Value, node.Style, node.Content = yaml.ScalarNode, "!!str", value, 0, nil
}

// setPosition sets the position of node and all of its descendants.
func setPosition(node *yaml.Node, line, column int) {
	node.Line, node.Column = line, column
	for _, child := range node.Content {
		setPosition(child, line, column)
	}
}

// source is where a configuration entry is defined.
type source struct {
	file string
	line int // The line of the entry's key, or 0 to use the node's line.
	node *yaml.Node
}

// sourceIndex maps configuration entries to where they are defined. Keys are
// "<section>.<name>" (e.g. "applications.app"), or "<section>" for a Fixed
// file holding a whole section (e.g. .platform/services.yaml).
type sourceIndex map[string]source

// locate sets the file and line of issues that do not have them, by matching
// each issue's path to an entry and then to a node within it.
func (s sourceIndex) locate(result *Result) {
	for _, issues := range [][]Issue{result.Errors, result.Warnings} {
		for i := range issues {
			if issues[i].File != "" {
				continue
			}
			key, rest, ok := s.match(issues[i].Path)
			if !ok {
				continue
			}
			src := s[key]
			issues[i].File = src.file
			if issues[i].Line = lineOf(src.node, rest); rest == "" && src.line > 0 {
				issues[i].Line = src.line
			}
		}
	}
}

// match returns the longest entry key that prefixes path, and the rest of the
// path. Route keys are URLs that may contain dots, and issues may refer to an
// entry as <section>["<name>"].
func (s sourceIndex) match(p string) (key, rest string, ok bool) {
	for k := range s {
		section, name, hasName := strings.Cut(k, ".")
		prefixes := []string{k}
		if hasName {
			prefixes = append(prefixes, fmt.Sprintf("%s[%q]", section, name))
		}
		for _, prefix := range prefixes {
			r, found := strings.CutPrefix(p, prefix)
			if found && (r == "" || r[0] == '.' || r[0] == '[') && len(k) > len(key) {
				key, rest, ok = k, r, true
			}
		}
	}
	return key, rest, ok
}

// lineOf follows a path such as ".web.locations[\"/\"].root" or
// ".authorizations.0" from node, and returns the line of the deepest part found.
func lineOf(node *yaml.Node, p string) int {
	if node == nil {
		return 0
	}
	line := node.Line
	for p != "" {
		switch node.Kind {
		case yaml.MappingNode:
			var key string
			if r, ok := strings.CutPrefix(p, `["`); ok {
				end := strings.Index(r, `"]`)
				if end < 0 {
					return line
				}
				key, p = r[:end], r[end+2:]
			} else {
				p = strings.TrimPrefix(p, ".")
				// Keys may contain dots, so the longest matching key wins.
				for i := 0; i+1 < len(node.Content); i += 2 {
					k := node.Content[i].Value
					if (p == k || strings.HasPrefix(p, k+".") || strings.HasPrefix(p, k+"[")) && len(k) > len(key) {
						key = k
					}
				}
				if key == "" {
					return line
				}
				p = p[len(key):]
			}
			value := mappingValue(node, key)
			if value == nil {
				return line
			}
			line, node = value.keyLine, value.node
		case yaml.SequenceNode:
			r := strings.TrimPrefix(p, ".")
			end := strings.IndexAny(r, ".[")
			if end < 0 {
				end = len(r)
			}
			i, err := strconv.Atoi(r[:end])
			if err != nil || i < 0 || i >= len(node.Content) {
				return line
			}
			node, p = node.Content[i], r[end:]
			line = node.Line
		default:
			return line
		}
	}
	return line
}

type mappingEntry struct {
	keyLine int
	node    *yaml.Node
}

// mappingValue returns the value for key in a mapping node, or nil.
func mappingValue(node *yaml.Node, key string) *mappingEntry {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return &mappingEntry{keyLine: node.Content[i].Line, node: node.Content[i+1]}
		}
	}
	return nil
}
