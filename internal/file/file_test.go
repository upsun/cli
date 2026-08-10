package file

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteIfNeeded(t *testing.T) {
	largeContentLength := 128 * 1024
	largeContent := make([]byte, largeContentLength)
	largeContent[0] = 'f'
	largeContent[largeContentLength-2] = 'o'
	largeContent[largeContentLength-1] = 'o'

	largeContent2 := make([]byte, largeContentLength)
	largeContent2[0] = 'b'
	largeContent2[largeContentLength-2] = 'a'
	largeContent2[largeContentLength-1] = 'r'

	assert.Equal(t, len(largeContent), len(largeContent2))

	cases := []struct {
		name        string
		initialData []byte
		sourceData  []byte
		expectWrite bool
	}{
		{"File does not exist", nil, []byte("new data"), true},
		{"File matches source", []byte("same data"), []byte("same data"), false},
		{"File content differs", []byte("old data"), []byte("new data"), true},
		{"Larger file content differs", largeContent, largeContent2, true},
		{"Larger file content matches", largeContent, largeContent, false},
		{"File size differs", []byte("short"), []byte("much longer data"), true},
		{"Empty source", []byte("existing data"), []byte{}, true},
	}

	tmpDir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			destFile := filepath.Join(tmpDir, "testfile")

			if c.initialData != nil {
				require.NoError(t, os.WriteFile(destFile, c.initialData, 0o600))
				time.Sleep(time.Millisecond * 5)
			}

			var modTimeBefore time.Time
			stat, err := os.Stat(destFile)
			if c.initialData == nil {
				require.True(t, os.IsNotExist(err))
			} else {
				require.NoError(t, err)
				modTimeBefore = stat.ModTime()
			}

			err = WriteIfNeeded(destFile, c.sourceData, 0o600)
			require.NoError(t, err)

			statAfter, err := os.Stat(destFile)
			require.NoError(t, err)
			modTimeAfter := statAfter.ModTime()

			if c.expectWrite {
				assert.Greater(t, modTimeAfter.Truncate(time.Millisecond), modTimeBefore.Truncate(time.Millisecond))
			} else {
				assert.Equal(t, modTimeBefore.Truncate(time.Millisecond), modTimeAfter.Truncate(time.Millisecond))
			}

			data, err := os.ReadFile(destFile)
			require.NoError(t, err)

			assert.True(t, bytes.Equal(data, c.sourceData))
		})
	}
}

func TestWriteIfChangedChecksTheWholeFile(t *testing.T) {
	destFile := filepath.Join(t.TempDir(), "dynamic-file")
	original := bytes.Repeat([]byte{'a'}, 64*1024)
	updated := bytes.Clone(original)
	updated[0] = 'b'

	require.NoError(t, os.WriteFile(destFile, original, 0o600))
	require.NoError(t, WriteIfChanged(destFile, updated, 0o600))
	actual, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, updated, actual,
		"equal size and matching final 32 KB must not hide an earlier change")

	before, err := os.Stat(destFile)
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, WriteIfChanged(destFile, updated, 0o600))
	after, err := os.Stat(destFile)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "matching contents should not be rewritten")
}
