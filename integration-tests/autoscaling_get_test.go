package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAutoscalingSettingsGetDirections checks that each direction of a trigger shows whether the trigger is enabled.
func TestAutoscalingSettingsGetDirections(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := setUpAutoscaling(apiHandler, map[string]any{
		"app": map[string]any{
			"triggers": map[string]any{
				"cpu": map[string]any{
					"enabled": true,
					"up":      map[string]any{"threshold": 80, "duration": 60},
					"down":    map[string]any{"threshold": 20, "duration": 300},
				},
			},
			"instances":      map[string]any{"min": 1, "max": 3},
			"scale_cooldown": map[string]any{"up": 300, "down": 600},
		},
	})
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()
	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("autoscaling:get", "-p", projectID, "-e", "main", "--format", "csv",
		"--columns", "service,metric,direction,threshold,duration,enabled,cooldown,min_instances,max_instances")
	require.NoError(t, err, stderr)
	assertTrimmed(t, `
App or service,Metric,Direction,Threshold (%),Duration (s),Enabled,Cooldown (s),Minimum instances,Maximum instances
app,cpu,up,80.0%,60,true,300,1,3
app,cpu,down,20.0%,300,true,600,1,3
`, stdout)
}

// TestAutoscalingSettingsSetPartial checks changes to settings that omit values, e.g. after a partial update.
func TestAutoscalingSettingsSetPartial(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := setUpAutoscaling(apiHandler, map[string]any{
		"app": map[string]any{
			"triggers": map[string]any{
				"cpu": map[string]any{"enabled": true, "up": map[string]any{"threshold": 80}},
			},
			"instances":      map[string]any{"max": 3},
			"scale_cooldown": map[string]any{"down": 600},
		},
	})
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()
	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Verbose output shows PHP warnings.
	_, stderr, err := f.RunCombinedOutput("autoscaling:set", "-v", "-p", projectID, "-e", "main",
		"--service", "app", "--metric", "cpu", "--duration-up", "2m", "--cooldown-up", "5m", "--instances-min", "2",
		"--dry-run")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stderr, "Warning")
	assert.Contains(t, stderr, "Duration: 2m")
	assert.Contains(t, stderr, "Cooldown: 5m")
	assert.Contains(t, stderr, "Min: 2")
}

// TestAutoscalingSettingsSetInteractivePartialDefaults checks the interactive form when the defaults omit a value.
func TestAutoscalingSettingsSetInteractivePartialDefaults(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := setUpAutoscalingWithDefaults(apiHandler, map[string]any{
		"triggers": map[string]any{"cpu": map[string]any{
			"up":   map[string]any{"threshold": 80, "duration": 60},
			"down": map[string]any{"threshold": 20, "duration": 600},
		}},
		"instances": map[string]any{"min": 1},
	}, map[string]any{})
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()
	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Accept the defaults for the metric, and for each scaling direction's threshold and duration
	// (the only app is chosen without a question). The cooldowns and the maximum instance count
	// have no default: an empty answer is rejected and the question is asked again.
	_, stderr, err := f.RunInteractive("\n\n\n\n5m\n\n\n\n5m\n\n\n3\n",
		"autoscaling:set", "-p", projectID, "-e", "main", "--dry-run")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stderr, "TypeError")
	assert.Equal(t, 3, strings.Count(stderr, "A value is required."))
	assert.Contains(t, stderr, "Cooldown: 5m")
	assert.Contains(t, stderr, "Max: 3")
}
