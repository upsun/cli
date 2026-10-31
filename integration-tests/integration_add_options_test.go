package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationAddOptions(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setUpIntegrationProject(t)

	syslog := []string{"--type", "syslog", "--syslog-host", "logs.example.com", "--syslog-port", "6514"}
	httplog := []string{"--type", "httplog", "--url", "https://logs.example.com"}
	github := []string{"--type", "github", "--repository", "org/repo", "--token", "gh-token"}
	cases := []struct {
		name     string
		args     []string
		wantBody map[string]any
		wantErr  string
	}{
		{
			name: "syslog defaults",
			args: syslog,
			wantBody: map[string]any{
				"type": "syslog", "host": "logs.example.com", "port": 6514.0, "protocol": "tls", "facility": 1.0,
				"message_format": "rfc5424", "auth_mode": "prefix", "tls_verify": true, "redaction": false,
			},
		},
		{
			name: "syslog options",
			args: append(syslog, "--protocol", "udp", "--facility", "23", "--message-format", "rfc3164",
				"--auth-mode", "structured_data", "--auth-token", "secret", "--verify-tls=false", "--redaction=yes",
				"--excluded-services", "app,db"),
			wantBody: map[string]any{
				"type": "syslog", "host": "logs.example.com", "port": 6514.0, "protocol": "udp", "facility": 23.0,
				"message_format": "rfc3164", "auth_mode": "structured_data", "auth_token": "secret",
				"tls_verify": false, "redaction": true, "excluded_services": []any{"app", "db"},
			},
		},
		{name: "syslog port 0", args: []string{"--type", "syslog", "--syslog-host", "h", "--syslog-port", "0"},
			wantErr: "Invalid port number: 0"},
		{name: "syslog port fraction", args: []string{"--type", "syslog", "--syslog-host", "h", "--syslog-port", "514.5"},
			wantErr: "Invalid port number: 514.5"},
		{name: "syslog port exponent", args: []string{"--type", "syslog", "--syslog-host", "h", "--syslog-port", "5e3"},
			wantErr: "Invalid port number: 5e3"},
		{name: "syslog port too high", args: []string{"--type", "syslog", "--syslog-host", "h", "--syslog-port", "65536"},
			wantErr: "Invalid port number: 65536"},
		{name: "syslog facility too high", args: append(syslog, "--facility", "24"),
			wantErr: "Invalid syslog facility code: 24"},
		{name: "syslog facility fraction", args: append(syslog, "--facility", "2.5"),
			wantErr: "Invalid syslog facility code: 2.5"},
		{name: "syslog facility negative", args: append(syslog, "--facility=-1"),
			wantErr: "Invalid syslog facility code: -1"},
		{name: "syslog protocol", args: append(syslog, "--protocol", "http"), wantErr: "http is not one of: tcp, udp, tls"},
		{name: "syslog message format", args: append(syslog, "--message-format", "rfc1234"),
			wantErr: "rfc1234 is not one of: rfc3164, rfc5424"},
		{name: "syslog auth mode", args: append(syslog, "--auth-mode", "none"),
			wantErr: "none is not one of: prefix, structured_data"},
		{name: "verify-tls", args: append(syslog, "--verify-tls", "maybe"), wantErr: "Invalid value for 'Verify TLS': maybe"},
		{
			name: "httplog headers",
			args: append(httplog, "--header", "Authorization: Bearer abc", "--header", "X-List : a, b"),
			wantBody: map[string]any{"type": "httplog", "headers": map[string]any{"Authorization": "Bearer abc",
				"X-List": "a, b"}},
		},
		{name: "httplog header without a value", args: append(httplog, "--header", "X-Empty"),
			wantErr: "Invalid header (no value): X-Empty"},
		{
			name:    "httplog duplicate header",
			args:    append(httplog, "--header", "X-Key: a", "--header", "x-key: b"),
			wantErr: "Duplicate header name: x-key",
		},
		{
			name: "splunk",
			args: []string{"--type", "splunk", "--url", "https://splunk.example.com", "--token", "t", "--index", "main",
				"--sourcetype", "st"},
			wantBody: map[string]any{"type": "splunk", "index": "main", "sourcetype": "st", "token": "t"},
		},
		{
			name:     "sumologic",
			args:     []string{"--type", "sumologic", "--url", "https://sumo.example.com", "--category", "prod"},
			wantBody: map[string]any{"type": "sumologic", "category": "prod"},
		},
		{
			name:     "newrelic",
			args:     []string{"--type", "newrelic", "--url", "https://nr.example.com", "--license-key", "lk"},
			wantBody: map[string]any{"type": "newrelic", "license_key": "lk"},
		},
		{
			name:    "option of another type",
			args:    []string{"--type", "webhook", "--url", "https://hooks.example.com", "--syslog-host", "h"},
			wantErr: "The option --syslog-host cannot be used with the integration type webhook.",
		},
		{
			name:    "resync-pull-requests on GitHub",
			args:    append(github, "--resync-pull-requests=true"),
			wantErr: "The option --resync-pull-requests cannot be used with the integration type github.",
		},
		{
			name:     "fetch-branches false implies prune-branches false",
			args:     append(github, "--fetch-branches=false"),
			wantBody: map[string]any{"type": "github", "fetch_branches": false, "prune_branches": false},
		},
		{
			name: "build options",
			args: append(github, "--build-draft-pull-requests=false", "--build-pull-requests-post-merge=true",
				"--pull-requests-clone-parent-data=false"),
			wantBody: map[string]any{
				"type": "github", "build_pull_requests": true, "build_draft_pull_requests": false,
				"build_pull_requests_post_merge": true, "pull_requests_clone_parent_data": false,
			},
		},
		{
			name:    "build option that depends on another",
			args:    append(github, "--build-pull-requests=false", "--build-draft-pull-requests=true"),
			wantErr: "The option --build-draft-pull-requests can only be used if --build-pull-requests is true.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			before := len(apiHandler.Requests("POST", "/projects/*/integrations"))
			args := append([]string{"integration:add", "-p", projectID, "--no-wait"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			requests := apiHandler.Requests("POST", "/projects/*/integrations")
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				assert.Len(t, requests, before)
				return
			}
			require.NoError(t, err, stderr)
			require.Len(t, requests, before+1)
			body := jsonObject(t, requests[before].Body)
			for k, v := range c.wantBody {
				assert.Equal(t, v, body[k], k)
			}
		})
	}
}
