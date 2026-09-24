package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestUpdateRepairsCorruptFile(t *testing.T) {
	home := t.TempDir()
	cnf := &config.Config{}
	cnf.Application.EnvPrefix = "TEST_STATE_"
	cnf.Application.WritableUserDir = ".test"
	cnf.Application.UserStateFile = "state.json"
	t.Setenv("TEST_STATE_HOME", home)

	statePath := filepath.Join(home, ".test", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(statePath), 0o700))
	require.NoError(t, os.WriteFile(statePath, []byte(`{"updates":{}}garbage`), 0o600))

	require.NoError(t, Update(cnf, func(s *State) { s.Updates.LastChecked = 42 }))

	s, err := Load(cnf)
	require.NoError(t, err)
	assert.EqualValues(t, 42, s.Updates.LastChecked)
}
