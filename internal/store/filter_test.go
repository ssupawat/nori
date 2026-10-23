package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func stockTable(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	columns := []Column{
		{Name: "sku", Type: TypeText},
		{Name: "qty", Type: TypeNumber},
		{Name: "seen", Type: TypeDate},
	}
	rows := []map[string]string{
		{"sku": "100", "qty": "5", "seen": "2026-01-10"},
		{"sku": "1001", "qty": "12", "seen": "2026-02-01"},
		{"sku": "Ab-7", "qty": "0", "seen": ""},
		{"sku": "", "qty": "", "seen": "2026-03-05"},
	}
	if err := s.ReplaceTable("stock", columns, rows); err != nil {
		t.Fatalf("replace: %v", err)
	}
	return s
}

func skus(rows []map[string]string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r["sku"]
	}
	return strings.Join(out, ",")
}

func TestGetRowsOperatorFilters(t *testing.T) {
	// Arrange: text, number and date columns, including empty cells.
	s := stockTable(t)
	cases := []struct {
		name  string
		conds []Cond
		want  string
	}{
		{"eq is exact, contains is not", []Cond{{"sku", OpEq, "100"}}, "100"},
		{"eq ignores case", []Cond{{"sku", OpEq, "ab-7"}}, "Ab-7"},
		{"ne keeps empty cells", []Cond{{"sku", OpNe, "100"}}, "1001,Ab-7,"},
		{"eq with no value finds blanks", []Cond{{"sku", OpEq, ""}}, ""},
		{"ne with no value finds filled", []Cond{{"sku", OpNe, ""}}, "100,1001,Ab-7"},
		{"contains operator", []Cond{{"sku", OpContains, "100"}}, "100,1001"},
		{"number lt skips empty cells", []Cond{{"qty", OpLt, "6"}}, "100,Ab-7"},
		{"number lte", []Cond{{"qty", OpLte, "5"}}, "100,Ab-7"},
		{"number gt compares numerically, not as text", []Cond{{"qty", OpGt, "9"}}, "1001"},
		{"number gte", []Cond{{"qty", OpGte, "12"}}, "1001"},
		{"number eq is numeric", []Cond{{"qty", OpEq, "5.0"}}, "100"},
		{"number ne keeps empty cells", []Cond{{"qty", OpNe, "5"}}, "1001,Ab-7,"},
		{"range is two conditions", []Cond{{"qty", OpGte, "1"}, {"qty", OpLt, "12"}}, "100"},
		{"date lt skips empty cells", []Cond{{"seen", OpLt, "2026-02-01"}}, "100"},
		{"date gte", []Cond{{"seen", OpGte, "2026-02-01"}}, "1001,"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, total, err := s.GetRows("stock", RowQuery{Conds: tc.conds})

			// Assert
			if err != nil {
				t.Fatalf("get rows: %v", err)
			}
			if skus(got) != tc.want || total != len(got) {
				t.Fatalf("got %q (total %d), want %q", skus(got), total, tc.want)
			}
		})
	}
}

