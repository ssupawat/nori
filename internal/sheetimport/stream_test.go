package sheetimport

import (
	"bytes"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// collectTables drains a TableStream into the same shape Parse returns.
func collectTables(ts TableStream) (Table, error) {
	out := Table{Name: ts.Name, Columns: ts.Columns}
	err := ts.Rows(func(page []map[string]string) error {
		out.Rows = append(out.Rows, page...)
		return nil
	})
	return out, err
}

func TestParseStreamMatchesParse(t *testing.T) {
	cases := []struct {
		name   string
		sheets map[string][][]string
	}{
		{
			name: "two sheets",
			sheets: map[string][][]string{
				"Tasks": {
					{"title", "status"},
					{`write "&<>' tests`, "done"},
					{"café ✅", ""},
				},
				"Numbers": {
					{"value"},
					{"7"},
				},
			},
		},
		{
			name: "header only",
			sheets: map[string][][]string{
				"Hollow": {{"a", "b"}},
			},
		},
		{
			name: "short and empty rows",
			sheets: map[string][][]string{
				"Ragged": {
					{"a", "b", "c"},
					{"1", "2"},
					{"", ""},
					{"3", "4", "5"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := buildTestXlsx(t, tc.sheets)

			want, err := Parse(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			var got []Table
			err = ParseStream(bytes.NewReader(data), func(ts TableStream) error {
				tb, err := collectTables(ts)
				if err != nil {
					return err
				}
				got = append(got, tb)
				return nil
			})
			if err != nil {
				t.Fatalf("parse stream: %v", err)
			}

			if len(got) != len(want) {
				t.Fatalf("tables = %d, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i].Name != want[i].Name {
					t.Errorf("table %d name = %q, want %q", i, got[i].Name, want[i].Name)
				}
				if len(got[i].Columns) != len(want[i].Columns) {
					t.Fatalf("table %d columns = %v, want %v", i, got[i].Columns, want[i].Columns)
				}
				for j, c := range want[i].Columns {
					if got[i].Columns[j] != c {
						t.Errorf("table %d column %d = %q, want %q", i, j, got[i].Columns[j], c)
					}
				}
				if len(got[i].Rows) != len(want[i].Rows) {
					t.Fatalf("table %d rows = %d, want %d", i, len(got[i].Rows), len(want[i].Rows))
				}
				for j := range want[i].Rows {
					for _, c := range want[i].Columns {
						if got[i].Rows[j][c] != want[i].Rows[j][c] {
							t.Errorf("table %d row %d col %q = %q, want %q", i, j, c, got[i].Rows[j][c], want[i].Rows[j][c])
						}
					}
				}
			}
		})
	}
}

func TestParseStreamBoundedMemory(t *testing.T) {
	columns := []string{"name", "category", "value", "note"}
	rows := wantRows(100000)
	var buf bytes.Buffer
	if err := Write(&buf, "stress", columns, rows); err != nil {
		t.Fatalf("build workbook: %v", err)
	}
	rows = nil

	runtime.GC()
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				for {
					old := peak.Load()
					if ms.HeapInuse <= old || peak.CompareAndSwap(old, ms.HeapInuse) {
						break
					}
				}
			}
		}
	}()

	var count int
	err := ParseStream(bytes.NewReader(buf.Bytes()), func(ts TableStream) error {
		return ts.Rows(func(page []map[string]string) error {
			count += len(page)
			return nil
		})
	})
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("parse stream: %v", err)
	}
	if count != 100000 {
		t.Errorf("rows = %d, want 100000", count)
	}

	const bound = 150 << 20
	if p := peak.Load(); p > bound {
		t.Errorf("peak heap = %d MB, want <= %d MB (parse not bounded)", p>>20, bound>>20)
	}
}
