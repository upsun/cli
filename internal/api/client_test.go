package api

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testError struct{}

func (testError) Error() string { return "test" }

func TestError_Unwrap(t *testing.T) {
	err := error(Error{Original: testError{}, URL: "https://api.example.com"})
	var te testError
	assert.True(t, errors.As(err, &te), "the original error must be in the chain")
	assert.Nil(t, errors.Unwrap(Error{URL: "https://api.example.com"}))
}
