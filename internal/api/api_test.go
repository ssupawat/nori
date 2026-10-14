package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	clover "github.com/ostafen/clover/v2"

	"nori/internal/auth"
	"nori/internal/sheetimport"
	"nori/internal/store"
)

type testEnv struct {
	t      *testing.T
	server *httptest.Server
	srv    *Server
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db, err := clover.Open(t.TempDir())
	if err != nil {
		t.Fatalf("clover: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	a, err := auth.Open(db)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, err := a.EnsureAdmin("admin", "adminpw1"); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}

	root := chi.NewRouter()
	root.Get("/llms.txt", NewServer(s, a).LLMsTxt)
	root.Get("/openapi.json", NewServer(s, a).OpenAPISpec)
	srv := NewServer(s, a).WithBranding(Branding{
		Name:    "acme",
		Tagline: "acme data",
		Color:   "#2563eb",
	})
	root.Mount("/api", srv.Routes())
	ts := httptest.NewServer(root)
	t.Cleanup(ts.Close)
	return &testEnv{t: t, server: ts, srv: srv}
}

func (e *testEnv) do(method, path, token string, body io.Reader) (*http.Response, []byte) {
	return e.doWithHeaders(method, path, token, body, nil)
}

func (e *testEnv) doWithHeaders(method, path, token string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, body)
	if err != nil {
		e.t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	e.t.Cleanup(func() { res.Body.Close() })
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatalf("read response: %v", err)
	}
	return res, raw
}

func (e *testEnv) doJSON(method, path, token string, body io.Reader) (*http.Response, map[string]any) {
	res, raw := e.do(method, path, token, body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return res, decoded
}

func (e *testEnv) login(username, password string) string {
	e.t.Helper()
	body := fmt.Sprintf(`{"username": %q, "password": %q}`, username, password)
	res, decoded := e.doJSON("POST", "/api/login", "", strings.NewReader(body))
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("login %s: status %d body %v", username, res.StatusCode, decoded)
	}
	token, _ := decoded["token"].(string)
	if token == "" {
		e.t.Fatalf("no token in response: %v", decoded)
	}
	return token
}

func (e *testEnv) importXlsx(token string, sheets map[string][][]string, mode string) map[string]any {
	e.t.Helper()
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	fw, _ := mp.CreateFormFile("file", "data.xlsx")
	fw.Write(buildXlsx(e.t, sheets))
	mp.WriteField("mode", mode)
	mp.Close()

	res, raw := e.doWithHeaders("POST", "/api/import", token, &buf, map[string]string{
		"Content-Type": mp.FormDataContentType(),
	})
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("import: status %d body %s", res.StatusCode, raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		e.t.Fatalf("decode import response: %v (%s)", err, raw)
	}
	return decoded
}

func TestHealthPublic(t *testing.T) {
	// Arrange
	e := newTestEnv(t)

	// Act
	res, decoded := e.doJSON("GET", "/api/health", "", nil)

	// Assert
	if res.StatusCode != http.StatusOK || decoded["status"] != "ok" {
		t.Fatalf("health: %d %v", res.StatusCode, decoded)
	}
}

func TestAuthEnforcement(t *testing.T) {
	// Arrange: an admin, an editor, a viewer, and an imported Tasks table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	res, _ := e.do("GET", "/api/tables", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token should be 401, got %d", res.StatusCode)
	}

	if _, err := e.server.Client().Post(e.server.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"wrong"}`)); err != nil {
		t.Fatalf("bad login: %v", err)
	}

	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("setup users: %v", err)
	}
	editor := e.login("ed", "editorpw")
	viewer := e.login("view", "viewerpw")

	e.importXlsx(admin, map[string][][]string{
		"Tasks": {{"title", "status"}, {"a", "active"}, {"b", "done"}},
	}, "replace")

	// Act + Assert: each role hits each route and gets exactly the
	// status the grammar promises.
	cases := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"viewer lists rows", "GET", "/api/tables/Tasks/rows", viewer, http.StatusOK},
		{"viewer cannot create", "POST", "/api/tables/Tasks/rows", viewer, http.StatusForbidden},
		{"viewer cannot delete", "DELETE", "/api/tables/Tasks/rows/x", viewer, http.StatusForbidden},
		{"viewer cannot import", "POST", "/api/import", viewer, http.StatusForbidden},
		{"viewer cannot list users", "GET", "/api/users/", viewer, http.StatusForbidden},
		{"viewer can export", "GET", "/api/tables/Tasks/export", viewer, http.StatusOK},
		{"editor creates", "POST", "/api/tables/Tasks/rows", editor, http.StatusCreated},
		{"editor cannot import", "POST", "/api/import", editor, http.StatusForbidden},
		{"editor cannot add column", "POST", "/api/tables/Tasks/columns", editor, http.StatusForbidden},
		{"editor cannot delete table", "DELETE", "/api/tables/Tasks", editor, http.StatusForbidden},
		{"editor cannot manage users", "POST", "/api/users/", editor, http.StatusForbidden},
		{"admin creates", "POST", "/api/tables/Tasks/rows", admin, http.StatusCreated},
		{"admin lists users", "GET", "/api/users/", admin, http.StatusOK},
		{"unknown token 401", "GET", "/api/tables", "bogus", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		var body io.Reader
		if tc.method == "POST" && !strings.Contains(tc.path, "import") {
			body = strings.NewReader(`{}`)
		}
		res, _ = e.do(tc.method, tc.path, tc.token, body)
		if res.StatusCode != tc.want {
			t.Errorf("%s: %s %s with %s token = %d, want %d", tc.name, tc.method, tc.path, tc.token, res.StatusCode, tc.want)
		}
	}
}

func (e *testEnv) createUsersViaAPI(adminToken string) error {
	for _, u := range []struct{ name, pw, role string }{
		{"ed", "editorpw", "editor"},
		{"view", "viewerpw", "viewer"},
	} {
		body := fmt.Sprintf(`{"username": %q, "password": %q, "role": %q}`, u.name, u.pw, u.role)
		res, _ := e.do("POST", "/api/users/", adminToken, strings.NewReader(body))
		if res.StatusCode != http.StatusCreated {
			return fmt.Errorf("create %s: %d", u.name, res.StatusCode)
		}
	}
	return nil
}

func TestRowCrudAndFilters(t *testing.T) {
	// Arrange: an imported Tasks table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"Tasks": {
			{"title", "status"},
			{"alpha", "active"},
			{"beta", "done"},
			{"gamma", "active"},
		},
	}, "replace")

	// Act + Assert: filter + sort over the list endpoint.
	res, decoded := e.doJSON("GET", "/api/tables/Tasks/rows?status=active&sort=title&order=desc", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %v", res.StatusCode, decoded)
	}
	if decoded["total"] != float64(2) {
		t.Fatalf("filter total: %v", decoded)
	}
	rows := decoded["rows"].([]any)
	if rows[0].(map[string]any)["title"] != "gamma" {
		t.Fatalf("sort desc rows: %v", rows)
	}

	// Act + Assert: unknown filter column is a 400.
	res, decoded = e.doJSON("GET", "/api/tables/Tasks/rows?nope=1", admin, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown filter: %d %v", res.StatusCode, decoded)
	}

	// Act + Assert: create returns the row with an id.
	res, decoded = e.doJSON("POST", "/api/tables/Tasks/rows", admin, strings.NewReader(`{"title": "delta", "status": "todo"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %v", res.StatusCode, decoded)
	}
	id, _ := decoded["id"].(string)
	if id == "" {
		t.Fatalf("created row missing id: %v", decoded)
	}

	// Act + Assert: read, patch, and bad-column rejections.
	res, decoded = e.doJSON("GET", "/api/tables/Tasks/rows/"+id, admin, nil)
	if res.StatusCode != http.StatusOK || decoded["title"] != "delta" {
		t.Fatalf("get: %d %v", res.StatusCode, decoded)
	}
	res, decoded = e.doJSON("PATCH", "/api/tables/Tasks/rows/"+id, admin, strings.NewReader(`{"status": "done"}`))
	if res.StatusCode != http.StatusOK || decoded["title"] != "delta" || decoded["status"] != "done" {
		t.Fatalf("patch: %d %v", res.StatusCode, decoded)
	}
	res, decoded = e.doJSON("PATCH", "/api/tables/Tasks/rows/"+id, admin, strings.NewReader(`{"nope": "x"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown column patch: %d %v", res.StatusCode, decoded)
	}
	res, decoded = e.doJSON("POST", "/api/tables/Tasks/rows", admin, strings.NewReader(`{"nope": "x"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown column create: %d %v", res.StatusCode, decoded)
	}

	// Act + Assert: delete, then the row and a missing table are both 404.
	res, _ = e.do("DELETE", "/api/tables/Tasks/rows/"+id, admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/tables/Tasks/rows/"+id, admin, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/tables/Missing/rows", admin, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing table: %d", res.StatusCode)
	}
}

func TestImportAppendAndAddColumn(t *testing.T) {
	// Arrange: an admin with an imported People table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"People": {{"name"}, {"amy"}},
	}, "replace")

	// Act: append an import carrying a new column.
	e.importXlsx(admin, map[string][][]string{
		"People": {{"name", "age"}, {"bob", "40"}},
	}, "append")
	res, raw := e.do("GET", "/api/tables", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", res.StatusCode, raw)
	}

	// Assert: the column merged into the table metadata.
	var tables []any
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatalf("decode tables: %v (%s)", err, raw)
	}
	if len(tables) != 1 {
		t.Fatalf("tables: %v", tables)
	}
	columns := tables[0].(map[string]any)["columns"].([]any)
	if len(columns) != 2 {
		t.Fatalf("columns after append: %v", columns)
	}

	// Act: add another column through the endpoint.
	res, decoded := e.doJSON("POST", "/api/tables/People/columns", admin, strings.NewReader(`{"name": "email"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add column: %d %v", res.StatusCode, decoded)
	}

	// Assert: the typed column is in the response, and existing rows read
	// the new column as present.
	added := decoded["columns"].([]any)
	last := added[len(added)-1].(map[string]any)
	if last["name"] != "email" || last["type"] != "text" {
		t.Fatalf("added column body: %v", decoded)
	}
	rowsData := e.listRows(admin, "People")
	if rowsData["total"] != float64(2) {
		t.Fatalf("append total: %v", rowsData)
	}
	for _, r := range rowsData["rows"].([]any) {
		if _, ok := r.(map[string]any)["email"]; !ok {
			t.Fatalf("row missing new column: %v", r)
		}
	}
}

func (e *testEnv) listRows(token, table string) map[string]any {
	e.t.Helper()
	res, decoded := e.doJSON("GET", "/api/tables/"+table+"/rows?limit=100", token, nil)
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("list %s: %d %v", table, res.StatusCode, decoded)
	}
	return decoded
}

func TestDeleteTableAndUserGuards(t *testing.T) {
	// Arrange: an admin, an imported table, and two extra users.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{"Tmp": {{"a"}, {"1"}}}, "replace")

	// Act + Assert: delete the table, its rows endpoint is gone.
	res, _ := e.do("DELETE", "/api/tables/Tmp", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete table: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/tables/Tmp/rows", admin, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("rows of deleted table: %d", res.StatusCode)
	}

	// Act + Assert: self delete is blocked, deleting another user works.
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("setup users: %v", err)
	}
	res, decoded := e.doJSON("DELETE", "/api/users/admin", admin, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("self delete should be blocked: %d %v", res.StatusCode, decoded)
	}
	res, _ = e.do("DELETE", "/api/users/ed", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete user: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/users/", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list users: %d", res.StatusCode)
	}
}

func TestRenameTableAndColumn(t *testing.T) {
	// Arrange: an imported People table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"People": {{"name", "age"}, {"amy", "30"}, {"bob", "40"}},
	}, "replace")

	// Act: rename the table.
	res, raw := e.do("PATCH", "/api/tables/People", admin, strings.NewReader(`{"name":"Folks"}`))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rename table: %d", res.StatusCode)
	}

	// Assert: the response carries the renamed table (name + typed
	// columns) and the listing agrees.
	var renamed map[string]any
	if err := json.Unmarshal(raw, &renamed); err != nil {
		t.Fatalf("rename json: %v (%s)", err, raw)
	}
	if renamed["name"] != "Folks" {
		t.Fatalf("rename body: %v", renamed)
	}
	if cols, ok := renamed["columns"].([]any); !ok || len(cols) == 0 {
		t.Fatalf("rename columns: %v", renamed)
	}
	var list []map[string]any
	res, raw = e.do("GET", "/api/tables", admin, nil)
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("tables json: %v", err)
	}
	if len(list) != 1 || list[0]["name"] != "Folks" {
		t.Fatalf("tables after rename: %v", list)
	}

	// Act + Assert: rows survive the rename.
	var page map[string]any
	res, raw = e.do("GET", "/api/tables/Folks/rows", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rows after rename: %d", res.StatusCode)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("rows json: %v", err)
	}
	if int(page["total"].(float64)) != 2 {
		t.Fatalf("rows total: %v", page["total"])
	}

	// Act + Assert: rename a column - the data moves under the new name.
	res, _ = e.do("PATCH", "/api/tables/Folks/columns/name", admin, strings.NewReader(`{"name":"full name"}`))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rename column: %d", res.StatusCode)
	}
	res, raw = e.do("GET", "/api/tables/Folks/rows", admin, nil)
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("rows json: %v", err)
	}
	cols := fmt.Sprint(page["columns"])
	if !strings.Contains(cols, "full name") {
		t.Fatalf("columns after rename: %v", page["columns"])
	}
	rows := page["rows"].([]any)
	first := rows[0].(map[string]any)
	if first["full name"] != "amy" {
		t.Fatalf("renamed column data: %v", first)
	}

	// Act + Assert: old names are gone.
	res, _ = e.do("GET", "/api/tables/People/rows", admin, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("old table should 404: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/tables/Folks/rows?name=amy", admin, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("old column filter should 400: %d", res.StatusCode)
	}

	// Act + Assert: rename onto an existing name is 409, renaming a
	// missing table is 404.
	e.importXlsx(admin, map[string][][]string{"Other": {{"x"}, {"1"}}}, "replace")
	res, _ = e.do("PATCH", "/api/tables/Other", admin, strings.NewReader(`{"name":"Folks"}`))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("rename onto existing table should 409: %d", res.StatusCode)
	}
	res, _ = e.do("PATCH", "/api/tables/Ghost", admin, strings.NewReader(`{"name":"X"}`))
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("rename missing table should 404: %d", res.StatusCode)
	}

	// Act + Assert: editor and viewer cannot rename.
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("setup users: %v", err)
	}
	ed := e.login("ed", "editorpw")
	vw := e.login("view", "viewerpw")
	res, _ = e.do("PATCH", "/api/tables/Folks", ed, strings.NewReader(`{"name":"Nope"}`))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("editor rename table: %d", res.StatusCode)
	}
	res, _ = e.do("PATCH", "/api/tables/Folks/columns/full%20name", vw, strings.NewReader(`{"name":"Nope"}`))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer rename column: %d", res.StatusCode)
	}
}

func TestCreateTable(t *testing.T) {
	// Arrange
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	// Act: create a table with typed columns.
	res, raw := e.do("POST", "/api/tables", admin, strings.NewReader(
		`{"name":"NewTab","columns":[{"name":"title"},{"name":"amount","type":"number"}]}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", res.StatusCode, raw)
	}
	res, raw = e.do("GET", "/api/tables", admin, nil)

	// Assert: the listing carries the table with its types.
	var tables []map[string]any
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatalf("tables json: %v", err)
	}
	if len(tables) != 1 || tables[0]["name"] != "NewTab" {
		t.Fatalf("tables after create: %v", tables)
	}
	if fmt.Sprint(tables[0]["types"]) != "[text number]" {
		t.Fatalf("types: %v", tables[0]["types"])
	}

	// Act + Assert: rows can be added right away, bad numbers are 400.
	res, _ = e.do("POST", "/api/tables/NewTab/rows", admin, strings.NewReader(`{"title":"hello","amount":"12.5"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("insert into new table: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables/NewTab/rows", admin, strings.NewReader(`{"amount":"oops"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad number should 400: %d", res.StatusCode)
	}

	// Act + Assert: duplicate create is 409, no columns is 400.
	res, _ = e.do("POST", "/api/tables", admin, strings.NewReader(
		`{"name":"NewTab","columns":[{"name":"a"}]}`))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create should 409: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables", admin, strings.NewReader(`{"name":"X","columns":[]}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("no columns should 400: %d", res.StatusCode)
	}

	// Act + Assert: an editor cannot create tables.
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("setup users: %v", err)
	}
	ed := e.login("ed", "editorpw")
	res, _ = e.do("POST", "/api/tables", ed, strings.NewReader(
		`{"name":"Nope","columns":[{"name":"a"}]}`))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("editor create table: %d", res.StatusCode)
	}
}

