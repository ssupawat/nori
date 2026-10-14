package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nori/internal/store"
)

func TestStoreErrStatus(t *testing.T) {
	// Arrange
	cases := []struct {
		err  error
		want int
	}{
		{store.ErrNotFound, http.StatusNotFound},
		{fmt.Errorf("wrapped: %w", store.ErrConflict), http.StatusConflict},
		{store.ErrInvalid, http.StatusBadRequest},
		{store.ErrTableFull, http.StatusBadRequest},
		{store.ErrTooManyCols, http.StatusBadRequest},
		{errors.New("write /data/data.db: no space left on device"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		// Act
		got := storeErrStatus(c.err)

		// Assert
		if got != c.want {
			t.Errorf("%v: got %d, want %d", c.err, got, c.want)
		}
	}
}

func TestColumnAndKeyGrammar(t *testing.T) {
	// Arrange: an imported People table and an admin session.
	e := newTestEnv(t)
	admin := e.login("admin", "adminpw1")
	e.importXlsx(admin, map[string][][]string{"People": {{"email"}, {"a@x"}}}, "replace")

	// Act + Assert: column and key names follow the grammar.
	res, _ := e.do("POST", "/api/tables/People/columns", admin, strings.NewReader(`{"name":"email","type":"text"}`))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("add existing column should 409: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables/People/columns", admin, strings.NewReader(`{"name":"age","type":"bogus"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("add column with bad type should 400: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/tables/People/columns", admin, strings.NewReader(`{"name":"note"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add column without type should 201: %d", res.StatusCode)
	}
	res, _ = e.do("PATCH", "/api/tables/People/columns/note", admin, strings.NewReader(`{"name":"email"}`))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("rename onto existing column should 409: %d", res.StatusCode)
	}
	res, _ = e.do("GET", "/api/tables/People/rows?sort=nope", admin, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown sort column should 400: %d", res.StatusCode)
	}

	// Act + Assert: a row written before "note" existed still reads with
	// every column.
	_, list := e.doJSON("GET", "/api/tables/People/rows", admin, nil)
	id := list["rows"].([]any)[0].(map[string]any)["id"].(string)
	_, row := e.doJSON("GET", "/api/tables/People/rows/"+id, admin, nil)
	if v, ok := row["note"]; !ok || v != "" {
		t.Fatalf("GET row must fill missing columns: %v", row)
	}

	// Act + Assert: key names are unique and stay out of the user namespace.
	res, _ = e.do("POST", "/api/keys/", admin, strings.NewReader(`{"name":"etl","role":"editor"}`))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create key: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/keys/", admin, strings.NewReader(`{"name":"etl","role":"viewer"}`))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate key name should 409: %d", res.StatusCode)
	}
	res, _ = e.do("POST", "/api/users/", admin, strings.NewReader(`{"username":"key:etl","password":"longenough1","role":"viewer"}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("username in the key namespace should 400: %d", res.StatusCode)
	}
}

// add must never do I/O: with a nil database, an inline flush would panic.
func TestAuditAddNeverFlushesInline(t *testing.T) {
	// Arrange: an audit log with no database behind it.
	a := &auditLog{kick: make(chan struct{}, 1)}

	// Act: queue more events than the early-flush threshold.
	for i := 0; i < auditFlushSize*3; i++ {
		a.add(auditEntry{Action: "create_row"})
	}

	// Assert: the flush loop was woken exactly once, never inline.
	if len(a.kick) != 1 {
		t.Fatal("a full queue should wake the flush loop")
	}

	// Arrange: the loop is known to be failing.
	<-a.kick
	a.failing.Store(true)

	// Act: one more write arrives.
	a.add(auditEntry{Action: "create_row"})

	// Assert: no extra wake-up while flushes fail; it waits for the ticker.
	if len(a.kick) != 0 {
		t.Fatal("while flushes fail, writes must wait for the ticker instead of retrying")
	}
}

// SecurityHeaders must land on every response, whatever serves it.
func TestSecurityHeaders(t *testing.T) {
	// Arrange: a handler wrapped in the middleware.
	outer := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Act: serve one request through it.
	rec := httptest.NewRecorder()
	outer.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	h := rec.Header()

	// Assert: the hardening headers are all present.
	if h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options: %q", h.Get("X-Frame-Options"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options: %q", h.Get("X-Content-Type-Options"))
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "frame-ancestors 'none'", "img-src *"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
}

// Sign-ins land in the audit trail: failures with the attempted name,
// successes with the account, both with the client IP.
func TestLoginAudited(t *testing.T) {
	// Arrange: a fresh server with its bootstrap admin.
	e := newTestEnv(t)

	// Act: one failed sign-in, then a successful one, then flush.
	if res, _ := e.do("POST", "/api/login", "", strings.NewReader(`{"username":"admin","password":"wrong-pass1"}`)); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("failed login: %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/login", "", strings.NewReader(`{"username":"admin","password":"adminpw1"}`)); res.StatusCode != http.StatusOK {
		t.Fatal("good login failed")
	}
	e.srv.audit.flush()

	// Assert: both events are in the trail, attributed to admin.
	_, raw := e.do("GET", "/api/audit", e.login("admin", "adminpw1"), nil)
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("audit json: %v", err)
	}
	seen := map[string]string{}
	for _, en := range entries {
		seen[en["action"].(string)] = en["user"].(string)
	}
	if seen["login_failed"] != "admin" || seen["login"] != "admin" {
		t.Fatalf("login events missing: %v", seen)
	}
}
