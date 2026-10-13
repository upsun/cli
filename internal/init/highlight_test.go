package init

import (
	"bytes"
	"testing"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHighlightYAMLMatchesChroma(t *testing.T) {
	source := `# Comment
applications:
  app:
    type: "php:8.4"
    hooks:
      build: |
        set -e
        composer install --no-dev
    web:
      locations:
        "/": {root: "public", passthru: "/index.php"}
services:
  db:
    type: mariadb:11.4
routes:
  "https://{default}/": {type: upstream, upstream: "app:http"}
list:
  - 1
  - true
  - ~
`
	var expected, actual bytes.Buffer
	require.NoError(t, quick.Highlight(&expected, source, "yaml", "terminal", "bw"))
	require.NoError(t, highlightYAML(&actual, source))
	assert.Equal(t, expected.String(), actual.String())
	assert.NotEqual(t, source, actual.String(), "the output must be highlighted")
}
