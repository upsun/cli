package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadFileWhileReplaced reads a file while another goroutine replaces it, which fails a plain read with a
// sharing violation on Windows.
func TestReadFileWhileReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, WriteFileAtomic(path, []byte("{}")))

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Go(func() {
		defer close(done)
		for range 500 {
			if err := WriteFileAtomic(path, []byte("{}")); err != nil {
				t.Error(err)
				return
			}
		}
	})
	var plainErrors int
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		if _, err := os.ReadFile(path); err != nil {
			plainErrors++
		}
		b, err := ReadFile(path)
		if !assert.NoError(t, err) {
			break
		}
		assert.Equal(t, "{}", string(b))
	}
	wg.Wait()
	t.Logf("%d plain reads failed", plainErrors)
}
