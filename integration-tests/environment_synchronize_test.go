package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentSynchronize(t *testing.T) {
	t.Parallel()
	feature := makeEnv(layoutProjectID, "feature", "development", "active", "main")
	feature.AddOperations("synchronize")
	p := setUpProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil), feature)

	cases := []struct {
		name       string
		stdin      string
		args       []string
		wantBody   map[string]any
		wantErr    bool
		wantStdErr string
	}{
		{
			name:       "arguments",
			args:       []string{"code", "data"},
			stdin:      "y\n",
			wantBody:   map[string]any{"synchronize_code": true, "synchronize_data": true, "rebase": false},
			wantStdErr: "Are you sure you want to synchronize code and data from main to feature?",
		},
		{
			name:     "both, rebasing",
			args:     []string{"both", "--rebase"},
			stdin:    "y\n",
			wantBody: map[string]any{"synchronize_code": true, "synchronize_data": true, "rebase": true},
		},
		{
			name:       "prompts for code with rebase",
			stdin:      "y\ny\nn\n",
			wantBody:   map[string]any{"synchronize_code": true, "synchronize_data": false, "rebase": true},
			wantStdErr: "Do you want to synchronize code by rebasing instead of merging?",
		},
		{
			name:     "prompts for data",
			stdin:    "n\ny\n",
			wantBody: map[string]any{"synchronize_code": false, "synchronize_data": true, "rebase": false},
		},
		{
			name:     "--yes merges",
			args:     []string{"--yes"},
			wantBody: map[string]any{"synchronize_code": true, "synchronize_data": true, "rebase": false},
		},
		{
			name:     "--yes with --rebase",
			args:     []string{"--yes", "--rebase"},
			wantBody: map[string]any{"synchronize_code": true, "synchronize_data": true, "rebase": true},
		},
		{
			name:       "nothing selected",
			stdin:      "n\nn\n",
			wantErr:    true,
			wantStdErr: "You did not select anything to synchronize.",
		},
		{
			name:       "declined",
			args:       []string{"data"},
			stdin:      "n\n",
			wantErr:    true,
			wantStdErr: "Are you sure you want to synchronize data from main to feature?",
		},
		{
			name:       "invalid argument",
			args:       []string{"files"},
			wantErr:    true,
			wantStdErr: "Invalid value: files (it must be one of: code, data or both).",
		},
	}
	// The cases run in sequence, each adding at most one request.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(p.actions("feature"))
			args := append([]string{"sync", "-p", layoutProjectID, "-e", "feature"}, c.args...)
			_, stdErr, err := p.f.RunInteractive(c.stdin, args...)
			if c.wantErr {
				assertExitCode(t, 1, err)
			} else {
				require.NoError(t, err)
			}
			assert.Contains(t, stdErr, c.wantStdErr)
			actions := p.actions("feature")
			if c.wantBody == nil {
				assert.Len(t, actions, before)
				return
			}
			require.Len(t, actions, before+1)
			assert.Equal(t, mockapi.EnvironmentAction{Type: "environment.synchronize", Body: c.wantBody}, actions[before])
		})
	}

	_, stdErr, err := p.f.RunCombinedOutput("sync", "-p", layoutProjectID, "-e", "main", "data")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Operation not available: The environment main can't be synchronized.")
	assert.Contains(t, stdErr, "The environment does not have a parent.")
}
