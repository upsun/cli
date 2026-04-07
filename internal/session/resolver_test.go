package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitiseID_AlphanumericPassthrough(t *testing.T) {
	assert.Equal(t, "abc123", sanitiseID("abc123"))
}

func TestSanitiseID_UnderscoreAndHyphenPassthrough(t *testing.T) {
	assert.Equal(t, "my_session-1", sanitiseID("my_session-1"))
}

func TestSanitiseID_SpaceReplacedWithHyphen(t *testing.T) {
	assert.Equal(t, "my-session", sanitiseID("my session"))
}

func TestSanitiseID_ConsecutiveInvalidCollapsedToSingleHyphen(t *testing.T) {
	// PHP: preg_replace('/[^\w\-]+/', '-', $id) — multiple invalid chars → one hyphen
	assert.Equal(t, "a-b", sanitiseID("a   b"))
	assert.Equal(t, "a-b", sanitiseID("a@#$b"))
	assert.Equal(t, "a-b", sanitiseID("a!@#b"))
}

func TestSanitiseID_UnicodeReplacedWithHyphen(t *testing.T) {
	assert.Equal(t, "caf-au-lait", sanitiseID("café au lait"))
}

func TestSanitiseID_EmptyString(t *testing.T) {
	assert.Equal(t, "", sanitiseID(""))
}

func TestSanitiseID_OnlyInvalidChars(t *testing.T) {
	// All invalid chars collapse to a single hyphen
	assert.Equal(t, "-", sanitiseID("@#$"))
}

func TestSessionDirName(t *testing.T) {
	assert.Equal(t, "sess-default", sessionDirName("default"))
	assert.Equal(t, "sess-my-session", sessionDirName("my session"))
}

func TestCLIDirName(t *testing.T) {
	assert.Equal(t, "sess-cli-default", cliDirName("default"))
	assert.Equal(t, "sess-cli-my-session", cliDirName("my session"))
}
