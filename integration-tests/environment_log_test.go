package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func TestEnvironmentLogAPI(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})

	// 150 log lines, one per second, ending 20 seconds ago.
	end := time.Now().Add(-20 * time.Second).UTC().Truncate(time.Second)
	type logLine struct {
		Cursor   string `json:"cursor"`
		Datetime string `json:"datetime"`
		Severity string `json:"severity"`
		Service  string `json:"service"`
		LogKind  string `json:"log_kind"`
		Content  string `json:"content"`
		Context  string `json:"context,omitempty"`
	}
	lines := make([]logLine, 0, 150)
	for i := range 150 {
		ts := end.Add(time.Duration(i-149) * time.Second)
		severity := "INFO"
		if i%50 == 0 {
			severity = "ERROR"
		}
		lines = append(lines, logLine{
			Cursor:   fmt.Sprintf("%026d", i),
			Datetime: ts.Format("2006-01-02T15:04:05.000Z"),
			Severity: severity,
			Service:  "app",
			LogKind:  "access",
			Content:  fmt.Sprintf("line %d", i),
		})
	}

	lines[100].Context = `{"time":"x","level":"ERROR","msg":"line 100","status":500,"path":"/a b","trace_id":"abc",` +
		`"req":{"method":"GET"},"keys":["k"]}`

	var (
		mu      sync.Mutex
		queries []url.Values
	)
	envPath := "/projects/" + projectID + "/environments/main"
	queryPath := envPath + "/observability/logs/query"
	apiHandler.Get(envPath+"/observability/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks("logs_query=" + apiServer.URL + queryPath),
			"data_retention": map[string]any{
				"logs": map[string]any{"retention_period": 1440, "max_range": 64800, "recommended_default_range": 15},
			},
		})
	})
	// Modeled on the API: pages of up to 100 lines, filtered by time, cursor and severity.
	apiHandler.Get(queryPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
		to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
		cursor := q.Get("cursor")
		desc := q.Get("order_by") != "ASC"
		severities := map[string]bool{}
		for _, s := range q["severities[]"] {
			severities[s] = true
		}
		var matched []logLine
		for i := range lines {
			l := lines[len(lines)-1-i]
			if !desc {
				l = lines[i]
			}
			ts, _ := time.Parse(time.RFC3339, l.Datetime)
			if ts.Unix() < from || ts.Unix() >= to {
				continue
			}
			if cursor != "" && ((desc && l.Cursor >= cursor) || (!desc && l.Cursor <= cursor)) {
				continue
			}
			if len(severities) > 0 && !severities[l.Severity] {
				continue
			}
			matched = append(matched, l)
		}
		hasMore := len(matched) > 100
		if hasMore {
			matched = matched[:100]
		}
		next := cursor
		if len(matched) > 0 {
			next = matched[len(matched)-1].Cursor
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_cursor":           next,
			"_has_more_results": hasMore,
			"data":              matched,
		})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// The default is 100 lines, oldest first.
	out := f.Run("log", "access", "-p", projectID, "-e", "main", "--format", "raw")
	assert.Equal(t, "line 50\n", out[:len("line 50\n")])
	assert.Contains(t, out, "\nline 149\n")
	assert.NotContains(t, out, "line 49\n")

	mu.Lock()
	first := queries[0]
	mu.Unlock()
	assert.Equal(t, []string{"access"}, first["log_kinds[]"])
	assert.Equal(t, "1", first.Get("log_kinds_mode"))
	assert.Equal(t, "DESC", first.Get("order_by"))

	// Pagination and multiple time windows.
	out = f.Run("log", "access", "-p", projectID, "-e", "main", "--format", "raw", "--lines", "150")
	assert.Equal(t, 150, strings.Count(out, "\n"))

	// The "error" type filters by severity.
	out = f.Run("log", "error", "-p", projectID, "-e", "main")
	assert.Equal(t, 3, strings.Count(out, "\n"))
	assert.Contains(t, out, lines[100].Datetime+
		` app access ERROR line 100 status=500 path="/a b" req.method=GET keys=["k"]`+"\n")
	assert.Contains(t, out, lines[50].Datetime+" app access ERROR line 50\n")

	// JSON lines.
	out = f.Run("log", "-p", projectID, "-e", "main", "--lines", "1", "--format", "json")
	var decoded logLine
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, lines[149], decoded)

	mu.Lock()
	last := queries[len(queries)-1]
	mu.Unlock()
	assert.Equal(t, []string{"platform"}, last["log_kinds[]"])
	assert.Equal(t, "-1", last.Get("log_kinds_mode"))

	// Filters.
	f.Run("log", "-p", projectID, "-e", "main", "-s", "app,worker", "--severity", "warning", "--since", "5m")
	mu.Lock()
	last = queries[len(queries)-1]
	mu.Unlock()
	assert.Equal(t, []string{"app", "worker"}, last["services[]"])
	assert.Equal(t, []string{"EMERGENCY", "ALERT", "CRITICAL", "ERROR", "WARNING"}, last["severities[]"])
	from, _ := strconv.ParseInt(last.Get("from"), 10, 64)
	assert.InDelta(t, time.Now().Add(-5*time.Minute).Unix(), from, 60)

	// The app can be selected by an environment variable.
	f.extraEnv = []string{"PLATFORM_APPLICATION_NAME=app"}
	f.Run("log", "-p", projectID, "-e", "main")
	mu.Lock()
	last = queries[len(queries)-1]
	mu.Unlock()
	assert.Equal(t, []string{"app"}, last["services[]"])
}

func TestEnvironmentLogSSHFallback(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := sshServer.Stop(); err != nil {
			t.Error(err)
		}
	})

	projectID := mockapi.ProjectID()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})
	mainEnv := makeEnv(projectID, "main", "production", "active", nil)
	mainEnv.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Disk: 2048, Mounts: map[string]mockapi.Mount{}},
		},
		Services: map[string]mockapi.App{},
		Workers:  map[string]mockapi.Worker{},
		Routes:   mockRoutes(),
		Links:    mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	mainEnv.Links["pf:ssh:app:0"] = mockapi.HALLink{HREF: "ssh://app--0@ssh.cli-tests.example.com"}
	apiHandler.SetEnvironments([]*mockapi.Environment{mainEnv})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{
		EnvPrefix + "SSH_OPTIONS=HostName 127.0.0.1\nPort " + strconv.Itoa(sshServer.Port()),
		EnvPrefix + "SSH_HOST_KEYS=" + sshServer.HostKeyConfig(),
	}

	// The observability API is not mocked, so it responds with 404.
	_, stdErr, _ := f.RunCombinedOutput("log", "access", "-p", projectID, "-e", "main")
	assert.Contains(t, stdErr, "Reading log file app--0@ssh.cli-tests.example.com:/var/log/access.log")

	_, stdErr, err = f.RunCombinedOutput("log", "access", "-p", projectID, "-e", "main", "--severity", "error")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The --severity option cannot be used: the logs API is not available for this environment")
}