func TestGetRowsOperatorFilterErrors(t *testing.T) {
	s := stockTable(t)
	for name, conds := range map[string][]Cond{
		"ordered operator on text":  {{"sku", OpLt, "5"}},
		"number operand not number": {{"qty", OpLt, "abc"}},
		"date operand not a date":   {{"seen", OpGt, "soon"}},
		"ordered operator no value": {{"qty", OpGt, ""}},
		"unknown column":            {{"nope", OpEq, "1"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.GetRows("stock", RowQuery{Conds: conds}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
}

func TestParseFilterOp(t *testing.T) {
	for raw, want := range map[string][3]string{
		"eq:100":       {"eq", "100", "y"},
		"lte:5":        {"lte", "5", "y"},
		"contains:eq:": {"contains", "eq:", "y"},
		"eq:":          {"eq", "", "y"},
		"100":          {"", "", ""},
		"foo:bar":      {"", "", ""},
		"EQ:1":         {"", "", ""},
	} {
		op, val, ok := ParseFilterOp(raw)
		if op != want[0] || val != want[1] || (want[2] == "y") != ok {
			t.Errorf("ParseFilterOp(%q) = %q, %q, %v", raw, op, val, ok)
		}
	}
}

func TestUpdateRowIncrement(t *testing.T) {
	// Arrange: a stock row at qty 5.
	s := stockTable(t)
	got, _, err := s.GetRows("stock", RowQuery{Conds: []Cond{{"sku", OpEq, "100"}}})
	if err != nil || len(got) != 1 {
		t.Fatalf("lookup: %v %v", got, err)
	}
	id := got[0]["id"]

	// Act + Assert: decrement, decimals stay exact, empty counts as 0.
	for _, tc := range []struct{ delta, want string }{
		{"-1", "4"},
		{"0.1", "4.1"},
		{"0.2", "4.3"},
		{"-4.3", "0"},
		{"1,000", "1000"},
	} {
		row, old, err := s.UpdateRowOps("stock", id, map[string]string{}, map[string]string{"qty": tc.delta})
		if err != nil {
			t.Fatalf("inc %s: %v", tc.delta, err)
		}
		if row["qty"] != tc.want || old == nil {
			t.Fatalf("inc %s: qty %q, want %q", tc.delta, row["qty"], tc.want)
		}
	}
	blank, _, _ := s.GetRows("stock", RowQuery{Conds: []Cond{{"sku", OpEq, ""}}})
	row, _, err := s.UpdateRowOps("stock", blank[0]["id"], map[string]string{}, map[string]string{"qty": "3"})
	if err != nil || row["qty"] != "3" {
		t.Fatalf("inc on empty cell: %v %v", row, err)
	}

	// Act + Assert: the update is also what later reads see.
	again, _ := s.GetRow("stock", id)
	if again["qty"] != "1000" {
		t.Fatalf("persisted qty %q", again["qty"])
	}
}

func TestUpdateRowIncrementRejects(t *testing.T) {
	s := stockTable(t)
	got, _, _ := s.GetRows("stock", RowQuery{Conds: []Cond{{"sku", OpEq, "100"}}})
	id := got[0]["id"]
	for name, c := range map[string]struct{ values, incs map[string]string }{
		"text column":      {map[string]string{}, map[string]string{"sku": "1"}},
		"not a number":     {map[string]string{}, map[string]string{"qty": "abc"}},
		"set and inc":      {map[string]string{"qty": "9"}, map[string]string{"qty": "1"}},
		"unknown column":   {map[string]string{}, map[string]string{"nope": "1"}},
		"overflows number": {map[string]string{}, map[string]string{"qty": "999999999999999"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.UpdateRowOps("stock", id, c.values, c.incs); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
	if row, _ := s.GetRow("stock", id); row["qty"] != "5" {
		t.Fatalf("rejected update changed the row: %v", row)
	}
}

func TestUpdateRowIncrementConcurrent(t *testing.T) {
	// Arrange: qty 5, and 50 writers each taking 1.
	s := stockTable(t)
	got, _, _ := s.GetRows("stock", RowQuery{Conds: []Cond{{"sku", OpEq, "100"}}})
	id := got[0]["id"]

	// Act
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.UpdateRowOps("stock", id, map[string]string{}, map[string]string{"qty": "-1"}); err != nil {
				t.Errorf("inc: %v", err)
			}
		}()
	}
	wg.Wait()

	// Assert: no decrement was lost.
	if row, _ := s.GetRow("stock", id); row["qty"] != "-45" {
		t.Fatalf("qty %q, want -45", row["qty"])
	}
}

func TestGetRowsColumnRefFilters(t *testing.T) {
	// Arrange: stock with per-row thresholds, dates and an alias column.
	s := openTestStore(t)
	columns := []Column{
		{Name: "sku", Type: TypeText},
		{Name: "alias", Type: TypeText},
		{Name: "qty", Type: TypeNumber},
		{Name: "reorder_level", Type: TypeNumber},
		{Name: "ordered", Type: TypeDate},
		{Name: "arrived", Type: TypeDate},
	}
	rows := []map[string]string{
		{"sku": "A", "alias": "a", "qty": "2", "reorder_level": "5", "ordered": "2026-01-01", "arrived": "2026-01-05"},
		{"sku": "B", "alias": "x", "qty": "9", "reorder_level": "5", "ordered": "2026-02-10", "arrived": "2026-02-01"},
		{"sku": "C", "alias": "C", "qty": "5", "reorder_level": "5", "ordered": "2026-03-01", "arrived": ""},
		{"sku": "D", "alias": "", "qty": "1", "reorder_level": "", "ordered": "", "arrived": "2026-04-01"},
	}
	if err := s.ReplaceTable("stock", columns, rows); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cases := []struct {
		name  string
		conds []Cond
		want  string
	}{
		{"below own threshold, blank threshold skipped", []Cond{{"qty", OpLt, "col:reorder_level"}}, "A"},
		{"at or below", []Cond{{"qty", OpLte, "col:reorder_level"}}, "A,C"},
		{"above", []Cond{{"qty", OpGt, "col:reorder_level"}}, "B"},
		{"numeric eq", []Cond{{"qty", OpEq, "col:reorder_level"}}, "C"},
		{"ne keeps rows with a blank side", []Cond{{"qty", OpNe, "col:reorder_level"}}, "A,B,D"},
		{"dates compare per row", []Cond{{"arrived", OpLt, "col:ordered"}}, "B"},
		{"text eq ignores case", []Cond{{"sku", OpEq, "col:alias"}}, "A,C"},
		{"combines with literals", []Cond{{"qty", OpLte, "col:reorder_level"}, {"qty", OpGt, "2"}}, "C"},
		{"contains takes col: literally", []Cond{{"sku", OpContains, "col:qty"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, total, err := s.GetRows("stock", RowQuery{Conds: tc.conds})

			// Assert
			if err != nil {
				t.Fatalf("get rows: %v", err)
			}
			if skus(got) != tc.want || total != len(got) {
				t.Fatalf("got %q (total %d), want %q", skus(got), total, tc.want)
			}
		})
	}

	for name, conds := range map[string][]Cond{
		"unknown referenced column": {{"qty", OpLt, "col:nope"}},
		"number against date":       {{"qty", OpLt, "col:ordered"}},
		"number against text":       {{"qty", OpEq, "col:sku"}},
		"ordered operator on text":  {{"sku", OpLt, "col:alias"}},
		"empty reference":           {{"qty", OpLt, "col:"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.GetRows("stock", RowQuery{Conds: conds}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
}