func TestBranding(t *testing.T) {
	// Arrange
	e := newTestEnv(t)

	// Act: read the public branding.
	res, raw := e.do("GET", "/api/branding", "", nil)

	// Assert: it carries the configured flag values.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("branding: %d", res.StatusCode)
	}
	var b map[string]string
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("branding json: %v", err)
	}
	if b["name"] != "acme" || b["tagline"] != "acme data" || b["color"] != "#2563eb" || b["logoUrl"] != "" {
		t.Fatalf("branding values: %v", b)
	}
}

func TestLLMsTxt(t *testing.T) {
	// Arrange: an imported People table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"People": {{"name", "age"}, {"amy", "30"}},
	}, "replace")

	// Act: read llms.txt with a session.
	res, raw := e.do("GET", "/llms.txt", admin, nil)

	// Assert: the handbook refers to the spec, lists the table, and does
	// not duplicate the endpoint catalog.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("llms.txt: %d", res.StatusCode)
	}
	body := string(raw)
	if !strings.Contains(body, "/openapi.json") || !strings.Contains(body, "People: name, age(number) (1 rows)") {
		t.Fatalf("llms.txt content:\n%s", body)
	}
	if strings.Contains(body, "POST /api/import") {
		t.Fatalf("llms.txt should defer endpoint details to openapi.json:\n%s", body)
	}

	// Act + Assert: without a token it is 401.
	res, _ = e.do("GET", "/llms.txt", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("llms.txt without token: %d", res.StatusCode)
	}
}

