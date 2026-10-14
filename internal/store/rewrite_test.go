package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// stageCrash builds a complete staging copy and its manifest, then stops -
// the state a crash right before finishSwap leaves behind.
func stageCrash(t *testing.T, s *Store, name string, columns []Column, docsFrom string, colRename [2]string, from string) {
	t.Helper()
	docs, err := s.copyDocs(docsFrom, colRename)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	staging := stagingPrefix + name
	if err := s.db.CreateCollection(staging); err != nil {
		t.Fatalf("staging: %v", err)
	}
	if err := s.saveMeta(staging, columns); err != nil {
		t.Fatalf("staging meta: %v", err)
	}
	if err := s.db.Insert(staging, docs...); err != nil {
		t.Fatalf("staging insert: %v", err)
	}
	if err := s.markSwap(name, true, from); err != nil {
		t.Fatalf("manifest: %v", err)
	}
}

func rowIDs(t *testing.T, s *Store, table string) map[string]string {
	t.Helper()
	rows, _, err := s.GetRows(table, RowQuery{Limit: 1000})
	if err != nil {
		t.Fatalf("rows of %s: %v", table, err)
	}
	ids := map[string]string{}
	for _, r := range rows {
		ids[r["id"]] = r["name"] + r["full"]
	}
	return ids
}

