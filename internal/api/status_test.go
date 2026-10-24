package api

import (
	"testing"
	"time"
)

// a local-zone time must format as the UTC instant, never local
// wall numbers wearing a Z
func TestSinceRFC3339ConvertsToUTC(t *testing.T) {
	bangkok := time.FixedZone("+07", 7*3600)
	local := time.Date(2026, 10, 23, 13, 0, 0, 0, bangkok) // 13:00 Bangkok = 06:00 UTC
	got := SinceRFC3339(local)
	want := "2026-10-23T06:00:00Z"
	if got != want {
		t.Fatalf("SinceRFC3339(+0700 13:00) = %q, want %q", got, want)
	}
}