func TestExportRoundTrip(t *testing.T) {
	// Arrange: an imported two-row table.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{
		"People": {{"name", "age"}, {"amy", "30"}, {"bob", "40"}},
	}, "replace")

	// Act: export it and parse the bytes back.
	res, raw := e.do("GET", "/api/tables/People/export", admin, nil)

	// Assert: the export reads back as the same table.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("content type: %s", ct)
	}
	tables, err := sheetimport.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("re-parse export: %v", err)
	}
	if len(tables) != 1 || tables[0].Name != "People" || len(tables[0].Rows) != 2 {
		t.Fatalf("round trip: %+v", tables)
	}
	if tables[0].Rows[0]["name"] != "amy" || tables[0].Rows[1]["age"] != "40" {
		t.Fatalf("round trip rows: %+v", tables[0].Rows)
	}
}

func TestLogoutAndCors(t *testing.T) {
	// Arrange: a logged-in admin.
	e := newTestEnv(t)
	token := e.login("admin", "adminpw1")

	// Act + Assert: a CORS preflight is answered with allow-origin *.
	req, _ := http.NewRequest(http.MethodOptions, e.server.URL+"/api/tables", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d %s", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"))
	}

	// Act: log out, then use the same token.
	res, _ = e.do("POST", "/api/logout", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("logout: %d", res.StatusCode)
	}

	// Assert: the token is dead after logout.
	res, _ = e.do("GET", "/api/tables", token, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token after logout should be dead: %d", res.StatusCode)
	}
}

