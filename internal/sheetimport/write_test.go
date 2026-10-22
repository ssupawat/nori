package sheetimport

import (
	"bytes"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// wantRows builds the dataset shared by the correctness tests.
func wantRows(n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		k := i + 1
		rows[i] = []string{
			`item "&<>' ` + itoa(k),
			"café-émoji-✅",
			itoa(k * 7),
			"",
			longString(k),
		}
	}
	return rows
}

func itoa(n int) string {
	return fmtInt(n)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func longString(k int) string {
	s := make([]byte, 500)
	for i := range s {
		s[i] = byte('a' + k%26)
	}
	return string(s)
}

func parseOne(t *testing.T, data []byte) Table {
	t.Helper()
	tables, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	return tables[0]
}

func rowsToMaps(columns []string, rows [][]string) []map[string]string {
	out := make([]map[string]string, len(rows))
	for i, row := range rows {
		m := make(map[string]string, len(columns))
		for j, c := range columns {
			m[c] = row[j]
		}
		out[i] = m
	}
	return out
}

func TestWriteStreamingRoundTrip(t *testing.T) {
	columns := []string{"name", "category", "value", "empty", "note"}
	rows := wantRows(1000)

	var buf bytes.Buffer
	err := WriteStreaming(&buf, "stress", columns, func(yield func([][]string) error) error {
		return yield(rows)
	})
	if err != nil {
		t.Fatalf("write streaming: %v", err)
	}

	got := parseOne(t, buf.Bytes())
	if got.Name != "stress" {
		t.Errorf("sheet name = %q, want %q", got.Name, "stress")
	}
	if len(got.Columns) != len(columns) {
		t.Fatalf("columns = %v, want %v", got.Columns, columns)
	}
	for i, c := range columns {
		if got.Columns[i] != c {
			t.Errorf("column %d = %q, want %q", i, got.Columns[i], c)
		}
	}
	if len(got.Rows) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(got.Rows), len(rows))
	}
	want := rowsToMaps(columns, rows)
	for i := range want {
		for _, c := range columns {
			if got.Rows[i][c] != want[i][c] {
				t.Errorf("row %d col %q = %q, want %q", i, c, got.Rows[i][c], want[i][c])
			}
		}
	}
}

func TestWriteStreamingMatchesWrite(t *testing.T) {
	cases := []struct {
		name  string
		cols  []string
		rows  [][]string
		sheet string
	}{
		{"zero rows", []string{"a", "b"}, nil, "empty"},
		{"one row", []string{"a", "b"}, [][]string{{"x", "y"}}, "one"},
		{"empty strings", []string{"a", "b", "c"}, [][]string{{"", "", ""}}, "blank"},
		{"normal", []string{"name", "value"}, wantRows(50), "normal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wantBuf, gotBuf bytes.Buffer
			if err := Write(&wantBuf, tc.sheet, tc.cols, tc.rows); err != nil {
				t.Fatalf("write: %v", err)
			}
			err := WriteStreaming(&gotBuf, tc.sheet, tc.cols, func(yield func([][]string) error) error {
				if len(tc.rows) == 0 {
					return nil
				}
				return yield(tc.rows)
			})
			if err != nil {
				t.Fatalf("write streaming: %v", err)
			}

			want, got := parseOne(t, wantBuf.Bytes()), parseOne(t, gotBuf.Bytes())
			if got.Name != want.Name {
				t.Errorf("name = %q, want %q", got.Name, want.Name)
			}
			if len(got.Columns) != len(want.Columns) {
				t.Fatalf("columns = %v, want %v", got.Columns, want.Columns)
			}
			for i := range want.Columns {
				if got.Columns[i] != want.Columns[i] {
					t.Errorf("column %d = %q, want %q", i, got.Columns[i], want.Columns[i])
				}
			}
			if len(got.Rows) != len(want.Rows) {
				t.Fatalf("rows = %d, want %d", len(got.Rows), len(want.Rows))
			}
			for i := range want.Rows {
				for _, c := range want.Columns {
					if got.Rows[i][c] != want.Rows[i][c] {
						t.Errorf("row %d col %q = %q, want %q", i, c, got.Rows[i][c], want.Rows[i][c])
					}
				}
			}
		})
	}
}

func TestWriteStreamingBoundedMemory(t *testing.T) {
	columns := []string{"name", "category", "value", "note"}
	rows := wantRows(1000)
	const pages = 100 // 100k rows total

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

	err := WriteStreaming(io.Discard, "stress", columns, func(yield func([][]string) error) error {
		for p := 0; p < pages; p++ {
			if err := yield(rows); err != nil {
				return err
			}
		}
		return nil
	})
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("write streaming: %v", err)
	}

	const bound = 120 << 20
	if p := peak.Load(); p > bound {
		t.Errorf("peak heap = %d MB, want <= %d MB (streaming not bounded)", p>>20, bound>>20)
	}
}
