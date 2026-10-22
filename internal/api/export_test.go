package api

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"nori/internal/sheetimport"
	"nori/internal/store"
)

func TestExportHandlerStreams(t *testing.T) {
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	cols := []store.Column{{Name: "name", Type: "text"}, {Name: "value", Type: "text"}}
	if err := e.srv.store.CreateTable("stress", cols); err != nil {
		t.Fatalf("create table: %v", err)
	}
	const n = 500
	for i := 0; i < n; i++ {
		if _, err := e.srv.store.InsertRow("stress", map[string]string{
			"name":  fmt.Sprintf(`item "&<>' %d`, i),
			"value": fmt.Sprintf("v%d", i),
		}); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	res, raw := e.do("GET", "/api/tables/stress/export", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}

	tables, err := sheetimport.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse exported file: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	got := tables[0]
	if got.Name != "stress" || len(got.Columns) != 2 || len(got.Rows) != n {
		t.Fatalf("shape: name=%q cols=%v rows=%d", got.Name, got.Columns, len(got.Rows))
	}
	if got.Rows[0]["name"] != `item "&<>' 0` || got.Rows[n-1]["value"] != fmt.Sprintf("v%d", n-1) {
		t.Fatalf("content mismatch: first=%v last=%v", got.Rows[0], got.Rows[n-1])
	}
}

func TestExportHandlerEmptyTable(t *testing.T) {
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	cols := []store.Column{{Name: "a", Type: "text"}, {Name: "b", Type: "text"}}
	if err := e.srv.store.CreateTable("hollow", cols); err != nil {
		t.Fatalf("create table: %v", err)
	}

	res, raw := e.do("GET", "/api/tables/hollow/export", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}

	tables, err := sheetimport.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse exported file: %v", err)
	}
	if len(tables) != 1 || tables[0].Name != "hollow" || len(tables[0].Rows) != 0 {
		t.Fatalf("shape: %+v", tables)
	}
}