func TestTableFullRejected(t *testing.T) {
	// Arrange: a tiny row cap and a fresh table at it.
	old := store.MaxTableRows
	store.MaxTableRows = 2
	t.Cleanup(func() { store.MaxTableRows = old })

	e := newTestEnv(t)
	token := e.login("admin", "adminpw1")
	e.doJSON("POST", "/api/tables", token, strings.NewReader(`{"name":"tiny","columns":[{"name":"a"}]}`))
	for i := 0; i < 2; i++ {
		resp, _ := e.doJSON("POST", "/api/tables/tiny/rows", token, strings.NewReader(`{"a":"x"}`))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("insert %d: %s", i, resp.Status)
		}
	}

	// Act: one more row, then read llms.txt.
	resp, body := e.doJSON("POST", "/api/tables/tiny/rows", token, strings.NewReader(`{"a":"x"}`))
	_, llms := e.do("GET", "/llms.txt", token, nil)

	// Assert: the insert is a 400 "full", and llms.txt tells agents about
	// the limit.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("insert past cap: %s %s", resp.Status, body["error"])
	}
	if !strings.Contains(fmt.Sprint(body["error"]), "full") {
		t.Fatalf("error message: %v", body["error"])
	}
	if !strings.Contains(string(llms), "2 rows max") {
		t.Fatalf("llms.txt limits missing: %s", llms)
	}
}

func TestUsageReport(t *testing.T) {
	// Arrange: a table with two rows, a viewer, and some real traffic.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("create users: %v", err)
	}
	viewerToken := e.login("view", "viewerpw")

	e.doJSON("POST", "/api/tables", admin, strings.NewReader(`{"name":"stuff","columns":[{"name":"a"}]}`))
	e.doJSON("POST", "/api/tables/stuff/rows", admin, strings.NewReader(`{"a":"1"}`))
	e.doJSON("POST", "/api/tables/stuff/rows", admin, strings.NewReader(`{"a":"2"}`))
	e.do("GET", "/api/tables/stuff/rows", viewerToken, nil)

	// Act + Assert: usage is admin-only.
	resp, _ := e.do("GET", "/api/usage", viewerToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("usage as viewer: %s", resp.Status)
	}

	// Act: read the report as admin.
	_, body := e.doJSON("GET", "/api/usage", admin, nil)

	// Assert: traffic totals, hourly buckets, capacity and the cap.
	if body["requests24h"].(float64) < 4 {
		t.Fatalf("requests24h = %v, want >= 4", body["requests24h"])
	}
	byHour, ok := body["byHour"].([]any)
	if !ok || len(byHour) != 24 {
		t.Fatalf("byHour = %v, want 24 hourly buckets", body["byHour"])
	}
	tables := body["tables"].([]any)
	var found bool
	for _, tb := range tables {
		if tb.(map[string]any)["name"] == "stuff" && tb.(map[string]any)["rows"].(float64) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("capacity missing table stuff: %v", tables)
	}
	if body["maxRows"].(float64) != float64(store.MaxTableRows) {
		t.Fatalf("maxRows = %v", body["maxRows"])
	}
}

func TestStatus(t *testing.T) {
	// Arrange: a viewer and an admin.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("create users: %v", err)
	}
	viewerToken := e.login("view", "viewerpw")

	// Act + Assert: status is admin-only.
	resp, _ := e.do("GET", "/api/status", viewerToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status as viewer: %s", resp.Status)
	}

	// Act: read it as admin.
	_, body := e.doJSON("GET", "/api/status", admin, nil)

	// Assert: version, uptime, data dir and size are present; backup is
	// absent because backups are off in the test env.
	if body["version"] != "dev" {
		t.Fatalf("version = %v", body["version"])
	}
	if body["uptimeSeconds"].(float64) < 0 {
		t.Fatalf("uptimeSeconds = %v", body["uptimeSeconds"])
	}
	if body["dataDir"] == "" {
		t.Fatalf("dataDir empty")
	}
	if body["dataBytes"].(float64) <= 0 {
		t.Fatalf("dataBytes = %v, want > 0 (db files exist)", body["dataBytes"])
	}
	if _, ok := body["backup"]; ok {
		t.Fatalf("backup should be absent when backups are disabled: %v", body["backup"])
	}
}

func TestGeneratedPasswordAndSelfChange(t *testing.T) {
	// Arrange: an admin creating a user without a password.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	// Act: create without a password - the server generates one and
	// returns it once.
	_, body := e.doJSON("POST", "/api/users/", admin, strings.NewReader(`{"username":"newbie","role":"editor"}`))

	// Assert
	if body["password"] == nil || body["password"].(string) == "" {
		t.Fatalf("expected generated password in response: %v", body)
	}
	generated := body["password"].(string)

	// Act + Assert: the generated password logs in.
	res, _ := e.do("POST", "/api/login", "", strings.NewReader(`{"username":"newbie","password":"`+generated+`"}`))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login with generated password: %s", res.Status)
	}

	// Act: the new user rotates their own password - wrong current fails,
	// correct current succeeds.
	newbie := e.login("newbie", generated)
	res, _ = e.doJSON("POST", "/api/me/password", newbie, strings.NewReader(`{"currentPassword":"wrong","password":"newpass123"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-change with wrong current password: %s", res.Status)
	}
	res, _ = e.doJSON("POST", "/api/me/password", newbie, strings.NewReader(`{"currentPassword":"`+generated+`","password":"newpass123"}`))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("self-change: %s", res.Status)
	}

	// Assert: the old password no longer logs in, the calling session
	// survived, and the new password works.
	res, _ = e.do("POST", "/api/login", "", strings.NewReader(`{"username":"newbie","password":"`+generated+`"}`))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password still valid: %s", res.Status)
	}
	if _, err := e.srv.auth.Validate(newbie); err != nil {
		t.Fatalf("calling session should survive self-change: %v", err)
	}
	res, _ = e.do("POST", "/api/login", "", strings.NewReader(`{"username":"newbie","password":"newpass123"}`))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login with rotated password: %s", res.Status)
	}
}

// TestSampleWorkbook fetches the generated demo workbook and closes the
// loop by importing it back: the no-file-at-hand onboarding path must work
// end to end, admin-only.
func TestSampleWorkbook(t *testing.T) {
	// Arrange: an admin.
	env := newTestEnv(t)
	admin := env.login("admin", "adminpw1")

	// Act: download the generated sample workbook.
	res, raw := env.doWithHeaders("GET", "/api/sample.xlsx", admin, nil, nil)

	// Assert: it comes back as a real spreadsheet.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sample: status %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("sample content-type: %s", res.Header.Get("Content-Type"))
	}
	if len(raw) < 1000 {
		t.Fatalf("sample implausibly small: %d bytes", len(raw))
	}

	// Act: import it back.
	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	fw, _ := mp.CreateFormFile("file", "sample.xlsx")
	fw.Write(raw)
	mp.WriteField("mode", "replace")
	mp.Close()
	res2, raw2 := env.doWithHeaders("POST", "/api/import", admin, body, map[string]string{"Content-Type": mp.FormDataContentType()})

	// Assert: all three sheets became tables.
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("import sample: status %d body %s", res2.StatusCode, raw2)
	}
	var decoded struct {
		Tables []struct {
			Name string `json:"name"`
			Rows int    `json:"rows"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw2, &decoded); err != nil {
		t.Fatalf("decode import response: %v (%s)", err, raw2)
	}
	if len(decoded.Tables) != 3 {
		t.Fatalf("expected 3 tables from the sample, got %d (%s)", len(decoded.Tables), raw2)
	}

	// Act + Assert: the download is admin-only.
	if err := env.createUsersViaAPI(admin); err != nil {
		t.Fatal(err)
	}
	viewer := env.login("view", "viewerpw")
	res3, _ := env.do("GET", "/api/sample.xlsx", viewer, nil)
	if res3.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer sample: status %d", res3.StatusCode)
	}
}

