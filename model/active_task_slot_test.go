package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestActiveTaskSlotsEnforceLimitsAndReleaseIdempotently(t *testing.T) {
	manager := newActiveTaskSlotManager(2, 1, time.Hour)
	first, err := manager.Acquire(1, "one", "video-model", []byte(`{"prompt":"one"}`))
	require.NoError(t, err)
	_, err = manager.Acquire(1, "one", "video-model", []byte(`{"prompt":"two"}`))
	require.ErrorIs(t, err, ErrActiveTaskUserLimit)

	second, err := manager.Acquire(2, "two", "video-model", []byte(`{"prompt":"three"}`))
	require.NoError(t, err)
	_, err = manager.Acquire(3, "three", "video-model", []byte(`{"prompt":"four"}`))
	require.ErrorIs(t, err, ErrActiveTaskGlobalLimit)

	require.True(t, manager.AttachTaskID(first.Token, "task_first"))
	require.True(t, manager.ReleaseByTaskID("task_first"))
	require.False(t, manager.ReleaseByTaskID("task_first"))
	require.True(t, manager.Release(second.Token))
	require.Equal(t, 0, manager.Stats().GlobalActiveSlots)
}

func TestActiveTaskSlotsExpireAndBoundProfiles(t *testing.T) {
	manager := newActiveTaskSlotManager(10, 10, time.Minute)
	slot, err := manager.Acquire(1, "one", "video-model", []byte(`{"prompt":"stable phrase"}`))
	require.NoError(t, err)

	manager.mu.Lock()
	manager.slots[slot.Token].ExpiresAt = time.Now().Add(-time.Second)
	manager.mu.Unlock()
	require.Equal(t, 1, manager.SweepExpired())
	require.Equal(t, 0, manager.Stats().GlobalActiveSlots)

	// Similar normalized input produces a deterministic SimHash; no request
	// body is retained in the profile map.
	require.Equal(t,
		activeTaskSimHash("video-model", []byte(`{"prompt":"hello world"}`)),
		activeTaskSimHash("video-model", []byte(`{"prompt":"hello world"}`)),
	)
}

func TestActiveTaskActivityIsSeparateFromAsyncSlots(t *testing.T) {
	manager := newActiveTaskSlotManager(10, 10, time.Minute)
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte(`{"messages":[{"content":"hello world"}]}`)))
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte(`{"messages":[{"content":"hello world"}]}`)))

	activity := manager.ActivityStats(30 * time.Second)
	require.Equal(t, 1, activity.GlobalActiveSlots)
	require.Equal(t, 1, activity.ActiveUsers)
	require.Equal(t, 1, activity.Rank[0].ActiveSlots)
	require.Equal(t, 0, manager.Stats().GlobalActiveSlots)

	slot, err := manager.Acquire(1, "one", "video-model", []byte(`{"prompt":"video"}`))
	require.NoError(t, err)
	require.Equal(t, 1, manager.Stats().GlobalActiveSlots)
	require.Equal(t, 1, manager.ActivityStats(30*time.Second).GlobalActiveSlots)
	require.True(t, manager.Release(slot.Token))
}

func TestActiveTaskActivitySupportsLegacyQueryWindows(t *testing.T) {
	manager := newActiveTaskSlotManager(10, 10, time.Minute)
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte(`{"messages":[{"content":"hello"}]}`)))

	manager.mu.Lock()
	for _, profile := range manager.profiles {
		profile.lastSeen = time.Now().Add(-30 * time.Minute)
	}
	manager.mu.Unlock()

	require.Equal(t, 0, manager.ActivityStats(30*time.Second).GlobalActiveSlots)
	require.Equal(t, 1, manager.ActivityStats(time.Hour).GlobalActiveSlots)
}

func TestActiveTaskActivityPathMatching(t *testing.T) {
	for _, path := range []string{
		"/v1/chat/completions",
		"/chat/completions",
		"/v1/completions",
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages",
		"/v1beta/models/gemini-2.5-flash:generateContent",
	} {
		require.True(t, isActiveTaskActivityPath(path), path)
	}
	for _, path := range []string{"/v1/embeddings", "/v1/images/generations"} {
		require.False(t, isActiveTaskActivityPath(path), path)
	}
}

func TestHighActiveTaskRecordAutoMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:active-task-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&HighActiveTaskRecord{}))
	require.True(t, db.Migrator().HasTable(&HighActiveTaskRecord{}))
	require.True(t, db.Migrator().HasIndex(&HighActiveTaskRecord{}, "idx_active_task_history_created_user"))
}
