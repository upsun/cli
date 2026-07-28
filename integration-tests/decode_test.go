package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecode(t *testing.T) {
	f := &cmdFactory{t: t}

	// Simple base64-encoded JSON.
	assertTrimmed(t, `{
    "foo": "bar"
}`, f.Run("decode", "eyJmb28iOiAiYmFyIn0="))

	// Property extraction.
	assertTrimmed(t, "bar", f.Run("decode", "eyJmb28iOiAiYmFyIn0=", "-P", "foo"))

	// Nested property extraction.
	input := "eyJhIjogeyJiIjogImMifX0=" // {"a": {"b": "c"}}
	assertTrimmed(t, "c", f.Run("decode", input, "-P", "a.b"))

	// Invalid base64 input.
	_, stdErr, err := f.RunCombinedOutput("decode", "not-valid-base64!")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Invalid value")
}
