package store

import (
	"fmt"
	"strings"
	"testing"
)

func setByteBudget(t *testing.T, bytes int) {
	t.Helper()
	oldBytes := MaxCacheBytes
	MaxCacheBytes = bytes
	t.Cleanup(func() { MaxCacheBytes = oldBytes })
}

func bigTable(t *testing.T, s *Store, name string, rows int) {
	t.Helper()
	val := strings.Repeat("v", 1024)
	data := make([]map[string]string, rows)
	for i := range data {
		data[i] = map[string]string{"a": fmt.Sprintf("%d", i), "b": val}
	}
	if err := s.ReplaceTable(name, []Column{{Name: "a", Type: "text"}, {Name: "b", Type: "text"}}, data); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

func cachedBytes(s *Store) (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, entries := range s.rowCache {
		total += cacheTableBytes(entries)
	}
	return total, len(s.rowCache)
}

func TestCacheEvictsByBytes(t *testing.T) {
	s := openTestStore(t)
	setByteBudget(t, 300<<10) // 300KB: one ~235KB table fits, two do not

	for _, name := range []string{"t1", "t2", "t3"} {
		bigTable(t, s, name, 100)
		if _, _, err := s.GetRows(name, RowQuery{Limit: 1000}); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}

	total, tables := cachedBytes(s)
	if total > MaxCacheBytes {
		t.Fatalf("cached bytes = %d, budget = %d", total, MaxCacheBytes)
	}
	if tables == 3 {
		t.Fatalf("all three tables cached despite byte budget")
	}
}

func TestOversizedTableNotCached(t *testing.T) {
	s := openTestStore(t)
	setByteBudget(t, 100<<10) // 100KB < one table's ~235KB

	bigTable(t, s, "big", 100)
	got, total, err := s.GetRows("big", RowQuery{Limit: 1000})
	if err != nil || len(got) != 100 || total != 100 {
		t.Fatalf("reads must work uncached: rows=%d total=%d err=%v", len(got), total, err)
	}

	if n, tables := cachedBytes(s); n != 0 || tables != 0 {
		t.Fatalf("oversized table cached: %d bytes in %d tables", n, tables)
	}
}

func TestPatchInsertRespectsByteBudget(t *testing.T) {
	s := openTestStore(t)
	setByteBudget(t, 250<<10)

	bigTable(t, s, "t", 100)
	if _, _, err := s.GetRows("t", RowQuery{Limit: 1000}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if n, _ := cachedBytes(s); n == 0 {
		t.Fatalf("table should start cached")
	}

	for i := 0; i < 30; i++ {
		if _, err := s.InsertRow("t", map[string]string{"a": fmt.Sprintf("%d", i), "b": strings.Repeat("w", 1024)}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	if n, _ := cachedBytes(s); n > MaxCacheBytes {
		t.Fatalf("cached bytes = %d, budget = %d", n, MaxCacheBytes)
	}
	got, total, err := s.GetRows("t", RowQuery{Limit: 1000})
	if err != nil || total != 130 || len(got) != 130 {
		t.Fatalf("reads after eviction: rows=%d total=%d err=%v", len(got), total, err)
	}
}
