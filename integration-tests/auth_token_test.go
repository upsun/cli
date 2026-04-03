package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthToken_PrintsToken(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	out := f.Run("auth:token", "--no-warn")
	assert.Equal(t, "access-token-1", strings.TrimSpace(out))
}

func TestAuthToken_HeaderFlag(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	out := f.Run("auth:token", "--no-warn", "--header")
	assert.Equal(t, "Authorization: Bearer access-token-1", strings.TrimSpace(out))
}

func TestAuthToken_WarnsByDefault(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	_, stderr, err := f.RunCombinedOutput("auth:token")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Warning")
}
