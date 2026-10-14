package sheetimport

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildTestXlsx(t *testing.T, sheets map[string][][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() { f.Close() })

	first := true
	for name, grid := range sheets {
		sheet := name
		if first {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				t.Fatalf("rename sheet: %v", err)
			}
			first = false
		} else if _, err := f.NewSheet(name); err != nil {
			t.Fatalf("new sheet: %v", err)
		}
		for r, line := range grid {
			for c, value := range line {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				if err := f.SetCellValue(sheet, cell, value); err != nil {
					t.Fatalf("set cell: %v", err)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buf.Bytes()
}

func TestParseSingleSheet(t *testing.T) {
	// Arrange: a workbook with one sheet, two data rows and one empty row.
	data := buildTestXlsx(t, map[string][][]string{
		"Tasks": {
			{"title", "status"},
			{"write tests", "done"},
			{"ship it", "todo"},
			{"", ""},
		},
	})

	// Act
	tables, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Assert: one table, named after the sheet, empty row skipped.
	if len(tables) != 1 {
		t.Fatalf("tables: %+v", tables)
	}
	tb := tables[0]
	if tb.Name != "Tasks" || len(tb.Columns) != 2 || tb.Columns[0] != "title" {
		t.Fatalf("unexpected table: %+v", tb)
	}
	if len(tb.Rows) != 2 {
		t.Fatalf("empty row should be skipped: %+v", tb.Rows)
	}
	if tb.Rows[0]["title"] != "write tests" || tb.Rows[1]["status"] != "todo" {
		t.Fatalf("unexpected rows: %+v", tb.Rows)
	}
}

func TestParseMultipleSheetsAndHeaders(t *testing.T) {
	// Arrange: two sheets - one with an empty header cell in the middle,
	// one with a trailing fully empty row.
	data := buildTestXlsx(t, map[string][][]string{
		// Trailing empty header cells are trimmed by excelize, so the
		// unnamed header sits between two named ones.
		"one": {{"name", "", "age"}, {"a", "b", "c"}},
		"two": {{"name"}, {"x"}, {}},
	})

	// Act
	tables, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Assert: both sheets came through, the empty header got a default
	// name, the empty row was skipped.
	if len(tables) != 2 {
		t.Fatalf("tables: %+v", tables)
	}
	byName := map[string]Table{}
	for _, tb := range tables {
		byName[tb.Name] = tb
	}
	one := byName["one"]
	if len(one.Columns) != 3 || one.Columns[1] != "Column2" {
		t.Fatalf("empty header should get default name: %+v", one.Columns)
	}
	if one.Rows[0]["Column2"] != "b" {
		t.Fatalf("unexpected row: %+v", one.Rows[0])
	}
	two := byName["two"]
	if len(two.Rows) != 1 {
		t.Fatalf("fully empty row should be skipped: %+v", two.Rows)
	}
}

func TestSanitizeNames(t *testing.T) {
	// Arrange: a workbook whose sheet name collides with system names.
	data := buildTestXlsx(t, map[string][][]string{
		"_users": {{"a"}, {"1"}},
	})

	// Act: parse it, then sanitize a set of tricky names.
	tables, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Assert: the reserved prefix was stripped...
	if len(tables) != 1 || tables[0].Name == "_users" {
		t.Fatalf("reserved prefix should be stripped: %+v", tables)
	}
	// ...and the sanitizer maps each tricky name to its safe form.
	cases := map[string]string{
		"bad/name":  "bad-name",
		"a\\b":      "a-b",
		"  spaced ": "spaced",
		"___":       "Sheet",
		"":          "Sheet",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Fatalf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRejectsEmptyWorkbook(t *testing.T) {
	// Arrange: a workbook with no data sheets.
	data := buildTestXlsx(t, map[string][][]string{})

	// Act + Assert: parsing reports it.
	if _, err := Parse(bytes.NewReader(data)); err == nil {
		t.Fatal("expected error for workbook with no data")
	}
}

func TestWriteRoundTrip(t *testing.T) {
	// Arrange: nothing but the writer's inputs.

	// Act: write one table, then parse the bytes back.
	var buf bytes.Buffer
	if err := Write(&buf, "People", []string{"name", "age"}, [][]string{
		{"amy", "30"},
		{"bob", "40"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	tables, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}

	// Assert: the same table, columns and rows came back.
	if len(tables) != 1 || tables[0].Name != "People" {
		t.Fatalf("tables: %+v", tables)
	}
	tb := tables[0]
	if fmt.Sprint(tb.Columns) != "[name age]" {
		t.Fatalf("columns: %+v", tb.Columns)
	}
	if len(tb.Rows) != 2 || tb.Rows[0]["name"] != "amy" || tb.Rows[1]["age"] != "40" {
		t.Fatalf("rows: %+v", tb.Rows)
	}
}
