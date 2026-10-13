package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuietErrors checks that --verbose and --debug override --quiet for errors before the legacy CLI runs.
func TestQuietErrors(t *testing.T) {
	f := newCommandFactory(t, "", "")
	// A file in place of the temporary directory makes the legacy CLI fail to start.
	tmpFile := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(tmpFile, nil, 0o600))
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TMP="+tmpFile)

	cases := []struct {
		args      []string
		wantError bool
	}{
		{[]string{"-q"}, false},
		{[]string{"-qv"}, true},
		{[]string{"-q", "--debug"}, true},
		{nil, true},
	}
	for _, c := range cases {
		_, stderr, err := f.RunCombinedOutput(append([]string{"project:list"}, c.args...)...)
		assertExitCode(t, 1, err)
		if c.wantError {
			assert.Contains(t, stderr, "failed to initialize PHP CLI", "args: %v", c.args)
		} else {
			assert.Empty(t, stderr, "args: %v", c.args)
		}
	}
}