// TestBuilderRole pins the contract-without-contents promise: a builder
// identity reads schema and docs, and nothing about the rows.
func TestBuilderRole(t *testing.T) {
	// Arrange: an imported Tasks table and a builder user.
	env := newTestEnv(t)
	admin := env.login("admin", "adminpw1")
	env.importXlsx(admin, map[string][][]string{
		"Tasks": {
			{"title"},
			{"alpha"},
		},
	}, "replace")

	res, _ := env.do("POST", "/api/users/", admin, strings.NewReader(`{"username": "dev", "password": "builderpw1", "role": "builder"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create builder user: %d", res.StatusCode)
	}
	b := env.login("dev", "builderpw1")

	// Act + Assert: the contract - tables, llms.txt, openapi.json - is
	// visible to a builder.
	if res, _ = env.do("GET", "/api/tables", b, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("builder tables: %d", res.StatusCode)
	}
	if res, _ = env.do("GET", "/llms.txt", b, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("builder llms.txt: %d", res.StatusCode)
	}
	if res, _ = env.do("GET", "/openapi.json", b, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("builder openapi.json: %d", res.StatusCode)
	}

	// Act + Assert: the contents are refused - rows, writes, export, usage.
	if res, _ = env.do("GET", "/api/tables/Tasks/rows", b, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("builder read rows: %d", res.StatusCode)
	}
	if res, _ = env.do("POST", "/api/tables/Tasks/rows", b, strings.NewReader(`{"title":"x"}`)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("builder write row: %d", res.StatusCode)
	}
	if res, _ = env.do("GET", "/api/tables/Tasks/export", b, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("builder export: %d", res.StatusCode)
	}
	if res, _ = env.do("GET", "/api/usage", b, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("builder usage: %d", res.StatusCode)
	}

	// Act + Assert: a builder human can still rotate their own password.
	if res, _ = env.do("POST", "/api/me/password", b, strings.NewReader(`{"currentPassword":"builderpw1","password":"builderpw2"}`)); res.StatusCode != http.StatusOK {
		t.Fatalf("builder me/password: %d", res.StatusCode)
	}

	// Act: the same limits for a builder API key.
	kres, kraw := env.do("POST", "/api/keys/", admin, strings.NewReader(`{"name":"ci-agent","role":"builder"}`))
	if kres.StatusCode != http.StatusCreated {
		t.Fatalf("create builder key: %d %s", kres.StatusCode, kraw)
	}
	var created struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(kraw, &created); err != nil || created.Key == "" {
		t.Fatalf("decode key response: %v (%s)", err, kraw)
	}

	// Assert: the key sees the contract, never the rows.
	if res, _ = env.do("GET", "/api/tables", created.Key, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("builder key tables: %d", res.StatusCode)
	}
	if res, _ = env.do("GET", "/api/tables/Tasks/rows", created.Key, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("builder key rows: %d", res.StatusCode)
	}
}

// TestUsageExcludesDashboard marks calls with the dashboard's header and
// asserts they are not counted, so usage reflects the API's real consumers.
func TestUsageExcludesDashboard(t *testing.T) {
	// Arrange + Act: one plain call, one dashboard-marked call, then read
	// the report.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	e.do("GET", "/api/tables", admin, nil)
	e.doWithHeaders("GET", "/api/tables", admin, nil, map[string]string{"X-Requested-With": "nori-dashboard"})
	_, body := e.doJSON("GET", "/api/usage", admin, nil)

	// Assert: only the plain call was counted.
	byPath, ok := body["byPath"].(map[string]any)
	if !ok {
		t.Fatalf("byPath = %v", body["byPath"])
	}
	if byPath["GET /api/tables"].(float64) != 1 {
		t.Fatalf("GET /api/tables = %v, want 1 (the dashboard call must not be counted)", byPath["GET /api/tables"])
	}
}

func TestBackupNow(t *testing.T) {
	// Arrange: an admin and a viewer; the test server has no backup action.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("create users: %v", err)
	}
	viewerToken := e.login("view", "viewerpw")

	// Act + Assert: with no action installed, backup is 400...
	res, _ := e.do("POST", "/api/backup", admin, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("backup with no provider: %s", res.Status)
	}

	// Arrange: install a dump action.
	e.srv.WithBackupNow(func() (string, error) { return "/tmp/nori-dump-test.json.gz", nil })

	// Act + Assert: admin triggers it and gets the path...
	_, body := e.doJSON("POST", "/api/backup", admin, nil)
	if body["path"] != "/tmp/nori-dump-test.json.gz" {
		t.Fatalf("backup path = %v", body["path"])
	}

	// ...a viewer cannot.
	res, _ = e.do("POST", "/api/backup", viewerToken, nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("backup as viewer: %s", res.Status)
	}
}

// TestOpenAPISpecCoversRoutes walks the real router and fails when any
// method+path is missing from the hand-maintained openapi.json, so the
// spec cannot silently drift from the API.
func TestOpenAPISpecCoversRoutes(t *testing.T) {
	e := newTestEnv(t)
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openapiJSON, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	normalize := func(p string) string { return strings.TrimSuffix(p, "/") }
	byPath := map[string]map[string]any{}
	for p, ops := range spec.Paths {
		byPath[normalize(p)] = ops
	}

	root := chi.NewRouter()
	root.Mount("/api", e.srv.Routes())
	var missing []string
	err := chi.Walk(root, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/openapi.json" {
			return nil
		}
		p := normalize(route)
		if p == "" {
			return nil
		}
		ops, ok := byPath[p]
		if !ok {
			missing = append(missing, method+" "+route)
			return nil
		}
		if _, ok := ops[strings.ToLower(method)]; !ok {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("openapi.json is missing routes: %v", missing)
	}
}

func TestBrandingUpdateAndReset(t *testing.T) {
	// Arrange: an admin and a viewer; the flag branding is "acme".
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	if err := e.createUsersViaAPI(admin); err != nil {
		t.Fatalf("create users: %v", err)
	}
	viewerToken := e.login("view", "viewerpw")

	// Act + Assert: viewers cannot change branding.
	resp, _ := e.do("PATCH", "/api/branding", viewerToken, strings.NewReader(`{"name":"evil"}`))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("patch as viewer: %s", resp.Status)
	}

	// Act: admin sets an override.
	if res, body := e.doJSON("PATCH", "/api/branding", admin, strings.NewReader(`{"name":"acme","tagline":"acme data","color":"#2563eb","logoUrl":""}`)); res.StatusCode != http.StatusOK {
		t.Fatalf("patch: %s %v", res.Status, body)
	}

	// Assert: public GET reflects it immediately.
	_, pub := e.doJSON("GET", "/api/branding", "", nil)
	if pub["name"] != "acme" || pub["color"] != "#2563eb" {
		t.Fatalf("public branding after patch: %v", pub)
	}

	// Act + Assert: an invalid color is rejected.
	if res, _ := e.do("PATCH", "/api/branding", admin, strings.NewReader(`{"color":"red"}`)); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid color: %s", res.Status)
	}

	// Act: reset the override.
	if res, body := e.doJSON("DELETE", "/api/branding", admin, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("delete: %s %v", res.Status, body)
	}

	// Assert: branding fell back to the flag value, not the nori default
	// (newTestEnv mounts WithBranding({name: acme}) as the flag value).
	_, pub = e.doJSON("GET", "/api/branding", "", nil)
	if pub["name"] != "acme" {
		t.Fatalf("branding after reset: %v", pub)
	}
}

func TestAPIKeyAuthFlow(t *testing.T) {
	// Arrange: a table with one row and an editor key minted for it.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.doJSON("POST", "/api/tables", admin, strings.NewReader(`{"name":"k","columns":[{"name":"a"}]}`))
	e.doJSON("POST", "/api/tables/k/rows", admin, strings.NewReader(`{"a":"1"}`))

	// Act: mint the key.
	res, body := e.doJSON("POST", "/api/keys/", admin, strings.NewReader(`{"name":"agent","role":"editor"}`))

	// Assert: 201, the full key in nri_ shape.
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create key: %s %v", res.Status, body)
	}
	full := body["key"].(string)
	if !strings.HasPrefix(full, "nri_") {
		t.Fatalf("key format: %q", full)
	}

	// Act: list the keys.
	_, list := e.doJSON("GET", "/api/keys/", admin, nil)

	// Assert: the full key never leaks into the list.
	if strings.Contains(fmt.Sprint(list), full) {
		t.Fatalf("full key leaked in list: %v", list)
	}

	// Act + Assert: the editor key reads and writes.
	if resp, _ := e.do("GET", "/api/tables/k/rows", full, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("key read: %s", resp.Status)
	}
	if resp, _ := e.doJSON("POST", "/api/tables/k/rows", full, strings.NewReader(`{"a":"2"}`)); resp.StatusCode != http.StatusCreated {
		t.Fatalf("key write: %s", resp.Status)
	}

	// Act: flush the audit and read it.
	e.srv.audit.flush()
	_, raw := e.do("GET", "/api/audit?table=k", admin, nil)
	var auditEntries []map[string]any
	if err := json.Unmarshal(raw, &auditEntries); err != nil {
		t.Fatalf("audit json: %v (%s)", err, raw)
	}

	// Assert: the key write is attributed to "key:<name>".
	attributed := false
	for _, entry := range auditEntries {
		if entry["action"] == "create_row" && entry["user"] == "key:agent" {
			attributed = true
		}
	}
	if !attributed {
		t.Fatalf("key write not attributed to the key name: %s", raw)
	}

	// Act + Assert: a viewer key reads but not writes; keys never act as
	// admins and cannot mint keys.
	_, vbody := e.doJSON("POST", "/api/keys/", admin, strings.NewReader(`{"name":"read-agent","role":"viewer"}`))
	vfull := vbody["key"].(string)
	if resp, _ := e.do("GET", "/api/tables/k/rows", vfull, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer key read: %s", resp.Status)
	}
	if resp, _ := e.doJSON("POST", "/api/tables/k/rows", vfull, strings.NewReader(`{"a":"3"}`)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer key write: %s", resp.Status)
	}
	if resp, _ := e.do("GET", "/api/users/", full, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor key on admin route: %s", resp.Status)
	}
	if resp, _ := e.do("POST", "/api/keys/", vfull, strings.NewReader(`{"name":"x","role":"admin"}`)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer key minting keys: %s", resp.Status)
	}

	// Act: revoke the key.
	id := body["id"].(string)
	if resp, _ := e.do("DELETE", "/api/keys/"+id, admin, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %s", resp.Status)
	}

	// Assert: the key dies immediately; revoking it again is a 404 like
	// every other missing-resource delete.
	if resp, _ := e.do("GET", "/api/tables/k/rows", full, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked key still works: %s", resp.Status)
	}
	if resp, _ := e.do("DELETE", "/api/keys/"+id, admin, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke missing key: %s", resp.Status)
	}
}

func TestLoginRateLimit(t *testing.T) {
	// Arrange: a small login budget.
	old := maxLoginsPerMinute
	maxLoginsPerMinute = 3
	t.Cleanup(func() { maxLoginsPerMinute = old })

	e := newTestEnv(t)

	// Act: five wrong logins from one IP, then the correct password.
	var last *http.Response
	for i := 0; i < 5; i++ {
		resp, _ := e.do("POST", "/api/login", "", strings.NewReader(`{"username":"admin","password":"wrongpass1"}`))
		last = resp
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %s", last.Status)
	}

	// Assert: even a correct login from the same IP is blocked until the
	// window passes.
	if resp, _ := e.do("POST", "/api/login", "", strings.NewReader(`{"username":"admin","password":"adminpw1"}`)); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for valid login too, got %s", resp.Status)
	}
}

func TestOpenAPISpecAndKeyLaneDocs(t *testing.T) {
	// Arrange: an admin and a viewer key.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	// Act + Assert: the spec requires auth...
	if resp, _ := e.do("GET", "/openapi.json", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("spec without token: %s", resp.Status)
	}
	// ...an admin session reads it...
	resp, body := e.do("GET", "/openapi.json", admin, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spec with session: %s", resp.Status)
	}
	if !strings.Contains(string(body), `"openapi": "3.1.0"`) {
		t.Fatalf("spec body: %.80s", body)
	}
	// ...and the API-key lane can read both spec and llms.txt.
	_, kbody := e.doJSON("POST", "/api/keys/", admin, strings.NewReader(`{"name":"docs-agent","role":"viewer"}`))
	key := kbody["key"].(string)
	for _, path := range []string{"/openapi.json", "/llms.txt"} {
		if resp, _ := e.do("GET", path, key, nil); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s with API key: %s", path, resp.Status)
		}
	}
}

func TestAuditTrail(t *testing.T) {
	// Arrange: a fresh table.
	env := newTestEnv(t)
	tok := env.login("admin", "adminpw1")

	// Act: create table, create row, patch it, delete it.
	if res, _ := env.do("POST", "/api/tables", tok, strings.NewReader(`{"name":"AuditT","columns":[{"name":"x","type":"text"}]}`)); res.StatusCode != 201 {
		t.Fatalf("create table: %d", res.StatusCode)
	}
	res, body := env.do("POST", "/api/tables/AuditT/rows", tok, strings.NewReader(`{"x":"1"}`))
	if res.StatusCode != 201 {
		t.Fatalf("create row: %d", res.StatusCode)
	}
	ridRaw := strings.TrimPrefix(strings.TrimSpace(string(body)), `{"id":"`)
	rid := ridRaw[:strings.Index(ridRaw, `"`)]
	if res, _ = env.do("PATCH", "/api/tables/AuditT/rows/"+rid, tok, strings.NewReader(`{"x":"2"}`)); res.StatusCode != 200 {
		t.Fatalf("patch row: %d", res.StatusCode)
	}
	if res, _ = env.do("DELETE", "/api/tables/AuditT/rows/"+rid, tok, nil); res.StatusCode != 200 {
		t.Fatalf("delete row: %d", res.StatusCode)
	}
	// deleting a missing row is a 404, same as reading or patching one
	if res, _ = env.do("DELETE", "/api/tables/AuditT/rows/"+rid, tok, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("delete missing row: %d", res.StatusCode)
	}
	env.srv.audit.flush()

	// Assert: four entries, newest first, with the changed fields and
	// the acting user.
	res, body = env.do("GET", "/api/audit?table=AuditT", tok, nil)
	if res.StatusCode != 200 {
		t.Fatalf("audit list: %d", res.StatusCode)
	}
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("parse audit: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries (create_table/create_row/update_row/delete_row), got %d: %s", len(entries), body)
	}
	if entries[0]["action"] != "delete_row" {
		t.Fatalf("expected delete newest, got %v", entries[0]["action"])
	}
	if entries[1]["action"] != "update_row" {
		t.Fatalf("expected update second, got %v", entries[1]["action"])
	}
	if ch, ok := entries[1]["values"].(map[string]any); !ok || ch["x"] != "2" {
		t.Fatalf("update entry should carry changed field x=2, got %v", entries[1]["values"])
	}
	if u, _ := entries[2]["user"].(string); u != "admin" {
		t.Fatalf("entries should carry the acting user, got %v", entries[2]["user"])
	}

	// Act + Assert: a rowId filter scopes the trail to that row.
	res, body = env.do("GET", "/api/audit?table=AuditT&rowId="+rid, tok, nil)
	if res.StatusCode != 200 {
		t.Fatalf("audit scoped list: %d", res.StatusCode)
	}
	entries = nil
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 row-scoped entries, got %d", len(entries))
	}

	// Act + Assert: audit is admin-only.
	env.do("POST", "/api/users", tok, strings.NewReader(`{"username":"viewy","password":"viewypass1","role":"viewer"}`))
	vtok := env.login("viewy", "viewypass1")
	if res, _ = env.do("GET", "/api/audit", vtok, nil); res.StatusCode != 403 {
		t.Fatalf("audit should be admin-only, got %d", res.StatusCode)
	}
}

func TestAuditRetention(t *testing.T) {
	// Arrange: a tiny retention cap.
	env := newTestEnv(t)
	tok := env.login("admin", "adminpw1")
	env.do("POST", "/api/tables", tok, strings.NewReader(`{"name":"RetT","columns":[{"name":"x","type":"text"}]}`))
	old := auditMaxEntries
	auditMaxEntries = 3
	defer func() { auditMaxEntries = old }()

	// Act: six writes past the cap, flushed.
	for i := 0; i < 6; i++ {
		env.do("POST", "/api/tables/RetT/rows", tok, strings.NewReader(`{"x":"`+strconv.Itoa(i)+`"}`))
	}
	env.srv.audit.flush()
	env.srv.audit.flush()
	env.srv.audit.flush()

	// Assert: the trail never exceeds the cap.
	res, body := env.do("GET", "/api/audit?table=RetT", tok, nil)
	if res.StatusCode != 200 {
		t.Fatalf("audit list: %d", res.StatusCode)
	}
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) > 3 {
		t.Fatalf("retention cap exceeded: %d entries", len(entries))
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	// Arrange: a viewer with a live session.
	env := newTestEnv(t)
	adminTok := env.login("admin", "adminpw1")
	env.do("POST", "/api/users", adminTok, strings.NewReader(`{"username":"rot1","password":"oldpass99","role":"viewer"}`))
	oldTok := env.login("rot1", "oldpass99")
	if res, _ := env.do("GET", "/api/tables", oldTok, nil); res.StatusCode != 200 {
		t.Fatalf("viewer should work before rotation: %d", res.StatusCode)
	}

	// Act + Assert: a short password is 400, an unknown user is 404.
	if res, _ := env.do("PATCH", "/api/users/rot1", adminTok, strings.NewReader(`{"password":"short"}`)); res.StatusCode != 400 {
		t.Fatalf("short password should be 400, got %d", res.StatusCode)
	}
	if res, _ := env.do("PATCH", "/api/users/ghost", adminTok, strings.NewReader(`{"password":"newpass99"}`)); res.StatusCode != 404 {
		t.Fatalf("unknown user should be 404, got %d", res.StatusCode)
	}

	// Act: rotate the password.
	if res, _ := env.do("PATCH", "/api/users/rot1", adminTok, strings.NewReader(`{"password":"newpass99"}`)); res.StatusCode != 200 {
		t.Fatalf("rotate: %d", res.StatusCode)
	}

	// Assert: the old token died, the new password works.
	if res, _ := env.do("GET", "/api/tables", oldTok, nil); res.StatusCode != 401 {
		t.Fatalf("old session should be revoked, got %d", res.StatusCode)
	}
	if res, _ := env.do("GET", "/api/tables", env.login("rot1", "newpass99"), nil); res.StatusCode != 200 {
		t.Fatalf("new password should work")
	}

	// Act: the admin rotates their own password - every session of that
	// user dies, so re-login.
	if res, _ := env.do("PATCH", "/api/users/admin", adminTok, strings.NewReader(`{"password":"adminnew1"}`)); res.StatusCode != 200 {
		t.Fatalf("admin self rotation: %d", res.StatusCode)
	}
	if res, _ := env.do("GET", "/api/tables", adminTok, nil); res.StatusCode != 401 {
		t.Fatalf("admin old session should be revoked")
	}
	adminTok = env.login("admin", "adminnew1")

	// Act: flush and read the users audit trail.
	env.srv.audit.flush()
	res, body := env.do("GET", "/api/audit?table=users", adminTok, nil)
	if res.StatusCode != 200 {
		t.Fatalf("audit list: %d", res.StatusCode)
	}
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}

	// Assert: the rotation was audited.
	found := false
	for _, e := range entries {
		if e["action"] == "set_password" && e["rowId"] == "rot1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("set_password not audited: %s", body)
	}
}

