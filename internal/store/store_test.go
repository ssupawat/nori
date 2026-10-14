package store

import (
	"errors"
	"fmt"
	d "github.com/ostafen/clover/v2/document"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func cols(names ...string) []Column {
	out := make([]Column, len(names))
	for i, n := range names {
		out[i] = Column{Name: n, Type: TypeText}
	}
	return out
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReplaceAndListTables(t *testing.T) {
	// Arrange: a store with one imported table.
	s := openTestStore(t)

	rows := []map[string]string{
		{"name": "alice", "status": "active"},
		{"name": "bob", "status": "done"},
	}
	if err := s.ReplaceTable("tasks", cols("name", "status"), rows); err != nil {
		t.Fatalf("replace table: %v", err)
	}

	// Act
	tables, err := s.ListTables()
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}

	// Assert
	if len(tables) != 1 || tables[0].Name != "tasks" {
		t.Fatalf("unexpected tables: %+v", tables)
	}
	if len(tables[0].Columns) != 2 || tables[0].Columns[0] != "name" {
		t.Fatalf("unexpected columns: %+v", tables[0].Columns)
	}
}

func TestRowCRUD(t *testing.T) {
	// Arrange: an empty table.
	s := openTestStore(t)
	if err := s.ReplaceTable("t1", cols("name", "age"), nil); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act + Assert: create.
	row, err := s.InsertRow("t1", map[string]string{"name": "alice", "age": "30"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if row["id"] == "" || row["name"] != "alice" {
		t.Fatalf("unexpected row: %+v", row)
	}

	// Act + Assert: read.
	got, err := s.GetRow("t1", row["id"])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["age"] != "30" {
		t.Fatalf("unexpected row: %+v", got)
	}

	// Act + Assert: update, and unknown columns are rejected.
	updated, _, err := s.UpdateRow("t1", row["id"], map[string]string{"age": "31"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated["age"] != "31" || updated["name"] != "alice" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, _, err := s.UpdateRow("t1", row["id"], map[string]string{"nope": "x"}); err == nil {
		t.Fatal("expected error updating unknown column")
	}

	// Act + Assert: delete, and a missing row reports it like a missing
	// read does.
	if err := s.DeleteRow("t1", row["id"]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteRow("t1", row["id"]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing delete, got %v", err)
	}
	if _, err := s.GetRow("t1", row["id"]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetRowsQuery(t *testing.T) {
	// Arrange: three rows with mixed case names and statuses.
	s := openTestStore(t)
	rows := []map[string]string{
		{"name": "Alice", "status": "active"},
		{"name": "bob", "status": "done"},
		{"name": "Carla", "status": "active"},
	}
	if err := s.ReplaceTable("people", cols("name", "status"), rows); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act + Assert: case-insensitive column filter.
	got, total, err := s.GetRows("people", RowQuery{Filters: map[string]string{"status": "ACTIVE"}})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("filter: got %d rows, total %d", len(got), total)
	}

	// Act + Assert: column filters are contains-matches - a partial value hits.
	got, total, err = s.GetRows("people", RowQuery{Filters: map[string]string{"name": "ali"}})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if total != 1 || got[0]["name"] != "Alice" {
		t.Fatalf("contains filter: got %d rows, %+v", total, got)
	}

	// Act + Assert: q searches across all columns.
	got, total, err = s.GetRows("people", RowQuery{Q: "a"})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if total != 2 {
		t.Fatalf("search 'a': total %d, want 2", total)
	}

	// Act + Assert: unknown sort or filter columns are rejected.
	if _, _, err := s.GetRows("people", RowQuery{Sort: "nope"}); err == nil {
		t.Fatal("expected error for unknown sort column")
	}
	if _, _, err := s.GetRows("people", RowQuery{Filters: map[string]string{"nope": "x"}}); err == nil {
		t.Fatal("expected error for unknown filter column")
	}

	// Act + Assert: sort desc with limit and offset.
	got, _, err = s.GetRows("people", RowQuery{Sort: "name", Order: "desc", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	// Byte order: "bob" > "Carla" > "Alice"; drop first, take two.
	if got[0]["name"] != "Carla" || got[1]["name"] != "Alice" {
		t.Fatalf("sort desc by name: %+v", got)
	}
}

func TestPagination(t *testing.T) {
	// Arrange: a five-row table.
	s := openTestStore(t)
	rows := make([]map[string]string, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, map[string]string{"n": string(rune('a' + i))})
	}
	if err := s.ReplaceTable("nums", cols("n"), rows); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act: read the last page (offset past the last full page).
	page, total, err := s.GetRows("nums", RowQuery{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}

	// Assert: the short final page with the true total.
	if total != 5 || len(page) != 1 {
		t.Fatalf("page: len %d total %d, want 1 and 5", len(page), total)
	}
}

func TestAppendTableMergesColumns(t *testing.T) {
	// Arrange: a table with one column and one row.
	s := openTestStore(t)
	if err := s.ReplaceTable("t", cols("a"), []map[string]string{{"a": "1"}}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act: append rows carrying a new column.
	if err := s.AppendTable("t", cols("a", "b"), []map[string]string{{"a": "2", "b": "x"}}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Assert: the column merged in; the old row reads it as empty.
	columns, err := s.GetColumns("t")
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("columns: %+v", columns)
	}
	rows, total, err := s.GetRows("t", RowQuery{Limit: 10})
	if err != nil || total != 2 {
		t.Fatalf("rows: %d %v", total, err)
	}
	if rows[0]["b"] != "" {
		t.Fatalf("old row should have empty b: %+v", rows[0])
	}
}

// AppendTable must reject reserved column names exactly like ReplaceTable,
// or the insert guard silently drops their values.
func TestAppendTableRejectsReservedColumns(t *testing.T) {
	// Arrange
	s := openTestStore(t)

	// Act + Assert
	err := s.AppendTable("t", []Column{{Name: "seq", Type: TypeText}}, []map[string]string{{"seq": "x"}})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved-name error, got %v", err)
	}
}

func TestAddColumnAndDeleteTable(t *testing.T) {
	// Arrange: a table with one column.
	s := openTestStore(t)
	if err := s.ReplaceTable("t", cols("a"), nil); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act + Assert: add works, duplicate conflicts, bad type is invalid.
	if err := s.AddColumn("t", "newcol", TypeText); err != nil {
		t.Fatalf("add column: %v", err)
	}
	if err := s.AddColumn("t", "newcol", TypeText); !errors.Is(err, ErrConflict) {
		t.Fatalf("add duplicate column should conflict: %v", err)
	}
	if err := s.AddColumn("t", "other", "bogus"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("add column with bad type should be invalid: %v", err)
	}
	columns, _ := s.GetColumns("t")
	if len(columns) != 2 {
		t.Fatalf("columns: %+v", columns)
	}

	// Act + Assert: delete removes the table from the listing.
	if err := s.DeleteTable("t"); err != nil {
		t.Fatalf("delete table: %v", err)
	}
	tables, _ := s.ListTables()
	if len(tables) != 0 {
		t.Fatalf("tables after delete: %+v", tables)
	}
}

func TestNotFound(t *testing.T) {
	// Arrange
	s := openTestStore(t)

	// Act + Assert: reads of a missing table report it.
	if _, err := s.GetRow("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetColumns("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestExportAllRows(t *testing.T) {
	// Arrange: a two-by-two table.
	s := openTestStore(t)
	if err := s.ReplaceTable("t", cols("a", "b"), []map[string]string{
		{"a": "1", "b": "2"},
		{"a": "3", "b": "4"},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act
	columns, rows, err := s.AllRows("t")
	if err != nil {
		t.Fatalf("all rows: %v", err)
	}

	// Assert
	if len(columns) != 2 || len(rows) != 2 || rows[0][0] != "1" || rows[1][1] != "4" {
		t.Fatalf("unexpected export data: %v %v", columns, rows)
	}
}

func TestColumnTypes(t *testing.T) {
	// Arrange: a table with typed columns and mixed raw values.
	s := openTestStore(t)
	typedCols := []Column{
		{Name: "name", Type: TypeText},
		{Name: "price", Type: TypeNumber},
		{Name: "date", Type: TypeDate},
	}
	rows := []map[string]string{
		{"name": "a", "price": "1,234.50", "date": "2026-01-02"},
		{"name": "b", "price": "9", "date": "2026-02-01"},
		{"name": "c", "price": "250", "date": "2026-01-15"},
	}
	if err := s.ReplaceTable("typed", typedCols, rows); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Act + Assert: numeric sort (9 < 250 < 1234.5; lexicographic is wrong).
	got, _, err := s.GetRows("typed", RowQuery{Sort: "price", Order: "asc", Limit: 3})
	if err != nil {
		t.Fatalf("sort: %v", err)
	}
	want := []string{"9", "250", "1234.5"}
	for i, w := range want {
		if got[i]["price"] != w {
			t.Fatalf("numeric sort row %d: got %s want %s", i, got[i]["price"], w)
		}
	}

	// Act + Assert: date sort.
	got, _, err = s.GetRows("typed", RowQuery{Sort: "date", Order: "asc", Limit: 3})
	if err != nil {
		t.Fatalf("date sort: %v", err)
	}
	if got[0]["date"] != "2026-01-02" || got[2]["date"] != "2026-02-01" {
		t.Fatalf("date sort: %+v", got)
	}

	// Act + Assert: strict validation on insert.
	if _, err := s.InsertRow("typed", map[string]string{"price": "abc"}); err == nil {
		t.Fatal("expected validation error for non-number")
	}
	if _, err := s.InsertRow("typed", map[string]string{"date": "not-a-date"}); err == nil {
		t.Fatal("expected validation error for non-date")
	}

	// Act + Assert: canonical normalization on write.
	row, err := s.InsertRow("typed", map[string]string{"name": "d", "price": " 2,000 ", "date": "02/01/2006"})
	if err != nil {
		t.Fatalf("insert normalized: %v", err)
	}
	if row["price"] != "2000" || row["date"] != "2006-01-02" {
		t.Fatalf("normalized values: %+v", row)
	}

	// Act + Assert: empty is always allowed.
	if _, err := s.InsertRow("typed", map[string]string{"name": "e", "price": "", "date": ""}); err != nil {
		t.Fatalf("empty values: %v", err)
	}

	// Act + Assert: SetColumnType with convert rewrites values to
	// canonical form.
	if err := s.SetColumnType("typed", "date", TypeDate, true); err != nil {
		t.Fatalf("set type convert: %v", err)
	}
	gotAll, _, err := s.GetRows("typed", RowQuery{Limit: 10})
	if err != nil {
		t.Fatalf("rows after convert: %v", err)
	}
	for _, row := range gotAll {
		if row["date"] != "" && len(row["date"]) != 10 {
			t.Fatalf("date not canonical after convert: %+v", row)
		}
	}

	// Act + Assert: SetColumnType changes hint only when convert=false.
	if err := s.SetColumnType("typed", "price", TypeText, false); err != nil {
		t.Fatalf("set type: %v", err)
	}
	cols2, _ := s.GetColumnsMeta("typed")
	if cols2[1].Type != TypeText {
		t.Fatalf("type after set: %+v", cols2[1])
	}
}

func TestDetectColumns(t *testing.T) {
	// Arrange: rows whose columns are number-, date- and text-shaped.
	rows := []map[string]string{
		{"a": "1", "b": "2026-01-02", "c": "hello"},
		{"a": "2.5", "b": "2026-01-03", "c": "world"},
		{"a": "", "b": "", "c": ""},
	}

	// Act
	cols := openTestStore(t).DetectColumns([]string{"a", "b", "c"}, rows)

	// Assert
	want := []Column{
		{"a", TypeNumber},
		{"b", TypeDate},
		{"c", TypeText},
	}
	for i, c := range want {
		if cols[i] != c {
			t.Fatalf("detect col %d: %+v want %+v", i, cols[i], c)
		}
	}
}

func TestRowLimit(t *testing.T) {
	// Arrange: a deliberately tiny row cap and a table at it.
	old := MaxTableRows
	MaxTableRows = 3
	t.Cleanup(func() { MaxTableRows = old })

	s := openTestStore(t)
	if err := s.CreateTable("small", cols("a")); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Act + Assert: inserts up to the cap work, past it is ErrTableFull.
	for i := 0; i < 3; i++ {
		if _, err := s.InsertRow("small", map[string]string{"a": "x"}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if _, err := s.InsertRow("small", map[string]string{"a": "x"}); !errors.Is(err, ErrTableFull) {
		t.Fatalf("insert past cap: %v", err)
	}

	// Act + Assert: replace - the incoming batch itself must fit...
	if err := s.ReplaceTable("small", cols("a"), []map[string]string{
		{"a": "1"}, {"a": "2"}, {"a": "3"}, {"a": "4"},
	}); !errors.Is(err, ErrTableFull) {
		t.Fatalf("replace past cap: %v", err)
	}
	// ...but replace at cap is allowed even though a table exists.
	if err := s.ReplaceTable("small", cols("a"), []map[string]string{
		{"a": "1"}, {"a": "2"}, {"a": "3"},
	}); err != nil {
		t.Fatalf("replace at cap: %v", err)
	}

	// Act + Assert: append must count existing rows.
	if err := s.AppendTable("small", cols("a"), []map[string]string{{"a": "4"}}); !errors.Is(err, ErrTableFull) {
		t.Fatalf("append past cap: %v", err)
	}
}

func TestColumnLimit(t *testing.T) {
	// Arrange: a deliberately tiny column cap.
	old := MaxTableCols
	MaxTableCols = 3
	t.Cleanup(func() { MaxTableCols = old })

	s := openTestStore(t)

	// Act + Assert: create past the cap is rejected, at the cap works.
	tooMany := cols("a", "b", "c", "d")
	if err := s.CreateTable("wide", tooMany); !errors.Is(err, ErrTooManyCols) {
		t.Fatalf("create past col cap: %v", err)
	}
	if err := s.CreateTable("ok", cols("a", "b", "c")); err != nil {
		t.Fatalf("create at cap: %v", err)
	}

	// Act + Assert: add past the cap is rejected; an existing column is a
	// conflict, checked before the cap.
	if err := s.AddColumn("ok", "d", ""); !errors.Is(err, ErrTooManyCols) {
		t.Fatalf("add col past cap: %v", err)
	}
	if err := s.AddColumn("ok", "a", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("add existing col: %v", err)
	}

	// Act + Assert: append-merge past the cap is rejected and leaves meta
	// untouched.
	if err := s.AppendTable("ok", cols("d"), []map[string]string{{"a": "1", "d": "x"}}); !errors.Is(err, ErrTooManyCols) {
		t.Fatalf("append-merge past col cap: %v", err)
	}
	meta, err := s.GetColumnsMeta("ok")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if len(meta) != 3 {
		t.Fatalf("meta mutated by rejected append: %+v", meta)
	}
}

func TestSettings(t *testing.T) {
	// Arrange: a store with no branding setting.
	s := openTestStore(t)

	// Act + Assert: an unset setting reports not-ok...
	_, ok, err := s.GetSetting("branding")
	if err != nil || ok {
		t.Fatalf("unset setting: ok=%v err=%v", ok, err)
	}

	// Act: set, overwrite in place, read back, delete.
	vals := map[string]string{"name": "acme", "color": "#2563eb"}
	if err := s.SetSetting("branding", vals); err != nil {
		t.Fatalf("set: %v", err)
	}
	vals["tagline"] = "hello"
	if err := s.SetSetting("branding", vals); err != nil {
		t.Fatalf("set again: %v", err)
	}
	got, ok, err := s.GetSetting("branding")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if err := s.DeleteSetting("branding"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Assert: the values round-tripped and the delete took.
	if got["name"] != "acme" || got["color"] != "#2563eb" || got["tagline"] != "hello" {
		t.Fatalf("got: %v", got)
	}
	if _, ok, _ := s.GetSetting("branding"); ok {
		t.Fatal("setting survived delete")
	}
}

// Single-row writes must be visible in GetRows without a cache rebuild, and
// the cache must stay in seq order through insert/update/delete.
func TestIncrementalRowCache(t *testing.T) {
	// Arrange: a three-row table whose cache is warm.
	s := openTestStore(t)
	if err := s.CreateTable("tcache", []Column{{Name: "name", Type: TypeText}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	a, err := s.InsertRow("tcache", map[string]string{"name": "a"})
	if err != nil {
		t.Fatalf("insert a: %v", err)
	}
	b, err := s.InsertRow("tcache", map[string]string{"name": "b"})
	if err != nil {
		t.Fatalf("insert b: %v", err)
	}
	if _, err := s.InsertRow("tcache", map[string]string{"name": "c"}); err != nil {
		t.Fatalf("insert c: %v", err)
	}
	if _, _, err := s.GetRows("tcache", RowQuery{}); err != nil {
		t.Fatalf("list: %v", err)
	}

	// Act: mutate through the patch paths - delete, update, insert.
	if err := s.DeleteRow("tcache", b["id"]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := s.UpdateRow("tcache", a["id"], map[string]string{"name": "a2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := s.InsertRow("tcache", map[string]string{"name": "d"}); err != nil {
		t.Fatalf("insert d: %v", err)
	}

	// Assert: reads see the patched cache in seq order, and the deleted
	// row stays deleted.
	rows, total, err := s.GetRows("tcache", RowQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d (total %d)", len(rows), total)
	}
	if rows[0]["name"] != "a2" || rows[1]["name"] != "c" || rows[2]["name"] != "d" {
		t.Fatalf("unexpected order/content: %+v", rows)
	}
	if _, err := s.GetRow("tcache", b["id"]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted row still readable")
	}
}

// InsertRow sequences must be strictly increasing even when inserts happen
// back-to-back on a coarse clock, or row order becomes unstable.
func TestInsertSeqMonotonic(t *testing.T) {
	// Arrange: an empty table.
	s := openTestStore(t)
	if err := s.CreateTable("seq", []Column{{Name: "n", Type: TypeText}}); err != nil {
		t.Fatal(err)
	}

	// Act: insert 300 rows back-to-back, then read them in order.
	const n = 300
	for i := 0; i < n; i++ {
		if _, err := s.InsertRow("seq", map[string]string{"n": strconv.Itoa(i)}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	rows, total, err := s.GetRows("seq", RowQuery{Limit: n})
	if err != nil {
		t.Fatal(err)
	}

	// Assert: insertion order held.
	if total != n || len(rows) != n {
		t.Fatalf("expected %d rows, got %d (total %d)", n, len(rows), total)
	}
	for i, row := range rows {
		if row["n"] != strconv.Itoa(i) {
			t.Fatalf("order broken at %d: got %s", i, row["n"])
		}
	}
}

func TestDatetimeNormalize(t *testing.T) {
	// Arrange: a store with default settings.
	s := openTestStore(t)
	cases := []struct{ in, want string }{
		{"2026-09-21T14:30:00+07:00", "2026-09-21T07:30:00Z"},
		{"2026-09-21T14:30:00Z", "2026-09-21T14:30:00Z"},
		{"2026-09-21 14:30", "2026-09-21T14:30:00Z"},
		{"2026-09-21T14:30", "2026-09-21T14:30:00Z"},
		{"2026-09-21 14:30:05", "2026-09-21T14:30:05Z"},
	}

	for _, c := range cases {
		// Act + Assert: each input canonicalizes to UTC RFC3339.
		got, err := s.NormalizeColumn(TypeDatetime, c.in)
		if err != nil || got != c.want {
			t.Fatalf("datetime %q: got %q err %v (want %q)", c.in, got, err, c.want)
		}
	}

	// Act + Assert: invalid input fails, whitespace-only is empty.
	if _, err := s.NormalizeColumn(TypeDatetime, "tomorrow"); err == nil {
		t.Fatal("invalid datetime should fail")
	}
	if got, _ := s.NormalizeColumn(TypeDatetime, "   "); got != "" {
		t.Fatalf("empty datetime: %q", got)
	}
}

func TestDateOrder(t *testing.T) {
	// Arrange: a store with the default D/M order.
	s := openTestStore(t)
	if s.DateOrder() != OrderDMY {
		t.Fatalf("default order: %q", s.DateOrder())
	}

	// Act + Assert: D/M reads both padded and unpadded slash dates...
	if got, _ := s.NormalizeColumn(TypeDate, "3/4/2026"); got != "2026-04-03" {
		t.Fatalf("dmy 3/4/2026: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDate, "03/04/2026"); got != "2026-04-03" {
		t.Fatalf("dmy padded 03/04/2026: %q", got)
	}
	// ...and unambiguous formats are untouched by the order.
	if got, _ := s.NormalizeColumn(TypeDate, "2026-01-02"); got != "2026-01-02" {
		t.Fatalf("iso date: %q", got)
	}

	// Arrange: the order switched to M/D.
	if err := s.SetDateOrder(OrderMDY); err != nil {
		t.Fatalf("set order mdy: %v", err)
	}

	// Act + Assert: the same dates now read the other way, slash
	// datetimes follow too.
	if got, _ := s.NormalizeColumn(TypeDate, "3/4/2026"); got != "2026-03-04" {
		t.Fatalf("mdy 3/4/2026: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDate, "03/04/2026"); got != "2026-03-04" {
		t.Fatalf("mdy padded 03/04/2026: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDatetime, "3/4/2026 14:30"); got != "2026-03-04T14:30:00Z" {
		t.Fatalf("mdy slash datetime: %q", got)
	}

	// Act + Assert: an invalid order is rejected and changes nothing.
	if err := s.SetDateOrder("ymd"); err == nil {
		t.Fatal("invalid order should fail")
	}
	if s.DateOrder() != OrderMDY {
		t.Fatalf("order after invalid set: %q", s.DateOrder())
	}
}

func TestDateOrderPersistsAcrossReopen(t *testing.T) {
	// Arrange: a store whose order was switched to M/D, then closed.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.SetDateOrder(OrderMDY); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Act: reopen the same data dir.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: the order survived.
	if s2.DateOrder() != OrderMDY {
		t.Fatalf("order after reopen: %q", s2.DateOrder())
	}
}

func TestDetectDatetimeColumn(t *testing.T) {
	// Arrange: rows whose columns are datetime- and date-shaped.
	s := openTestStore(t)
	rows := []map[string]string{
		{"at": "2026-09-21 14:30", "d": "2026-09-21"},
		{"at": "2026-09-22T09:00:00+07:00", "d": "2026-09-22"},
	}

	// Act
	cols := s.DetectColumns([]string{"at", "d"}, rows)

	// Assert
	if cols[0].Type != TypeDatetime {
		t.Fatalf("at detected as %s, want datetime", cols[0].Type)
	}
	if cols[1].Type != TypeDate {
		t.Fatalf("d detected as %s, want date", cols[1].Type)
	}
}

func TestLeadingZeroAndHugeNumbersStayText(t *testing.T) {
	// Arrange
	s := openTestStore(t)

	// Act + Assert: codes and phone numbers with a leading zero are not
	// numbers...
	if _, err := s.NormalizeColumn(TypeNumber, "0812345678"); err == nil {
		t.Fatal("leading-zero phone should not normalize as number")
	}
	if _, err := s.NormalizeColumn(TypeNumber, "007"); err == nil {
		t.Fatal("007 should not normalize as number")
	}
	// ...and integers at 16+ digits lose float64 exactness - not numbers
	// either.
	if _, err := s.NormalizeColumn(TypeNumber, "4111111111111111"); err == nil {
		t.Fatal("16-digit integer should not normalize as number")
	}
	if _, err := s.NormalizeColumn(TypeNumber, "12345678901234567890"); err == nil {
		t.Fatal("20-digit integer should not normalize as number")
	}

	// Act + Assert: genuine numbers still work.
	for _, ok := range []string{"0", "0.5", "12.5", "999999999999999", "1e10", "-0.25", "2026"} {
		if _, err := s.NormalizeColumn(TypeNumber, ok); err != nil {
			t.Fatalf("valid number %q rejected: %v", ok, err)
		}
	}

	// Act + Assert: detection follows - a column of phones comes out
	// text, values intact.
	rows := []map[string]string{
		{"phone": "0812345678"},
		{"phone": "0898765432"},
	}
	cols := s.DetectColumns([]string{"phone"}, rows)
	if cols[0].Type != TypeText {
		t.Fatalf("phone column detected as %s, want text", cols[0].Type)
	}
	if rows[0]["phone"] != "0812345678" {
		t.Fatalf("phone value mutated: %q", rows[0]["phone"])
	}
}

func TestBuddhistYearSetting(t *testing.T) {
	// Arrange: a store with Buddhist years defaulting on.
	s := openTestStore(t)
	if !s.BuddhistYears() {
		t.Fatal("Buddhist years should default on")
	}

	// Act + Assert: พ.ศ. 2567 = ค.ศ. 2024 in all three input shapes, and
	// ordinary CE years pass through.
	if got, _ := s.NormalizeColumn(TypeDate, "21/09/2567"); got != "2024-09-21" {
		t.Fatalf("be date: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDate, "2567-09-21"); got != "2024-09-21" {
		t.Fatalf("be iso date: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDatetime, "2567-09-21 14:30"); got != "2024-09-21T14:30:00Z" {
		t.Fatalf("be datetime: %q", got)
	}
	if got, _ := s.NormalizeColumn(TypeDate, "2026-09-21"); got != "2026-09-21" {
		t.Fatalf("ce date: %q", got)
	}

	// Arrange: Buddhist years switched off.
	if err := s.SetBuddhistYears(false); err != nil {
		t.Fatalf("set off: %v", err)
	}

	// Act + Assert: large years stay as written, and the two settings
	// don't clobber each other.
	if got, _ := s.NormalizeColumn(TypeDate, "21/09/2567"); got != "2567-09-21" {
		t.Fatalf("be off: %q", got)
	}
	if err := s.SetDateOrder(OrderMDY); err != nil {
		t.Fatalf("set order: %v", err)
	}
	if s.BuddhistYears() {
		t.Fatal("beYear should still be off")
	}

	// Act: reopen the same data dir.
	dir := s.Dir()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: both settings survive.
	if s2.DateOrder() != OrderMDY || s2.BuddhistYears() {
		t.Fatalf("after reopen: order=%q be=%v", s2.DateOrder(), s2.BuddhistYears())
	}
}

func TestReplaceTableAtomicUnderSimulatedCrash(t *testing.T) {
	// Arrange: a seeded table, then a complete staging copy marked ready
	// to swap - the exact state a crash right before the swap leaves.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rows := []map[string]string{{"name": "amy"}, {"name": "bob"}}
	if err := s.ReplaceTable("People", cols("name"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	newRows := []map[string]string{{"name": "carol"}}
	staging := "_staging_People"
	if err := s.ReplaceTableStagingForTest("People", cols("name"), newRows, staging); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := s.SetSetting("import_staging", map[string]string{"table": "People", "phase": "swap"}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Act: reopen - boot recovery must finish the marked swap.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: the new dataset is live and the staging copy was cleaned up.
	got, _, err := s2.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "carol" {
		t.Fatalf("recovery should finish the swap: %+v", got)
	}
	if exists, _ := s2.TableExists(staging); exists {
		t.Fatal("staging should be cleaned up")
	}
}

func TestOrphanStagingCleaned(t *testing.T) {
	// Arrange: an aborted build leaves a staging collection but no manifest.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.db.CreateCollection("_staging_Ghost"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Act: reopen.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: the orphan staging collection was dropped.
	if exists, _ := s2.db.HasCollection("_staging_Ghost"); exists {
		t.Fatal("orphan staging should be dropped on open")
	}
}

func TestNaNAndInfRejected(t *testing.T) {
	// Arrange
	s := openTestStore(t)

	// Act + Assert: every NaN/Inf spelling is rejected...
	for _, bad := range []string{"NaN", "nan", "-NaN", "Inf", "+Inf", "-Infinity", "infinity"} {
		if _, err := s.NormalizeColumn(TypeNumber, bad); err == nil {
			t.Fatalf("%q should be rejected in a number column", bad)
		}
	}
	// ...while genuine numbers still work.
	for _, good := range []string{"1.5", "-0.25", "1e10", "2026"} {
		if _, err := s.NormalizeColumn(TypeNumber, good); err != nil {
			t.Fatalf("valid number %q rejected: %v", good, err)
		}
	}
}

// ReplaceTableStagingForTest builds the staging collection for a replace
// without performing the swap, simulating a crash point for tests.
func (s *Store) ReplaceTableStagingForTest(name string, columns []Column, rows []map[string]string, staging string) error {
	s.normalizeRows(columns, rows)
	if exists, _ := s.db.HasCollection(staging); !exists {
		if err := s.db.CreateCollection(staging); err != nil {
			return err
		}
	}
	if err := s.saveMeta(staging, columns); err != nil {
		return err
	}
	return s.insertAll(staging, rows)
}

func TestGetRowsReturnsIndependentMaps(t *testing.T) {
	// Arrange: a table where one row lacks the second column.
	s := openTestStore(t)
	rows := []map[string]string{
		{"name": "amy", "note": "has note"},
		{"name": "bob"}, // "note" missing -> default-filled
	}
	if err := s.ReplaceTable("People", cols("name", "note"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Act: read the rows, then mutate the returned maps.
	got, _, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	for _, r := range got {
		r["note"] = "MUTATED"
		r["extra"] = "MUTATED"
	}
	again, _, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("get rows again: %v", err)
	}

	// Assert: the shared cache was never touched.
	if again[0]["note"] != "has note" {
		t.Fatalf("cache mutated through returned maps: %+v", again)
	}
	if again[1]["note"] != "" {
		t.Fatalf("unexpected note: %+v", again[1])
	}
	if _, ok := again[0]["extra"]; ok {
		t.Fatalf("phantom key leaked into cache: %+v", again[0])
	}
}

func TestRenameColumnPreservesRowIDs(t *testing.T) {
	// Arrange: a two-row table with its row ids recorded.
	s := openTestStore(t)
	rows := []map[string]string{{"name": "amy", "age": "30"}, {"name": "bob", "age": "40"}}
	if err := s.ReplaceTable("People", cols("name", "age"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, _, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	ids := map[string]bool{before[0]["id"]: true, before[1]["id"]: true}

	// Act: rename the column.
	if err := s.RenameColumn("People", "age", "years"); err != nil {
		t.Fatalf("rename column: %v", err)
	}
	after, _, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("get after: %v", err)
	}

	// Assert: same ids, values under the new name.
	if len(after) != 2 {
		t.Fatalf("rows lost: %+v", after)
	}
	for _, r := range after {
		if !ids[r["id"]] {
			t.Fatalf("row id changed: %v (want one of %v)", r["id"], ids)
		}
		if r["years"] == "" || r["age"] != "" {
			t.Fatalf("rename wrong: %+v", r)
		}
	}
}

func TestSetColumnTypeConvertSurvivesAndPreservesIDs(t *testing.T) {
	// Arrange: a table with raw score values and its row ids recorded.
	s := openTestStore(t)
	rows := []map[string]string{
		{"name": "amy", "score": "1,234.50"},
		{"name": "bob", "score": "12"},
	}
	if err := s.ReplaceTable("Scores", cols("name", "score"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, _, _ := s.GetRows("Scores", RowQuery{})
	ids := map[string]bool{before[0]["id"]: true, before[1]["id"]: true}

	// Act: convert the column to number with rewrite.
	if err := s.SetColumnType("Scores", "score", TypeNumber, true); err != nil {
		t.Fatalf("convert: %v", err)
	}
	after, _, err := s.GetRows("Scores", RowQuery{})
	if err != nil {
		t.Fatalf("get after: %v", err)
	}

	// Assert: ids preserved, values canonical, and writes now validate
	// against the new type.
	want := map[string]string{"amy": "1234.5", "bob": "12"}
	for _, r := range after {
		if !ids[r["id"]] {
			t.Fatalf("row id changed: %v", r["id"])
		}
		if r["score"] != want[r["name"]] {
			t.Fatalf("score %q (%s): want %q", r["score"], r["name"], want[r["name"]])
		}
	}
	if _, _, err := s.UpdateRow("Scores", after[0]["id"], map[string]string{"score": "oops"}); err == nil {
		t.Fatal("write against new type should validate")
	}
}

func TestFinishSwapFailureKeepsStagingAndManifest(t *testing.T) {
	// Arrange: a staged copy and manifest, like ReplaceTable does just
	// before committing - then the staging meta is broken so the swap
	// fails at its first step.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	staging := stagingPrefix + "People"
	rows := []map[string]string{{"name": "carol"}}
	if err := s.ReplaceTableStagingForTest("People", cols("name"), rows, staging); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := s.SetSetting("import_staging", map[string]string{"table": "People", "phase": "swap"}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	meta, err := s.findMeta(staging)
	if err != nil || meta == nil {
		t.Fatalf("staging meta missing: %v", err)
	}
	if err := s.db.DeleteById(metaCollection, meta.ObjectId()); err != nil {
		t.Fatalf("drop meta: %v", err)
	}
	s.invalidateMetaCache(staging) // a fresh process would have an empty cache

	// Act: run the failing swap.
	if err := s.finishSwap("People", staging, false, ""); err == nil {
		t.Fatal("finishSwap should fail without staging meta")
	}

	// Assert: both the staging copy and the manifest survived the failure.
	if exists, _ := s.db.HasCollection(staging); !exists {
		t.Fatal("staging must be kept on failure")
	}
	if _, ok, _ := s.GetSetting("import_staging"); !ok {
		t.Fatal("manifest must be kept on failure")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Act: the next start retries the swap - recovery keeps the manifest
	// and retries on every boot (each attempt is logged) rather than
	// discarding a staging copy that still holds data.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Assert: the manifest is still there for the retry.
	if _, ok, _ := s2.GetSetting("import_staging"); !ok {
		t.Fatal("manifest should be kept for retry")
	}
}

func TestReplaceTableErrorKeepsStagingForRecovery(t *testing.T) {
	// Arrange: a seeded table, then a staged replacement whose staging
	// meta is broken so the swap fails.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.ReplaceTable("People", cols("name"), []map[string]string{{"name": "amy"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	staging := stagingPrefix + "People"
	if err := s.ReplaceTableStagingForTest("People", cols("name"), []map[string]string{{"name": "carol"}}, staging); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := s.SetSetting("import_staging", map[string]string{"table": "People", "phase": "swap"}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	meta, _ := s.findMeta(staging)
	s.db.DeleteById(metaCollection, meta.ObjectId())
	s.invalidateMetaCache(staging)

	// Act: run the failing swap, then read the live table.
	if err := s.finishSwap("People", staging, false, ""); err == nil {
		t.Fatal("swap should fail")
	}

	// Assert: the old data is untouched.
	got, total, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if total != 1 || got[0]["name"] != "amy" {
		t.Fatalf("old data lost after failed swap: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestCheckHealthyStore(t *testing.T) {
	// Arrange: a seeded, healthy store.
	s := openTestStore(t)
	rows := []map[string]string{
		{"name": "amy", "age": "30"},
		{"name": "bob", "age": "40"},
	}
	if err := s.ReplaceTable("People", cols("name", "age"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Act
	rep := s.Check()

	// Assert: no problems, sensible totals, a clean People entry.
	if !rep.OK {
		t.Fatalf("healthy store reported problems: %+v", rep.Problems)
	}
	if rep.TotalDocs < 2 {
		t.Fatalf("total docs too low: %d", rep.TotalDocs)
	}
	var people *CheckTable
	for i := range rep.Tables {
		if rep.Tables[i].Name == "People" {
			people = &rep.Tables[i]
		}
	}
	if people == nil || people.Rows != 2 || len(people.Problems) != 0 {
		t.Fatalf("People check: %+v", people)
	}
}

func TestCheckDetectsOrphanAndMissingMeta(t *testing.T) {
	// Arrange: a seeded table, then two wounds - an orphan collection with
	// data but no meta, and the table's own meta deleted.
	s := openTestStore(t)
	if err := s.ReplaceTable("People", cols("name"), []map[string]string{{"name": "amy"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.db.CreateCollection("Orphan"); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if err := s.db.Insert("Orphan", func() *d.Document { doc := d.NewDocument(); doc.Set("x", "1"); return doc }()); err != nil {
		t.Fatalf("orphan doc: %v", err)
	}
	meta, _ := s.findMeta("People")
	s.db.DeleteById(metaCollection, meta.ObjectId())
	s.invalidateMetaCache("People")

	// Act
	rep := s.Check()

	// Assert: both wounds are reported.
	if rep.OK {
		t.Fatalf("problems not detected: %+v", rep)
	}
	joined := strings.Join(rep.Problems, "\n")
	if !strings.Contains(joined, "Orphan: no column metadata") {
		t.Fatalf("expected orphan problem, got: %s", joined)
	}
	if !strings.Contains(joined, "People: no column metadata") {
		t.Fatalf("expected missing-meta problem, got: %s", joined)
	}
}

func TestCheckDetectsNonNumericSeq(t *testing.T) {
	// Arrange: a seeded table with an injected raw document whose seq is a
	// string (the old format-1 restore bug).
	s := openTestStore(t)
	if err := s.ReplaceTable("People", cols("name"), []map[string]string{{"name": "amy"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	doc := d.NewDocument()
	doc.Set("name", "bad")
	doc.Set("seq", "not-a-number")
	if err := s.db.Insert("People", doc); err != nil {
		t.Fatalf("inject: %v", err)
	}
	s.invalidateRows("People")

	// Act
	rep := s.Check()

	// Assert
	joined := strings.Join(rep.Problems, "\n")
	if !strings.Contains(joined, "non-numeric seq") {
		t.Fatalf("non-numeric seq not detected: %s", joined)
	}
}

func TestCompactIntoPreservesEverything(t *testing.T) {
	// Arrange: a seeded table plus a setting, snapshotted before compaction.
	dir1, dir2 := t.TempDir(), t.TempDir()
	s, err := Open(dir1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rows := []map[string]string{
		{"name": "amy", "age": "30"},
		{"name": "bob", "age": "40"},
	}
	if err := s.ReplaceTable("People", cols("name", "age"), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.SetSetting("dates", map[string]string{"order": "mdy", "beYear": "true"}); err != nil {
		t.Fatalf("setting: %v", err)
	}
	before, _, _ := s.GetRows("People", RowQuery{})

	// Act: compact into a fresh dir, then open it.
	stats, skipped, err := s.CompactInto(dir2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	s2, err := Open(dir2)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}
	defer s2.Close()
	after, _, err := s2.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("rows after: %v", err)
	}

	// Assert: nothing skipped, the table intact, the setting survived.
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
	found := false
	for _, st := range stats {
		if st.Collection == "People" && st.Docs == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("People not compacted: %+v", stats)
	}
	if len(after) != len(before) {
		t.Fatalf("row count changed: %d -> %d", len(before), len(after))
	}
	byID := map[string]map[string]string{}
	for _, r := range after {
		byID[r["id"]] = r
	}
	for _, r := range before {
		a := byID[r["id"]]
		if a == nil {
			t.Fatalf("row id %s missing after compact", r["id"])
		}
		if a["name"] != r["name"] || a["age"] != r["age"] {
			t.Fatalf("row drifted: %+v vs %+v", a, r)
		}
	}
	if vals, ok, _ := s2.GetSetting("dates"); !ok || vals["order"] != "mdy" {
		t.Fatalf("settings lost: %v", vals)
	}
}

func TestCompactReclaimsSpace(t *testing.T) {
	// Arrange: 4000 rows with real payload, then replace with 300 (drops
	// 3700 rows' worth of pages into the freelist - the file keeps its
	// high-water size).
	dir1, dir2 := t.TempDir(), t.TempDir()
	s, err := Open(dir1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	big := make([]map[string]string, 0, 4000)
	for i := 0; i < 4000; i++ {
		big = append(big, map[string]string{
			"name": fmt.Sprintf("user-%04d", i),
			"pad":  strings.Repeat("x", 200),
		})
	}
	if err := s.ReplaceTable("Big", cols("name", "pad"), big); err != nil {
		t.Fatalf("seed big: %v", err)
	}
	small := big[:300]
	if err := s.ReplaceTable("Big", cols("name", "pad"), small); err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	oldSize := dirSize(dir1)

	// Act: compact into a fresh dir.
	s2, err := Open(dir1)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	stats, _, err := s2.CompactInto(dir2)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	s2.Close()

	// Assert: the rebuilt dir is smaller and Big holds exactly 300 docs.
	newSize := dirSize(dir2)
	if newSize >= oldSize {
		t.Fatalf("compact did not reclaim space: old %d, new %d", oldSize, newSize)
	}
	for _, st := range stats {
		if st.Collection == "Big" && st.Docs != 300 {
			t.Fatalf("Big docs after compact: %d", st.Docs)
		}
	}
}

func dirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// TestOpenFixtureV1 pins today's on-disk encoding: the committed fixture
// was written by an earlier binary, so if a clover upgrade changes the
// encoding, this test fails before real data dirs go unreadable. After an
// intentional storage change, regenerate the fixture and commit it:
//
//	UPDATE_FIXTURE=1 go test ./internal/store/ -run TestOpenFixtureV1
func TestOpenFixtureV1(t *testing.T) {
	// Arrange: regenerate the committed fixture when asked (the write
	// happens with current code; freezing it in git is what makes the
	// cross-version check meaningful).
	const fixtureDir = "testdata/fixture-v1"
	if os.Getenv("UPDATE_FIXTURE") != "" {
		if err := os.RemoveAll(fixtureDir); err != nil {
			t.Fatalf("clear fixture: %v", err)
		}
		s, err := Open(fixtureDir)
		if err != nil {
			t.Fatalf("open fixture dir: %v", err)
		}
		if err := s.ReplaceTable("People", cols("name", "age"), []map[string]string{
			{"name": "amy", "age": "30"},
			{"name": "bob", "age": "40"},
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := s.SetSetting("dates", map[string]string{"order": "dmy", "beYear": "true"}); err != nil {
			t.Fatalf("setting: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(fixtureDir)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	// Act: open a copy of it and read back.
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer s.Close()
	rows, total, err := s.GetRows("People", RowQuery{})
	if err != nil {
		t.Fatalf("fixture rows: %d %v", total, err)
	}

	// Assert: the data and settings are exactly as written.
	if total != 2 {
		t.Fatalf("fixture rows: %d %v", total, err)
	}
	if rows[0]["name"] != "amy" || rows[1]["name"] != "bob" {
		t.Fatalf("fixture rows drifted: %+v", rows)
	}
	if vals, ok, _ := s.GetSetting("dates"); !ok || vals["order"] != "dmy" {
		t.Fatalf("fixture settings lost: %v", vals)
	}
	if s.SchemaVersion() == "" {
		t.Fatal("fixture missing schema stamp")
	}
}

func TestCacheEvictionUnderBudget(t *testing.T) {
	// Arrange: a deliberately small budget and three tables cached under it.
	s := openTestStore(t)
	oldBudget := MaxCachedRowsTotal
	MaxCachedRowsTotal = 250
	defer func() { MaxCachedRowsTotal = oldBudget }()

	for _, name := range []string{"A", "B", "C"} {
		rows := make([]map[string]string, 0, 100)
		for i := 0; i < 100; i++ {
			rows = append(rows, map[string]string{"n": fmt.Sprintf("%s-%d", name, i)})
		}
		if err := s.ReplaceTable(name, cols("n"), rows); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		s.rowEntries(name) // force-cache each table
	}

	// Act: measure the cache, then read a table that was evicted.
	total := 0
	for _, e := range s.rowCache {
		total += len(e)
	}
	got, rowCount, err := s.GetRows("A", RowQuery{Limit: 100})
	if err != nil {
		t.Fatalf("evicted table read: %v", err)
	}

	// Assert: the budget held, and the evicted table still reads correctly
	// (re-scanned from disk).
	if total > MaxCachedRowsTotal {
		t.Fatalf("cache over budget: %d > %d", total, MaxCachedRowsTotal)
	}
	if rowCount != 100 || len(got) != 100 {
		t.Fatalf("evicted table broken: %d %v", rowCount, err)
	}
}
