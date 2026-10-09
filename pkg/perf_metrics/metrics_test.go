package perfmetrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthTrendsForFiveMinuteBuckets(t *testing.T) {
	const bucketSeconds int64 = 300
	current := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC).Unix()
	buckets := map[int64]counters{}

	for i := int64(0); i < 288; i++ {
		buckets[current-i*bucketSeconds] = counters{requestCount: 10, successCount: 10}
	}
	// Current 5-minute bucket: 50%.
	buckets[current] = counters{requestCount: 10, successCount: 5}
	// A bucket inside the last hour: 80%.
	buckets[current-11*bucketSeconds] = counters{requestCount: 10, successCount: 8}
	// A bucket inside 24h but outside 1h: 0%.
	buckets[current-12*bucketSeconds] = counters{requestCount: 10, successCount: 0}

	trends := healthTrends(buckets, current, bucketSeconds)
	require.NotNil(t, trends.Last5Minutes)
	require.NotNil(t, trends.LastHour)
	require.NotNil(t, trends.Last24Hours)
	require.Equal(t, 50.0, *trends.Last5Minutes)
	require.Equal(t, 94.17, *trends.LastHour)
	require.Equal(t, 99.41, *trends.Last24Hours)
}

func TestHealthTrendsUseNilForEmptyWindows(t *testing.T) {
	const bucketSeconds int64 = 300
	current := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC).Unix()
	buckets := map[int64]counters{
		current - 2*int64(time.Hour/time.Second): {requestCount: 4, successCount: 3},
	}

	trends := healthTrends(buckets, current, bucketSeconds)
	require.NotNil(t, trends.Last24Hours)
	require.Equal(t, 75.0, *trends.Last24Hours)
	require.Nil(t, trends.LastHour)
	require.Nil(t, trends.Last5Minutes)
}

func TestDisplaySuccessRateSelectsHigherAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name    string
		buckets map[int64]counters
		want    float64
	}{
		{"bucket average wins", map[int64]counters{0: {requestCount: 1, successCount: 1}, 300: {requestCount: 9}}, 50},
		{"request weighted wins", map[int64]counters{0: {requestCount: 9, successCount: 9}, 300: {requestCount: 1}}, 90},
		{"empty buckets ignored", map[int64]counters{0: {}, 300: {requestCount: 4, successCount: 3}}, 75},
		{"no traffic", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, displaySuccessRate(tc.buckets)) })
	}
}

func TestHealthTrendWindowsExcludeOlderTraffic(t *testing.T) {
	const now int64 = 86400
	buckets := map[int64]counters{
		now:         {requestCount: 10, successCount: 10},
		now - 1800:  {requestCount: 10},
		now - 7200:  {requestCount: 10},
		now - 25200: {requestCount: 10},
		now - 46800: {requestCount: 10},
	}
	trends := healthTrends(buckets, now, 300)
	require.Equal(t, 20.0, *trends.Last24Hours)
	require.Equal(t, 25.0, *trends.Last12Hours)
	require.Equal(t, 33.33, *trends.Last6Hours)
	require.Equal(t, 50.0, *trends.LastHour)
	require.Equal(t, 100.0, *trends.Last10Minutes)
}
