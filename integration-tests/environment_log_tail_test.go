//go:build unix

package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// Lines that become available late, with timestamps behind the last poll, are still printed by --tail.
func TestEnvironmentLogTailLateLines(t *testing.T) {
	t.Parallel()
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

	// Cursors are ordered by time, like the API's ULIDs.
	line := func(ts time.Time, content string) logLine {
		return logLine{
			Cursor:   fmt.Sprintf("%026d", ts.UnixMilli()),
			Datetime: ts.UTC().Format("2006-01-02T15:04:05.000Z"),
			Severity: "INFO",
			Service:  "app",
			LogKind:  "access",
			Content:  content,
		}
	}
	start := time.Now()

	var (
		mu    sync.Mutex
		lines = append(make([]logLine, 0, 3), line(start.Add(-40*time.Second), "line 0"))
		polls int
	)
	envPath := "/projects/" + projectID + "/environments/main"
	queryPath := envPath + "/observability/logs/query"
	apiHandler.Get(envPath+"/observability/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks("logs_query=" + apiServer.URL + queryPath),
		})
	})
	apiHandler.Get(queryPath, mockLogsQuery(func(q url.Values) ([]logLine, int) {
		mu.Lock()
		defer mu.Unlock()
		if q.Get("order_by") == "ASC" {
			// Polls wider than 30 seconds are rejected, like the API does for too much data.
			from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
			to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
			if to-from > 30 {
				return nil, 499
			}
			polls++
		}
		return lines, 0
	}))

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{EnvPrefix + "LOG_PROTOCOL=auto"}
	cmd := f.buildCommand("log", "access", "-p", projectID, "-e", "main", "--format", "raw", "--tail")
	// Run in a process group, to stop the PHP process too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	output := make(chan string, 10)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			output <- scanner.Text()
		}
		close(output)
	}()
	next := func(timeout time.Duration) string {
		select {
		case l := <-output:
			return l
		case <-time.After(timeout):
			return "(timeout)"
		}
	}

	assert.Equal(t, "line 0", next(20*time.Second))

	// After the first poll, a line arrives with a timestamp in the range that was already polled.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return polls > 0
	}, 20*time.Second, 100*time.Millisecond)
	mu.Lock()
	lines = append(lines, line(start.Add(-20*time.Second), "line 1 (late)"))
	mu.Unlock()

	assert.Equal(t, "line 1 (late)", next(20*time.Second))

	// A line that arrives on time is printed after the tail delay, and lines are not repeated.
	mu.Lock()
	lines = append(lines, line(time.Now(), "line 2"))
	mu.Unlock()
	assert.Equal(t, "line 2", next(30*time.Second))
	// Two more polls.
	assert.Equal(t, "(timeout)", next(11*time.Second))
}
