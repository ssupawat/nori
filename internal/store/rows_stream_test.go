package store

import (
	"errors"
	"testing"
)

func TestReplaceTableStreamPages(t *testing.T) {
	s := openTestStore(t)
	columns := []Column{{Name: "name", Type: "text"}, {Name: "value", Type: "text"}}

	pages := func(yield func([]map[string]string) error) error {
		for p := 0; p < 3; p++ {
			page := make([]map[string]string, 2)
			for i := range page {
				page[i] = map[string]string{
					"name":  pageValue(p, i),
					"value": itoa(p*2 + i),
				}
			}
			if err := yield(page); err != nil {
				return err
			}
		}
		return nil
	}
	n, err := s.ReplaceTableStream("t", columns, pages)
	if err != nil {
		t.Fatalf("replace stream: %v", err)
	}
	if n != 6 {
		t.Errorf("count = %d, want 6", n)
	}

	got, _, err := s.GetRows("t", RowQuery{Limit: 100})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("rows = %d, want 6", len(got))
	}
	if got[0]["name"] != "name-0-0" || got[5]["value"] != "5" {
		t.Fatalf("content mismatch: first=%v last=%v", got[0], got[5])
	}
}

func pageValue(p, i int) string {
	return "name-" + itoa(p) + "-" + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestReplaceTableStreamEmpty(t *testing.T) {
	s := openTestStore(t)
	columns := []Column{{Name: "a", Type: "text"}}

	n, err := s.ReplaceTableStream("empty", columns, func(yield func([]map[string]string) error) error {
		return nil
	})
	if err != nil {
		t.Fatalf("replace stream: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if _, err := s.GetColumns("empty"); err != nil {
		t.Fatalf("table should exist: %v", err)
	}
}

func TestReplaceTableStreamInvalidName(t *testing.T) {
	s := openTestStore(t)
	_, err := s.ReplaceTableStream("_staging_x", []Column{{Name: "a"}}, func(yield func([]map[string]string) error) error {
		return nil
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestAppendTableStreamPages(t *testing.T) {
	s := openTestStore(t)
	columns := []Column{{Name: "a", Type: "text"}}
	if err := s.ReplaceTable("t", columns, []map[string]string{{"a": "1"}, {"a": "2"}}); err != nil {
		t.Fatalf("seed table: %v", err)
	}

	n, err := s.AppendTableStream("t", columns, func(yield func([]map[string]string) error) error {
		return yield([]map[string]string{{"a": "3"}})
	})
	if err != nil {
		t.Fatalf("append stream: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	got, _, err := s.GetRows("t", RowQuery{Limit: 100})
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(got) != 3 || got[2]["a"] != "3" {
		t.Fatalf("after append: %v", got)
	}
}

func TestAppendTableStreamCreatesTable(t *testing.T) {
	s := openTestStore(t)
	columns := []Column{{Name: "a", Type: "text"}}

	n, err := s.AppendTableStream("fresh", columns, func(yield func([]map[string]string) error) error {
		return yield([]map[string]string{{"a": "x"}})
	})
	if err != nil {
		t.Fatalf("append stream: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}
