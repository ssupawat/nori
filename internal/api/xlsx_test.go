package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildXlsx(t *testing.T, sheets map[string][][]string) []byte {
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

func TestImportDetectsDatetime(t *testing.T) {
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	e.importXlsx(admin, map[string][][]string{"Events": {
		{"name", "at"},
		{"kickoff", "2026-09-21 14:30"},
		{"second", "2026-09-22T09:00:00+07:00"},
	}}, "replace")

	res, raw := e.do("GET", "/api/tables", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list tables: %d %s", res.StatusCode, raw)
	}
	var tables []struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
		Types   []string `json:"types"`
	}
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatalf("json: %v", err)
	}
	var types []string
	for _, tb := range tables {
		if tb.Name == "Events" {
			types = tb.Types
		}
	}
	if len(types) != 2 || types[1] != "datetime" {
		t.Fatalf("types: %v", types)
	}

	_, raw = e.do("GET", "/api/tables/Events/rows?sort=at", admin, nil)
	if !strings.Contains(string(raw), `"at":"2026-09-21T14:30:00Z"`) || !strings.Contains(string(raw), `"at":"2026-09-22T02:00:00Z"`) {
		t.Fatalf("datetime rows not canonical: %s", string(raw))
	}
}

func TestImportKeepsLeadingZerosAndIdColumn(t *testing.T) {
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	e.importXlsx(admin, map[string][][]string{"Docs": {
		{"id", "doc_no", "phone"},
		{"1", "0042", "0812345678"},
		{"2", "0100", "0898765432"},
	}}, "replace")

	res, raw := e.do("GET", "/api/tables", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list tables: %d %s", res.StatusCode, raw)
	}
	var tables []struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
		Types   []string `json:"types"`
	}
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatalf("json: %v", err)
	}
	var found bool
	for _, tb := range tables {
		if tb.Name != "Docs" {
			continue
		}
		found = true
		if len(tb.Columns) != 3 || tb.Columns[0] != "id_" {
			t.Fatalf("id column renamed wrong: %v", tb.Columns)
		}
		if tb.Types[1] != "text" || tb.Types[2] != "text" {
			t.Fatalf("codes should be text: %v", tb.Types)
		}
	}
	if !found {
		t.Fatal("Docs table missing")
	}

	_, raw = e.do("GET", "/api/tables/Docs/rows", admin, nil)
	for _, want := range []string{`"doc_no":"0042"`, `"phone":"0812345678"`, `"doc_no":"0100"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}
