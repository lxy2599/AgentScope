package adapter

import (
	"testing"
	"time"
)

func TestOptionalEventTimeDropsClickHouseEpochSentinel(t *testing.T) {
	epoch := time.Unix(0, 0).UTC()
	if got := optionalEventTime(&epoch); got != nil {
		t.Fatalf("optionalEventTime(epoch) = %v, want nil", got)
	}

	actual := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	got := optionalEventTime(&actual)
	if got == nil || !got.Equal(actual) {
		t.Fatalf("optionalEventTime(actual) = %v, want %v", got, actual)
	}
}
