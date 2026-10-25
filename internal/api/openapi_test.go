package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type specDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]map[string]any `json:"schemas"`
	} `json:"components"`
}

func operationIDs(t *testing.T, doc specDoc) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			if method == "parameters" {
				continue
			}
			var op struct {
				OperationID string `json:"operationId"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			where := strings.ToUpper(method) + " " + path
			if op.OperationID == "" {
				t.Errorf("%s has no operationId", where)
				continue
			}
			if prev, dup := ids[op.OperationID]; dup {
				t.Errorf("operationId %q used by %s and %s", op.OperationID, prev, where)
			}
			ids[op.OperationID] = where
		}
	}
	return ids
}

func TestOpenAPIOperationIDsUniqueAndPresent(t *testing.T) {
	// Arrange
	raw, err := specJSON(nil)
	if err != nil {
		t.Fatalf("encode spec: %v", err)
	}
	var doc specDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	// Act
	ids := operationIDs(t, doc)

	// Assert: spot-check names that follow the handlers.
	for id, where := range map[string]string{
		"listRows":  "GET /api/tables/{t}/rows",
		"updateRow": "PATCH /api/tables/{t}/rows/{id}",
		"listAudit": "GET /api/audit",
	} {
		if ids[id] != where {
			t.Errorf("operationId %q is on %q, want %q", id, ids[id], where)
		}
	}
}

func TestOpenAPITypedTablePaths(t *testing.T) {
	// Arrange: tables with typed columns, a space in a name, and a Thai name.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"Bills": {
			{"room", "month", "paid", "q"},
			{"A101", "2026-10-01", "4180", "x"},
		},
		"order items": {{"sku", "qty"}, {"A1", "3"}},
		"ห้อง":        {{"name"}, {"a"}},
	}, "replace")
	_, kbody := e.doJSON("POST", "/api/keys/", admin, strings.NewReader(`{"name":"spec-agent","role":"builder"}`))
	builder := kbody["key"].(string)

	// Act: a builder key reads the spec.
	res, raw := e.do("GET", "/openapi.json", builder, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("spec as builder: %s", res.Status)
	}
	var doc specDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	// Assert: every operation, generic and per table, has a unique operationId.
	ids := operationIDs(t, doc)
	for id, where := range map[string]string{
		"listBillsRows":       "GET /api/tables/Bills/rows",
		"updateBillsRow":      "PATCH /api/tables/Bills/rows/{id}",
		"listOrderItemsRows":  "GET /api/tables/order%20items/rows",
		"createTableRow":      "POST /api/tables/%E0%B8%AB%E0%B9%89%E0%B8%AD%E0%B8%87/rows",
		"deleteOrderItemsRow": "DELETE /api/tables/order%20items/rows/{id}",
	} {
		if ids[id] != where {
			t.Errorf("operationId %q is on %q, want %q", id, ids[id], where)
		}
	}

	// Assert: the served row schema carries the column types.
	row := doc.Components.Schemas["Row_Bills"]["properties"].(map[string]any)
	if got := row["paid"].(map[string]any)["x-nori-type"]; got != "number" {
		t.Errorf("Row_Bills.paid x-nori-type = %v", got)
	}
	if got := row["month"].(map[string]any)["format"]; got != "date" {
		t.Errorf("Row_Bills.month format = %v", got)
	}

	// Assert: inc is offered on number columns only, and unknown columns are closed.
	patch := doc.Components.Schemas["RowPatch_Bills"]
	props := patch["properties"].(map[string]any)
	if _, ok := props["paid"].(map[string]any)["oneOf"]; !ok {
		t.Errorf("RowPatch_Bills.paid should allow {inc}: %v", props["paid"])
	}
	if _, ok := props["room"].(map[string]any)["oneOf"]; ok {
		t.Errorf("RowPatch_Bills.room is text and should not allow {inc}")
	}
	if patch["additionalProperties"] != false {
		t.Errorf("RowPatch_Bills should reject unknown columns")
	}

	// Assert: one filter parameter per column, except reserved list params.
	var list struct {
		Parameters []struct {
			Name   string         `json:"name"`
			Schema map[string]any `json:"schema"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(doc.Paths["/api/tables/Bills/rows"]["get"], &list); err != nil {
		t.Fatalf("list op: %v", err)
	}
	var names []string
	var sortEnum []any
	for _, p := range list.Parameters {
		names = append(names, p.Name)
		if p.Name == "sort" {
			sortEnum, _ = p.Schema["enum"].([]any)
		}
	}
	for _, want := range []string{"room", "month", "paid"} {
		if !slices.Contains(names, want) {
			t.Errorf("Bills list has no %q filter: %v", want, names)
		}
	}
	if n := strings.Count(strings.Join(names, ","), "q"); n != 1 {
		t.Errorf("column q should not add a second q parameter: %v", names)
	}
	if len(sortEnum) != 4 {
		t.Errorf("sort enum = %v, want the 4 columns", sortEnum)
	}

	// Assert: the escaped paths in the spec are the ones the router serves.
	for _, path := range []string{"/api/tables/order%20items/rows", "/api/tables/%E0%B8%AB%E0%B9%89%E0%B8%AD%E0%B8%87/rows"} {
		if res, _ := e.do("GET", path, admin, nil); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %s", path, res.Status)
		}
	}

	// Assert: the generic list no longer has a parameter literally named {col}.
	if strings.Contains(string(doc.Paths["/api/tables/{t}/rows"]["get"]), `"name": "{col}"`) {
		t.Errorf("generic list still declares a {col} parameter")
	}
}

func TestLLMsTxtAccessSection(t *testing.T) {
	// Arrange: an admin session, and an editor and a builder key.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	key := func(name, role string) string {
		_, body := e.doJSON("POST", "/api/keys/", admin, strings.NewReader(`{"name":"`+name+`","role":"`+role+`"}`))
		return body["key"].(string)
	}
	for caller, want := range map[string]string{
		admin:                         "You are admin with the admin role. You can do everything",
		key("writer", "editor"):       "You are key:writer with the editor role. You can read and write rows",
		key("schema-only", "builder"): "You are key:schema-only with the builder role. You can read the schema",
	} {
		// Act
		res, raw := e.do("GET", "/llms.txt", caller, nil)

		// Assert: the caller's own role comes first, then the shared rules.
		body := string(raw)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, "## Access\n\n"+want) {
			t.Fatalf("access section for %q:\n%s", want, body)
		}
		if !strings.Contains(body, "no per-table or per-row access") || !strings.Contains(body, "don't expire") {
			t.Fatalf("access section is missing the shared rules:\n%s", body)
		}
	}
}
