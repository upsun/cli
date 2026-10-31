package tests

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsTimeOptions(t *testing.T) {
	t.Parallel()
	to := "2026-01-01T00:00:00Z"
	toTime, _ := time.Parse(time.RFC3339, to)
	toUnix := toTime.Unix()

	cases := []struct {
		name      string
		args      []string
		wantRange int64 // The expected "to" minus "from", in seconds.
		wantGrain string
		wantErr   string
	}{
		{name: "default range", args: nil, wantRange: 600},
		{name: "range in seconds", args: []string{"--range", "900"}, wantRange: 900},
		{name: "range in minutes", args: []string{"--range", "120m"}, wantRange: 7200},
		{name: "range in seconds with a unit", args: []string{"--range", "300s"}, wantRange: 300},
		{name: "range in hours and minutes", args: []string{"--range", "1h30m"}, wantRange: 5400},
		{name: "range in fractional hours", args: []string{"--range", "1.5h"}, wantRange: 5400},
		{name: "range in days", args: []string{"--range", "1d"}, wantRange: 86400},
		{name: "range in days and hours", args: []string{"--range", "2d 3h"}, wantRange: 183600},
		{name: "range in hours, minutes and seconds", args: []string{"--range", "1:30:00"}, wantRange: 5400},
		{name: "range in minutes and seconds", args: []string{"--range", "30:00"}, wantRange: 1800},
		{name: "invalid colon range", args: []string{"--range", "1:2:3:4"}, wantErr: "Invalid --range: 1:2:3:4"},
		{name: "interval in days", args: []string{"--range", "2d", "--interval", "1d"}, wantRange: 172800,
			wantGrain: "86400"},
		{name: "interval", args: []string{"--range", "1h", "--interval", "5m"}, wantRange: 3600, wantGrain: "300"},
		{name: "interval in seconds", args: []string{"--range", "1h", "--interval", "120s"}, wantRange: 3600,
			wantGrain: "120"},
		{name: "invalid range", args: []string{"--range", "abc"}, wantErr: "Invalid --range: abc"},
		{name: "invalid range unit", args: []string{"--range", "10x"}, wantErr: "Invalid --range: 10x"},
		{name: "negative range", args: []string{"--range=-10m"}, wantErr: "Invalid --range: -10m"},
		{name: "short range", args: []string{"--range", "4m"}, wantErr: "The --range 4m is too short"},
		{name: "invalid interval", args: []string{"--interval", "abc"}, wantErr: "Invalid --interval: abc"},
		{name: "short interval", args: []string{"--interval", "30s"}, wantErr: "The --interval 30s is too short"},
		{name: "zero interval", args: []string{"--interval", "0"}, wantErr: "Invalid --interval: 0"},
		{
			name:    "interval longer than the range",
			args:    []string{"--range", "10m", "--interval", "1h"},
			wantErr: "The --interval 1h is invalid. It cannot be greater than the selected time range",
		},
		{name: "zero range", args: []string{"--range", "0"}, wantErr: "Invalid --range: 0"},
		{name: "invalid to", args: []string{"--to", "not a time"}, wantErr: "Failed to parse --to time: not a time"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, apiHandler, projectID := setupMetricsTest(t, false, false)
			args := append([]string{"metrics:cpu", "-p", projectID, "-e", "main", "--to", to, "--format", "csv"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			requests := apiHandler.Requests("GET", "/projects/*/environments/main/observability/resources/overview")
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				assert.Empty(t, requests)
				return
			}
			require.NoError(t, err, stderr)
			require.Len(t, requests, 1)
			q := requests[0].Query
			assert.Equal(t, strconv.FormatInt(toUnix, 10), q.Get("to"))
			assert.Equal(t, strconv.FormatInt(toUnix-c.wantRange, 10), q.Get("from"))
			assert.Equal(t, c.wantGrain, q.Get("grain"))
		})
	}
}

func TestMetricsServiceOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		args         []string
		wantServices []string
		wantErr      string
	}{
		{name: "all services", args: nil},
		{name: "service", args: []string{"--service", "app"}, wantServices: []string{"app"}},
		{name: "services with a wildcard", args: []string{"-s", "a*,router"}, wantServices: []string{"app", "router"}},
		{name: "type without a version", args: []string{"--type", "mariadb"}, wantServices: []string{"db"}},
		{name: "type with a version", args: []string{"--type", "php:8.4"}, wantServices: []string{"app"}},
		{name: "service takes precedence over type", args: []string{"--service", "app", "--type", "mariadb"},
			wantServices: []string{"app"}},
		{name: "unknown service", args: []string{"--service", "nope"},
			wantErr: "No services were found matching the name(s): nope"},
		{name: "unknown type", args: []string{"--type", "nope"},
			wantErr: "No services were found matching the type(s): nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, apiHandler, projectID := setupMetricsTest(t, false, false)
			args := append([]string{"metrics:memory", "-p", projectID, "-e", "main", "--format", "csv"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				return
			}
			require.NoError(t, err, stderr)
			q := lastRequest(t, apiHandler, "GET", "/projects/*/environments/main/observability/resources/overview").Query
			var services []string
			for i := 0; q.Has("services[" + strconv.Itoa(i) + "]"); i++ {
				services = append(services, q.Get("services["+strconv.Itoa(i)+"]"))
			}
			assert.ElementsMatch(t, c.wantServices, services)
			if c.wantServices != nil {
				assert.Equal(t, "1", q.Get("services_mode"))
			} else {
				assert.False(t, q.Has("services_mode"))
			}
		})
	}
}

func TestMetricsDisplayOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "all in bytes",
			args: []string{"metrics:all", "--bytes", "--no-header", "-c", "service,mem_used,disk_used,tmp_disk_limit"},
			want: "app,256,100,1000\ndb,256,250,1000\n",
		},
		{
			name: "all in units",
			args: []string{"metrics:all", "--no-header", "-c", "service,mem_used,disk_used"},
			want: "app,256 B,100 B\ndb,256 B,250 B\n",
		},
		{
			name: "temporary disk usage",
			args: []string{"disk", "--tmp", "--bytes"},
			want: "Timestamp,Service,/tmp used,/tmp limit,/tmp %,/tmp inodes %\n" +
				"2026-09-23T19:00:00+00:00,app,500,1000,50.0%,4.0%\n" +
				"2026-09-23T19:00:00+00:00,db,10,1000,1.0%,0.1%\n",
		},
		{
			name: "temporary disk usage with columns",
			args: []string{"disk", "--tmp", "--bytes", "--no-header", "--columns", "service,tmp_used,tmp_iused"},
			want: "app,500,40\ndb,10,1\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, _, projectID := setupMetricsTest(t, false, false)
			args := append(slices.Clone(c.args), "-p", projectID, "-e", "main", "--format", "csv", "--date-fmt", "c")
			stdout, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, stderr)
			assert.Equal(t, c.want, stdout)
		})
	}
}
