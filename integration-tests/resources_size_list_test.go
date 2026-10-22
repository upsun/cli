package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSizeListGuaranteedCPU checks that guaranteed CPU sizes are
// listed only if the deployment's project info supports them.
func TestResourcesSizeListGuaranteedCPU(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		settings     map[string]any
		capabilities map[string]any
		want         string
	}{
		{
			name:         "supported",
			settings:     map[string]any{"enable_guaranteed_resources": true},
			capabilities: map[string]any{"guaranteed_resources": map[string]any{"enabled": true}},
			want:         "0.5,shared\n2,guaranteed\n",
		},
		{
			name:         "not enabled in settings",
			settings:     map[string]any{"enable_guaranteed_resources": false},
			capabilities: map[string]any{"guaranteed_resources": map[string]any{"enabled": true}},
			want:         "0.5,shared\n",
		},
		{
			name:         "not enabled in capabilities",
			settings:     map[string]any{"enable_guaranteed_resources": true},
			capabilities: map[string]any{"guaranteed_resources": map[string]any{"enabled": false}},
			want:         "0.5,shared\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			next := nextDeployment(map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources":         map[string]any{"profile_size": "0.5"},
					"instance_count":    1,
					"disk":              512,
				},
			}, map[string]any{
				"BALANCED": map[string]any{
					"0.5": map[string]any{"cpu": 0.5, "memory": 128, "cpu_type": "shared"},
					"2":   map[string]any{"cpu": 2, "memory": 512, "cpu_type": "guaranteed"},
				},
			})
			next["project_info"] = map[string]any{"settings": c.settings, "capabilities": c.capabilities}

			apiHandler := mockapi.NewHandler(t)
			projectID, _ := setUpResourcesProject(apiHandler, "org-size-list", next)
			f := serveAPI(t, apiHandler)

			stdout, stderr, err := f.RunCombinedOutput("resources:size:list", "-p", projectID, "-e", "main",
				"--format", "csv", "--columns", "size,cpu_type", "--no-header")
			require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.Equal(t, c.want, stdout)
		})
	}
}
