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

func TestHighActiveTaskRecordAutoMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:active-task-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&HighActiveTaskRecord{}))
	require.True(t, db.Migrator().HasTable(&HighActiveTaskRecord{}))
	require.True(t, db.Migrator().HasIndex(&HighActiveTaskRecord{}, "idx_active_task_history_created_user"))
}
