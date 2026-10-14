package api

import (
	"testing"
	"time"

	clover "github.com/ostafen/clover/v2"
)

func TestUsagePersistsAcrossRestart(t *testing.T) {
	// Arrange: a usage tracker over a fresh database, fed three hits.
	dir := t.TempDir()
	db, err := clover.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	u1 := newUsageTracker(db)
	u1.hit("GET /api/tables/Products/rows", time.Now())
	u1.hit("GET /api/tables/Products/rows", time.Now())
	u1.hit("GET /api/tables/Big/rows", time.Now())
	u1.flush()

	// Act: "restart" - a fresh tracker over the same database picks up the
	// flushed aggregates.
	u2 := newUsageTracker(db)
	total, byPath := u2.since(time.Hour)

	// Assert
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if byPath["GET /api/tables/Products/rows"] != 2 {
		t.Fatalf("byPath = %v", byPath)
	}
}