func TestRecoverColumnRenameKeepsRowIDs(t *testing.T) {
	// Arrange: a store with two rows, then a staged column rename whose
	// swap never ran (the crash state).
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.ReplaceTable("People", cols("name"), []map[string]string{{"name": "amy"}, {"name": "bob"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := rowIDs(t, s, "People")
	stageCrash(t, s, "People", cols("full"), "People", [2]string{"name", "full"}, "")
	s.Close()

	// Act: reopen - boot recovery must finish the staged swap.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: the same rows with the same ids under the renamed column.
	after := rowIDs(t, s2, "People")
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("recovery must keep row ids: before %v after %v", before, after)
	}
	if got, _ := s2.GetColumns("People"); len(got) != 1 || got[0] != "full" {
		t.Fatalf("renamed column lost: %v", got)
	}
}

func TestRecoverTableRenameDropsOldName(t *testing.T) {
	// Arrange: a table Old, then a staged rename to New whose swap never
	// ran (the crash state).
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.ReplaceTable("Old", cols("name"), []map[string]string{{"name": "amy"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := rowIDs(t, s, "Old")
	stageCrash(t, s, "New", cols("name"), "Old", [2]string{}, "Old")
	s.Close()

	// Act: reopen - boot recovery must finish the rename.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: New holds the rows, Old and its metadata are gone.
	if fmt.Sprint(rowIDs(t, s2, "New")) != fmt.Sprint(before) {
		t.Fatal("renamed table must keep its rows and ids")
	}
	if exists, _ := s2.TableExists("Old"); exists {
		t.Fatal("old name must be dropped by recovery")
	}
	if meta, _ := s2.findMeta("Old"); meta != nil {
		t.Fatal("old name's metadata must be dropped by recovery")
	}
}

func TestRenameTableKeepsIDsAndLeavesNoScaffolding(t *testing.T) {
	// Arrange: a store with a two-row table.
	s := openTestStore(t)
	if err := s.ReplaceTable("Old", cols("name"), []map[string]string{{"name": "amy"}, {"name": "bob"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := rowIDs(t, s, "Old")

	// Act: rename the table.
	if err := s.RenameTable("Old", "New"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Assert: rows and ids survived and nothing was left behind.
	if fmt.Sprint(rowIDs(t, s, "New")) != fmt.Sprint(before) {
		t.Fatal("rename must keep row ids")
	}
	if exists, _ := s.db.HasCollection(stagingPrefix + "New"); exists {
		t.Fatal("staging left behind")
	}
	if _, ok, _ := s.GetSetting(swapManifest); ok {
		t.Fatal("manifest left behind")
	}
	if meta, _ := s.findMeta("Old"); meta != nil {
		t.Fatal("old metadata left behind")
	}
}

// Row writes racing whole-table rewrites must all survive, and concurrent
// rewrites of different tables must each finish cleanly.
func TestRowWritesSurviveConcurrentRewrites(t *testing.T) {
	// Arrange: two tables, one writer inserting into A, one goroutine
	// rewriting A's columns, one rewriting B wholesale.
	s := openTestStore(t)
	if err := s.ReplaceTable("A", cols("name"), []map[string]string{{"name": "seed"}}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := s.ReplaceTable("B", cols("x"), []map[string]string{{"x": "1"}}); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	const inserts = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < inserts; i++ {
			if _, err := s.InsertRow("A", map[string]string{}); err != nil {
				t.Errorf("insert: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		names := []string{"name", "label"}
		for i := 0; i < 10; i++ {
			if err := s.RenameColumn("A", names[i%2], names[(i+1)%2]); err != nil {
				t.Errorf("rename column: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if err := s.ReplaceTable("B", cols("x"), []map[string]string{{"x": fmt.Sprint(i)}}); err != nil {
				t.Errorf("replace B: %v", err)
				return
			}
		}
	}()

	// Act: let the three goroutines race to completion.
	wg.Wait()

	// Assert: no rows lost, no table damaged, no manifest left behind.
	if n, _ := s.CountRows("A"); n != inserts+1 {
		t.Fatalf("rows lost to a rewrite: have %d, want %d", n, inserts+1)
	}
	if n, _ := s.CountRows("B"); n != 1 {
		t.Fatalf("B damaged: %d rows", n)
	}
	if _, ok, _ := s.GetSetting(swapManifest); ok {
		t.Fatal("manifest left behind")
	}
}

func TestSingleRowResponsesCarryEveryColumn(t *testing.T) {
	// Arrange: a table with one row, then a column added after the fact.
	s := openTestStore(t)
	if err := s.ReplaceTable("T", cols("a"), []map[string]string{{"a": "1"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.AddColumn("T", "note", ""); err != nil {
		t.Fatalf("add column: %v", err)
	}
	rows, _, _ := s.GetRows("T", RowQuery{})
	id := rows[0]["id"]

	// Act: read the old row, insert a new one, update the old one.
	row, err := s.GetRow("T", id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	inserted, err := s.InsertRow("T", map[string]string{"a": "2"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	updated, _, err := s.UpdateRow("T", id, map[string]string{"a": "3"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Assert: every single-row response carries every column.
	if v, ok := row["note"]; !ok || v != "" {
		t.Fatalf("GetRow must fill missing columns: %v", row)
	}
	if _, ok := inserted["note"]; !ok {
		t.Fatalf("InsertRow must return every column: %v", inserted)
	}
	if _, ok := updated["note"]; !ok {
		t.Fatalf("UpdateRow must return every column: %v", updated)
	}
	// the fill must not leak into the cache
	if _, ok := s.rowCache["T"]; ok {
		for _, e := range s.rowCache["T"] {
			if e.row["id"] == inserted["id"] {
				if _, ok := e.row["note"]; ok {
					t.Fatal("filled default written into the cache")
				}
			}
		}
	}
}

func TestCacheBudgetHoldsOnInsertsAndOversizedLoads(t *testing.T) {
	// Arrange: a deliberately tiny budget.
	s := openTestStore(t)
	oldBudget := MaxCachedRowsTotal
	MaxCachedRowsTotal = 150
	defer func() { MaxCachedRowsTotal = oldBudget }()

	seed := func(name string, n int) {
		rows := make([]map[string]string, n)
		for i := range rows {
			rows[i] = map[string]string{"n": fmt.Sprint(i)}
		}
		if err := s.ReplaceTable(name, cols("n"), rows); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		s.rowEntries(name)
	}
	cached := func() int {
		total := 0
		for _, e := range s.rowCache {
			total += len(e)
		}
		return total
	}
	seed("A", 100)
	seed("B", 50)

	// Act: push B past the budget with single-row inserts, then load a
	// table bigger than the whole budget.
	for i := 0; i < 20; i++ {
		if _, err := s.InsertRow("B", map[string]string{"n": "x"}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	seed("Big", 200)

	// Assert: the budget held under inserts, Big was never cached, and
	// the uncached table still reads in order.
	if got := cached(); got > MaxCachedRowsTotal {
		t.Fatalf("inserts pushed the cache over budget: %d > %d", got, MaxCachedRowsTotal)
	}
	if _, ok := s.rowCache["Big"]; ok {
		t.Fatal("a table larger than the whole budget must not be cached")
	}
	if got, total, err := s.GetRows("Big", RowQuery{Limit: 5}); err != nil || total != 200 || got[0]["n"] != "0" {
		t.Fatalf("uncached table must still read in order: %v %d %v", got, total, err)
	}
}

func TestStoreErrorKinds(t *testing.T) {
	// Arrange: a store with one two-column table.
	s := openTestStore(t)
	if err := s.CreateTable("T", cols("a", "b")); err != nil {
		t.Fatalf("create: %v", err)
	}
	checks := []struct {
		name string
		err  error
		want error
	}{
		{"create existing table", s.CreateTable("T", cols("a")), ErrConflict},
		{"create bad name", s.CreateTable("_x", cols("a")), ErrInvalid},
		{"rename onto existing column", s.RenameColumn("T", "a", "b"), ErrConflict},
		{"rename missing column", s.RenameColumn("T", "zz", "c"), ErrNotFound},
		{"bad type", s.SetColumnType("T", "a", "bogus", false), ErrInvalid},
		{"bad date order", s.SetDateOrder("ymd"), ErrInvalid},
		{"control chars in table name", s.CreateTable("T\tb", cols("a")), ErrInvalid},
		{"control chars in column rename", s.RenameColumn("T", "a", "b\nu"), ErrInvalid},
	}

	for _, c := range checks {
		// Act + Assert: each error carries its kind.
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.err, c.want)
		}
	}

	// Act + Assert: query validation and value validation are ErrInvalid.
	if _, _, err := s.GetRows("T", RowQuery{Sort: "nope"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown sort: %v", err)
	}
	if _, err := s.InsertRow("T", map[string]string{}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.SetColumnType("T", "a", TypeNumber, false); err != nil {
		t.Fatalf("set type: %v", err)
	}
	if _, err := s.InsertRow("T", map[string]string{"a": "abc"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad number: %v", err)
	}
}
