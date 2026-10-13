package init

import (
	"bytes"
	"embed"
	"io"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
)

// The YAML lexer and "bw" style are copied from chroma (see chroma/COPYING), as importing
// its lexers and styles packages registers all of them at startup, slowing every command.
//
//go:embed chroma/yaml.xml chroma/bw.xml
var chromaFiles embed.FS

// highlightYAML writes YAML with terminal syntax highlighting.
func highlightYAML(w io.Writer, source string) error {
	lexer, err := chroma.NewXMLLexer(chromaFiles, "chroma/yaml.xml")
	if err != nil {
		return err
	}
	styleXML, err := chromaFiles.ReadFile("chroma/bw.xml")
	if err != nil {
		return err
	}
	style, err := chroma.NewXMLStyle(bytes.NewReader(styleXML))
	if err != nil {
		return err
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		return err
	}
	return formatters.TTY.Format(w, style, it)
}
