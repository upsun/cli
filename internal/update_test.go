package internal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/state"
)

func TestNotificationFromState(t *testing.T) {
	const now = 1_700_000_000

	newState := func(latest string, lastNotified int64) state.State {
		var s state.State
		s.Updates.KnownLatestVersion = latest
		s.Updates.LastNotified = lastNotified
		return s
	}

	t.Run("newer version not yet notified", func(t *testing.T) {
		rel := notificationFromState(newState("v2.0.0", 0), "upsun/cli", "v1.0.0", now)
		if assert.NotNil(t, rel) {
			assert.Equal(t, "v2.0.0", rel.Version)
			assert.Equal(t, "https://github.com/upsun/cli/releases/tag/v2.0.0", rel.URL)
		}
	})

	t.Run("no cached version", func(t *testing.T) {
		assert.Nil(t, notificationFromState(newState("", 0), "upsun/cli", "v1.0.0", now))
	})

	t.Run("cached version not newer", func(t *testing.T) {
		assert.Nil(t, notificationFromState(newState("v1.0.0", 0), "upsun/cli", "v1.0.0", now))
		assert.Nil(t, notificationFromState(newState("v0.9.0", 0), "upsun/cli", "v1.0.0", now))
	})

	t.Run("notified within the past week is throttled", func(t *testing.T) {
		lastNotified := int64(now - 3*24*60*60) // 3 days ago
		assert.Nil(t, notificationFromState(newState("v2.0.0", lastNotified), "upsun/cli", "v1.0.0", now))
	})

	t.Run("notified over a week ago shows again", func(t *testing.T) {
		lastNotified := int64(now - 8*24*60*60) // 8 days ago
		assert.NotNil(t, notificationFromState(newState("v2.0.0", lastNotified), "upsun/cli", "v1.0.0", now))
	})

	t.Run("notifies when last notified is in the future", func(t *testing.T) {
		lastNotified := int64(now + 30*24*60*60) // clock skew
		assert.NotNil(t, notificationFromState(newState("v2.0.0", lastNotified), "upsun/cli", "v1.0.0", now))
	})
}

func TestUpdateCheckWait(t *testing.T) {
	t.Run("nil check returns immediately", func(t *testing.T) {
		var c *UpdateCheck
		start := time.Now()
		c.Wait(time.Second)
		assert.Less(t, time.Since(start), 100*time.Millisecond)
	})

	t.Run("returns when the check finishes", func(t *testing.T) {
		c := &UpdateCheck{done: make(chan struct{})}
		go func() {
			time.Sleep(10 * time.Millisecond)
			close(c.done)
		}()
		start := time.Now()
		c.Wait(5 * time.Second)
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("gives up after the timeout", func(t *testing.T) {
		c := &UpdateCheck{done: make(chan struct{})}
		start := time.Now()
		c.Wait(50 * time.Millisecond)
		assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	})
}
