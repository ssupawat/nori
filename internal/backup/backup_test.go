package backup

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

func openTempStore(t *testing.T) (*clover.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := clover.Open(dir)
	if err != nil {
		t.Fatalf("open clover: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dir
}

func TestDumpAndRestore(t *testing.T) {
	// Arrange: a database with system collections and one data table.
	db, _ := openTempStore(t)
	for _, c := range []string{"_users", "_meta", "t1"} {
		if err := db.CreateCollection(c); err != nil {
			t.Fatal(err)
		}
	}
	u := d.NewDocument()
	u.Set("username", "admin")
	u.Set("role", "admin")
	if _, err := db.InsertOne("_users", u); err != nil {
		t.Fatal(err)
	}
	meta := d.NewDocument()
	meta.Set("table", "t1")
	meta.Set("columns", []any{map[string]any{"name": "name", "type": "text"}})
	if _, err := db.InsertOne("_meta", meta); err != nil {
		t.Fatal(err)
	}
	row := d.NewDocument()
	row.Set("name", "alice")
	row.Set("seq", int64(1))
	if _, err := db.InsertOne("t1", row); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	// Act: dump, then dump three more times under a keep of 2.
	first, err := DumpJSON(db, outDir, 2)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := DumpJSON(db, outDir, 2); err != nil {
			t.Fatalf("dump %d: %v", i, err)
		}
	}

	// Assert: the dump is named right and retention kept only the newest 2.
	if !strings.HasSuffix(first, ".json.gz") {
		t.Fatalf("unexpected dump name: %s", first)
	}
	files, _ := filepath.Glob(filepath.Join(outDir, "nori-dump-*.json.gz"))
	sort.Strings(files)
	if len(files) != 2 {
		t.Fatalf("retention failed: %d dumps kept", len(files))
	}

	// Act: restore the newest dump into a fresh database.
	db2, dir2 := openTempStore(t)
	counts, err := RestoreJSON(db2, files[len(files)-1])
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	_ = dir2

	// Assert: every collection came back, the row with it.
	for _, c := range []string{"_users", "_meta", "t1"} {
		if counts[c] == 0 {
			t.Fatalf("collection %s not restored: %+v", c, counts)
		}
	}
	docs, err := db2.FindAll(query.NewQuery("t1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Get("name") != "alice" {
		t.Fatalf("unexpected restored row: %+v", docs)
	}
}

// seedHighSeqRows inserts rows whose seq values are UnixNano-sized (what
// InsertRow produces) directly into a collection.
func seedHighSeqRows(t *testing.T, db *clover.DB, name string, n int) []int64 {
	t.Helper()
	if err := db.CreateCollection(name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	base := time.Now().UnixNano()
	want := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		seq := base + int64(i)
		doc := d.NewDocument()
		doc.Set("name", fmt.Sprintf("row-%d", i))
		doc.Set("seq", seq)
		if _, err := db.InsertOne(name, doc); err != nil {
			t.Fatalf("insert: %v", err)
		}
		want = append(want, seq)
	}
	return want
}

func readSeqs(t *testing.T, db *clover.DB, name string) []int64 {
	t.Helper()
	docs, err := db.FindAll(query.NewQuery(name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	out := make([]int64, 0, len(docs))
	for _, doc := range docs {
		seq, ok := doc.Get("seq").(int64)
		if !ok {
			t.Fatalf("seq not int64 after restore: %T", doc.Get("seq"))
		}
		out = append(out, seq)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestDumpRestoreSeqPrecision(t *testing.T) {
	// Arrange: rows whose seq values are UnixNano-sized.
	db1, _ := openTempStore(t)
	const table = "Orders"
	want := seedHighSeqRows(t, db1, table, 3)

	// Act: dump, then restore into a fresh database.
	outDir := t.TempDir()
	dumpPath, err := DumpJSON(db1, outDir, 3)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	db2, _ := openTempStore(t)
	if _, err := RestoreJSON(db2, dumpPath); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Assert: no seq drifted through the float64 round-trip.
	got := readSeqs(t, db2, table)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seq %d drifted: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRestoreRejectsOldNumericSeqDump(t *testing.T) {
	// Arrange: a format-1 dump - numeric seq, no format marker.
	dump := fmt.Sprintf(`{"created":"2026-01-01T00:00:00Z","collections":{"Orders":[
		{"name":"row-0","seq":%d}]}}`, time.Now().UnixNano())
	path := filepath.Join(t.TempDir(), "old-dump.json.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gz := gzip.NewWriter(f)
	gz.Write([]byte(dump))
	gz.Close()
	f.Close()

	// Act: try to restore it.
	db, _ := openTempStore(t)
	_, err = RestoreJSON(db, path)

	// Assert: rejected, and named as an older version's dump.
	if err == nil {
		t.Fatal("old numeric-seq dump should be rejected")
	}
	if !strings.Contains(err.Error(), "older nori version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestoreNamesNewerFormat(t *testing.T) {
	// Arrange: a dump written by a newer format version.
	dump := fmt.Sprintf(`{"format":%d,"created":"2026-01-01T00:00:00Z","collections":{}}`, dumpFormat+1)
	path := filepath.Join(t.TempDir(), "new-dump.json.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gz := gzip.NewWriter(f)
	gz.Write([]byte(dump))
	gz.Close()
	f.Close()

	// Act: try to restore it.
	db, _ := openTempStore(t)
	_, err = RestoreJSON(db, path)

	// Assert: rejected, and named as a newer version's dump.
	if err == nil || !strings.Contains(err.Error(), "newer nori version") {
		t.Fatalf("a newer-format dump should be named as newer: %v", err)
	}
}

func TestDumpSkipsAuditAndSessions(t *testing.T) {
	// Arrange: a database with audit and session churn plus one real table.
	db, _ := openTempStore(t)
	if err := db.CreateCollection("_audit"); err != nil {
		t.Fatalf("audit coll: %v", err)
	}
	for i := 0; i < 50; i++ {
		doc := d.NewDocument()
		doc.Set("action", "update")
		if _, err := db.InsertOne("_audit", doc); err != nil {
			t.Fatalf("audit doc: %v", err)
		}
	}
	if err := db.CreateCollection("_sessions"); err != nil {
		t.Fatalf("sessions coll: %v", err)
	}
	doc := d.NewDocument()
	doc.Set("token", "t1")
	if _, err := db.InsertOne("_sessions", doc); err != nil {
		t.Fatalf("session doc: %v", err)
	}
	if err := db.CreateCollection("Users"); err != nil {
		t.Fatalf("users coll: %v", err)
	}
	udoc := d.NewDocument()
	udoc.Set("name", "amy")
	if _, err := db.InsertOne("Users", udoc); err != nil {
		t.Fatalf("user doc: %v", err)
	}

	// Act: dump, then read the dump's collection list back.
	outDir := t.TempDir()
	dumpPath, err := DumpJSON(db, outDir, 3)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	f, err := os.Open(dumpPath)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	var dump struct {
		Collections map[string]any `json:"collections"`
	}
	if err := json.NewDecoder(gz).Decode(&dump); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Assert: audit and sessions are skipped, user data is kept.
	if _, has := dump.Collections["_audit"]; has {
		t.Fatal("dump should skip _audit")
	}
	if _, has := dump.Collections["_sessions"]; has {
		t.Fatal("dump should skip _sessions")
	}
	if _, has := dump.Collections["Users"]; !has {
		t.Fatal("dump lost user table")
	}

	// Act: restore the dump into a fresh database.
	db2, _ := openTempStore(t)
	counts, err := RestoreJSON(db2, dumpPath)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Assert: the data boots back.
	if counts["Users"] != 1 {
		t.Fatalf("restored users: %v", counts)
	}
}
