package controller

import (
	"math"
	"testing"
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
