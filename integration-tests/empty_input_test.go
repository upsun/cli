package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestEmptyInteractiveInput checks that pressing Enter at prompts with no
// default is reported as a validation error rather than a crash.
func TestEmptyInteractiveInput(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	cases := []struct {
		name  string
		stdin string
		args  []string
		env   []string
		want  string
	}{
		{
			name:  "auth:api-token-login",
			stdin: "\n",
			args:  []string{"auth:api-token-login"},
			env:   []string{EnvPrefix + "TOKEN="},
			want:  "The token cannot be empty",
		},
		{
			name: "auth:verify-phone-number",
			// Accept the default channel, then submit an empty phone number.
			stdin: "\n\n",
			args:  []string{"auth:verify-phone-number"},
			want:  "A phone number is required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.extraEnv = c.env
			stdout, stderr, err := f.RunInteractive(c.stdin, c.args...)
			require.Error(t, err)
			assert.NotContains(t, stderr, "TypeError")
			assert.NotContains(t, stderr, "must be of type")
			assert.Contains(t, stderr, c.want, "stdout: %s\nstderr: %s", stdout, stderr)
		})
	}
}
