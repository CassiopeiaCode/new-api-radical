package controller

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRandomRedemptionRangeCapacity(t *testing.T) {
	if !randomRangeHasCapacity(10, 12, 3) {
		t.Fatal("inclusive range of three values should fit three codes")
	}
	if randomRangeHasCapacity(10, 12, 4) {
		t.Fatal("inclusive range of three values must not fit four codes")
	}
	if !randomRangeHasCapacity(math.MinInt64, math.MaxInt64, 100000) {
		t.Fatal("full int64 range should support a large batch without overflow")
	}
}

func TestRandomRedemptionKeyMatchesOrdinaryCodeLength(t *testing.T) {
	for _, prefix := range []string{"", "SUMMER-", "活动-"} {
		first, err := generateRandomRedemptionKey(prefix)
		if err != nil {
			t.Fatalf("generate key with prefix %q: %v", prefix, err)
		}
		second, err := generateRandomRedemptionKey(prefix)
		if err != nil {
			t.Fatalf("generate second key with prefix %q: %v", prefix, err)
		}
		if utf8.RuneCountInString(first) != redemptionKeyLength {
			t.Fatalf("key length = %d, want %d: %q", utf8.RuneCountInString(first), redemptionKeyLength, first)
		}
		if !strings.HasPrefix(first, prefix) {
			t.Fatalf("key %q does not preserve prefix %q", first, prefix)
		}
		if first == second {
			t.Fatalf("two generated keys unexpectedly match: %q", first)
		}
	}
}

func TestCryptoRandInt64Inclusive(t *testing.T) {
	for range 20 {
		value, err := cryptoRandInt64Inclusive(-2, 2)
		if err != nil {
			t.Fatalf("unexpected random error: %v", err)
		}
		if value < -2 || value > 2 {
			t.Fatalf("value %d is outside inclusive range", value)
		}
	}
}
