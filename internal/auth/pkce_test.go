package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/auth"
)

func TestGenerateVerifier(t *testing.T) {
	v1, err := auth.GenerateVerifier()
	require.NoError(t, err)
	v2, err := auth.GenerateVerifier()
	require.NoError(t, err)

	assert.Len(t, v1, 43) // 32 bytes base64url = 43 chars (no padding)
	assert.NotEqual(t, v1, v2)
	assert.False(t, strings.ContainsAny(v1, "+/="), "must be base64url, not standard base64")
}

func TestVerifierToChallenge(t *testing.T) {
	// RFC 7636 §B test vector
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := auth.VerifierToChallenge(verifier)
	assert.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", challenge)
}
