package mockapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergePatch(t *testing.T) {
	cases := []struct {
		name  string
		data  map[string]any
		patch map[string]any
		want  map[string]any
	}{
		{
			name:  "nested merge",
			data:  map[string]any{"a": map[string]any{"b": 1, "c": 2}},
			patch: map[string]any{"a": map[string]any{"b": 3}},
			want:  map[string]any{"a": map[string]any{"b": 3, "c": 2}},
		},
		{
			name:  "null deletes",
			data:  map[string]any{"a": map[string]any{"b": 1, "c": 2}, "d": 4},
			patch: map[string]any{"a": map[string]any{"b": nil}, "d": nil},
			want:  map[string]any{"a": map[string]any{"c": 2}},
		},
		{
			name:  "nested null in a new object",
			data:  nil,
			patch: map[string]any{"services": map[string]any{"app": map[string]any{"memory": nil, "cpu": 1}}},
			want:  map[string]any{"services": map[string]any{"app": map[string]any{"cpu": 1}}},
		},
		{
			name:  "object replaces a scalar",
			data:  map[string]any{"a": 1},
			patch: map[string]any{"a": map[string]any{"b": nil, "c": 2}},
			want:  map[string]any{"a": map[string]any{"c": 2}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, mergePatch(c.data, c.patch))
		})
	}
}
