package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/registry"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b     string
		expected int
	}{
		{"8.4", "8.4", 0},
		{"8.10", "8.4", 1},
		{"8.4", "9.0", -1},
		{"26.05", "25.11", 1},
		{"8.4", "8.4.1", -1},
		{"17", "16", 1},
	}
	for _, c := range cases {
		t.Run(c.a+"_"+c.b, func(t *testing.T) {
			assert.Equal(t, c.expected, max(-1, min(1, registry.CompareVersions(c.a, c.b))))
		})
	}
}