// With trust-proxy off (default), spoofing X-Forwarded-For must not bypass
// the per-IP login rate limit - the limiter keys on RemoteAddr.
func TestLoginRateLimitIgnoresSpoofedXFF(t *testing.T) {
	// Arrange: a small budget, no proxy trust.
	if trustProxy {
		t.Skip("trust-proxy enabled")
	}
	old := maxLoginsPerMinute
	maxLoginsPerMinute = 3
	t.Cleanup(func() { maxLoginsPerMinute = old })

	e := newTestEnv(t)

	// Act: twelve failed logins, each from a different spoofed XFF.
	var last *http.Response
	for i := 0; i < 12; i++ {
		xff := fmt.Sprintf("10.0.%d.%d", i, i)
		res, _ := e.doWithHeaders("POST", "/api/login", "", strings.NewReader(`{"username":"admin","password":"wrongpass1"}`), map[string]string{"X-Forwarded-For": xff})
		last = res
	}

	// Assert: the limiter still keyed on the real IP - 429.
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF bypassed the rate limit, got %s", last.Status)
	}
}

func TestImportSizeCap(t *testing.T) {
	// Arrange: a 1KB import cap and a 4KB upload.
	env := newTestEnv(t)
	tok := env.login("admin", "adminpw1")
	old := MaxImportBytes
	MaxImportBytes = 1024
	t.Cleanup(func() { MaxImportBytes = old })

	big := strings.Repeat("x", 4096)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("mode", "replace")
	part, _ := writer.CreateFormFile("file", "tiny.xlsx")
	part.Write([]byte(big))
	writer.Close()

	// Act
	res, _ := env.doWithHeaders("POST", "/api/import", tok, body, map[string]string{"Content-Type": writer.FormDataContentType()})

	// Assert
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 over the cap, got %s", res.Status)
	}
}

