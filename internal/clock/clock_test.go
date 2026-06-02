package clock

import (
	"testing"
	"time"
)

func TestRealClockReturnsUTC(t *testing.T) {
	t.Parallel()
	now := New().Now()
	if now.Location() != time.UTC {
		t.Fatalf("Real.Now location = %v, want UTC", now.Location())
	}
	if time.Since(now) > time.Minute {
		t.Fatalf("Real.Now looks stale: %v", now)
	}
}

func TestFixedClock(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if got := (Fixed{T: want}).Now(); !got.Equal(want) {
		t.Fatalf("Fixed.Now = %v, want %v", got, want)
	}
}
