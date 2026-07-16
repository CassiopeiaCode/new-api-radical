package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestActiveTaskSimHashSlotsMergeAndRespectLimits(t *testing.T) {
	manager := newActiveTaskSlotManager(2, 1)
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte("hello world")))
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte("hello world")))

	stats := manager.ActivityStats(30 * time.Second)
	require.Equal(t, 1, stats.GlobalActiveSlots)
	require.Equal(t, 1, stats.ActiveUsers)
	require.Equal(t, 1, stats.Rank[0].ActiveSlots)

	// A second distinct request for the same user replaces the user's oldest
	// slot when the legacy per-user LRU limit is reached.
	require.NoError(t, manager.RecordActivity(1, "one", "gpt-test", []byte("entirely different request body")))
	require.Equal(t, 1, manager.ActivityStats(30*time.Second).GlobalActiveSlots)

	// Another user can consume the remaining global temporary slot.
	require.NoError(t, manager.RecordActivity(2, "two", "gpt-test", []byte("another distinct request")))
	require.Equal(t, 2, manager.ActivityStats(30*time.Second).GlobalActiveSlots)
}

func TestActiveTaskActivitySupportsLegacyQueryWindows(t *testing.T) {
	manager := newActiveTaskSlotManager(10, 10)
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
	for _, path := range []string{"/v1/embeddings", "/v1/images/generations", "/v1/videos"} {
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