func TestDatesOrderSetting(t *testing.T) {
	// Arrange: an admin with the default settings.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	// Act + Assert: the default order is dmy.
	res, raw := e.do("GET", "/api/settings/dates", admin, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"dateOrder":"dmy"`) {
		t.Fatalf("default order: %d %s", res.StatusCode, raw)
	}

	// Act + Assert: PUT switches the order and GET confirms it.
	res, raw = e.do("PUT", "/api/settings/dates", admin, strings.NewReader(`{"order":"mdy"}`))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"dateOrder":"mdy"`) {
		t.Fatalf("put order: %d %s", res.StatusCode, raw)
	}
	res, raw = e.do("GET", "/api/settings/dates", admin, nil)
	if !strings.Contains(string(raw), `"dateOrder":"mdy"`) {
		t.Fatalf("order after put: %s", raw)
	}

	// Act + Assert: an invalid order is a 400.
	res, _ = e.do("PUT", "/api/settings/dates", admin, strings.NewReader(`{"order":"ymd"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid order should 400: %d", res.StatusCode)
	}

	// Act + Assert: a viewer cannot read or write the setting.
	e.createUsersViaAPI(admin)
	viewer := e.login("view", "viewerpw")
	res, _ = e.do("GET", "/api/settings/dates", viewer, nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer get should 403: %d", res.StatusCode)
	}
	res, _ = e.do("PUT", "/api/settings/dates", viewer, strings.NewReader(`{"order":"dmy"}`))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer put should 403: %d", res.StatusCode)
	}
}

func TestDatetimeColumnCanonical(t *testing.T) {
	// Arrange: a table with a datetime column.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")

	res, raw := e.do("POST", "/api/tables", admin, strings.NewReader(
		`{"name":"Events","columns":[{"name":"title"},{"name":"at","type":"datetime"}]}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", res.StatusCode, raw)
	}

	// Act: insert naive UTC, an offset timestamp, then an invalid value.
	res, _ = e.do("POST", "/api/tables/Events/rows", admin, strings.NewReader(`{"title":"a","at":"2026-09-21 14:30"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("naive insert: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables/Events/rows", admin, strings.NewReader(`{"title":"b","at":"2026-09-21T21:30:00+07:00"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("offset insert: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables/Events/rows", admin, strings.NewReader(`{"title":"c","at":"tomorrow"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid datetime should 400: %d", res.StatusCode)
	}

	// Assert: both accepted values canonicalized to the same UTC instant.
	_, raw = e.do("GET", "/api/tables/Events/rows", admin, nil)
	var page struct {
		Rows []map[string]string `json:"rows"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("rows json: %v", err)
	}
	want := []string{"2026-09-21T14:30:00Z", "2026-09-21T14:30:00Z"}
	for i, w := range want {
		if page.Rows[i]["at"] != w {
			t.Fatalf("row %d at = %q, want %q", i, page.Rows[i]["at"], w)
		}
	}
}

func TestAuditQueueCapWhileFailing(t *testing.T) {
	// Arrange: a tiny queue cap.
	oldCap := auditQueueMax
	auditQueueMax = 5
	defer func() { auditQueueMax = oldCap }()

	e := newTestEnv(t)

	// Act: 50 events with nothing draining the queue. add must never be
	// called while holding the lock.
	for i := 0; i < 50; i++ {
		e.srv.audit.add(auditEntry{At: nowRFC3339(), User: "admin", Action: "create", Table: "X"})
	}
	e.srv.audit.mu.Lock()
	qlen := len(e.srv.audit.queue)
	dropped := e.srv.audit.Dropped()
	e.srv.audit.mu.Unlock()

	// Assert: the queue plateaued at the cap and the overflow was counted.
	if qlen > auditQueueMax {
		t.Fatalf("queue over cap: %d", qlen)
	}
	if dropped == 0 {
		t.Fatalf("expected drops, got 0 (queue=%d)", qlen)
	}
}
