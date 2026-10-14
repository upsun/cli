package alt_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/upsun/cli/internal/config/alt"
)

func TestAlt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	tempDir := t.TempDir()

	configNode := &yaml.Node{}
	require.NoError(t, yaml.Unmarshal(testConfig, configNode))

	binPath := filepath.Join(tempDir, "bin", "test-binary")
	configPath := filepath.Join(tempDir, "config", "config.yaml")

	a := alt.New(
		binPath,
		"Generated for test",
		"example-target",
		"/opt/bin/example-target",
		configPath,
		configNode,
	)
	assert.NoError(t, a.GenerateAndSave())

	assert.FileExists(t, binPath)
	assert.FileExists(t, configPath)

	binContent, err := os.ReadFile(binPath)
	assert.NoError(t, err)
	assert.Equal(t, `#!/bin/sh
# Generated for test
export CLI_CONFIG_FILE=`+configPath+`
[ -x /opt/bin/example-target ] && exec /opt/bin/example-target "$@"
exec example-target "$@"
`, string(binContent))
}
