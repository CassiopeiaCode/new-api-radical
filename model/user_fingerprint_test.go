package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFingerprintTestDB(t *testing.T) {
	t.Helper()
	oldDB, oldLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:fingerprint-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &UserFingerprint{}))
	require.True(t, db.Migrator().HasTable(&UserFingerprint{}))
	require.True(t, db.Migrator().HasIndex(&UserFingerprint{}, "ux_user_fingerprints_user_visitor_ip"))
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogDB })
}

func createFingerprintTestUser(t *testing.T, id int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: id, Username: fmt.Sprintf("fp-user-%d", id), Password: "password1", DisplayName: "Fingerprint user", AffCode: fmt.Sprintf("fp%06d", id)}).Error)
}

func TestRecordFingerprintUpsertsPairAndTrimsOldest(t *testing.T) {
	setupFingerprintTestDB(t)
	createFingerprintTestUser(t, 1)

	require.NoError(t, RecordFingerprint(1, "visitor_a", "first-agent", "203.0.113.1"))
	require.NoError(t, RecordFingerprint(1, "visitor_a", "updated-agent", "203.0.113.1"))
	var count int64
	require.NoError(t, DB.Model(&UserFingerprint{}).Count(&count).Error)
	require.Equal(t, int64(1), count)

	for i := 0; i < MaxFingerprintsPerUser+1; i++ {
		require.NoError(t, RecordFingerprint(1, fmt.Sprintf("visitor_%d", i), "agent", fmt.Sprintf("203.0.113.%d", i+10)))
		time.Sleep(time.Millisecond)
	}
	items, err := GetUserFingerprints(1)
	require.NoError(t, err)
	require.Len(t, items, MaxFingerprintsPerUser)
	require.Equal(t, "visitor_5", items[0].VisitorId)
	for _, item := range items {
		require.NotEqual(t, "visitor_a", item.VisitorId)
	}
}

func TestFingerprintAssociationQueriesUseVisitorAndIP(t *testing.T) {
	setupFingerprintTestDB(t)
	createFingerprintTestUser(t, 1)
	createFingerprintTestUser(t, 2)
	createFingerprintTestUser(t, 3)
	require.NoError(t, RecordFingerprint(1, "shared_visitor", "agent", "2001:db8::1"))
	require.NoError(t, RecordFingerprint(2, "shared_visitor", "agent", "2001:db8::1"))
	require.NoError(t, RecordFingerprint(3, "shared_visitor", "agent", "2001:db8::2"))

	page := &common.PageInfo{Page: 1, PageSize: 100}
	duplicates, total, err := GetDuplicateFingerprints(page)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, duplicates, 1)
	require.Equal(t, "2001:db8::1", duplicates[0].IP)
	require.Equal(t, int64(2), duplicates[0].UserCount)
	require.Positive(t, duplicates[0].LastSeen)

	users, total, err := FindUsersByFingerprint("shared_visitor", "2001:db8::1", page)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, users, 2)
}

func TestValidateFingerprintInput(t *testing.T) {
	_, _, _, err := ValidateFingerprintInput("visitor ok", "203.0.113.1", "agent")
	require.Error(t, err)
	_, _, _, err = ValidateFingerprintInput("visitor_ok", "not-an-ip", "agent")
	require.Error(t, err)
	visitor, ip, userAgent, err := ValidateFingerprintInput("visitor-ok_1", "203.0.113.1", string(make([]byte, maxFingerprintUserAgent+10)))
	require.NoError(t, err)
	require.Equal(t, "visitor-ok_1", visitor)
	require.Equal(t, "203.0.113.1", ip)
	require.Len(t, userAgent, maxFingerprintUserAgent)
}
