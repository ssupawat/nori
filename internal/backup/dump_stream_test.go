package backup

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

func readDumpRaw(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return raw
}

// The dump is consumed by json.Unmarshal on restore and by anything else
// that reads it. Whatever writes it, the bytes must be exactly what
// json.Marshal of the decoded content produces: compact, sorted keys.
// That pins the streamed writer to the format the in-memory writer had.
func TestDumpIsCanonicalJSON(t *testing.T) {
	db, _ := openTempStore(t)
	for _, c := range []string{"t1", "empty"} {
		if err := db.CreateCollection(c); err != nil {
			t.Fatal(err)
		}
	}
	for _, vals := range []map[string]any{
		{"seq": int64(1729670400123456789), "name": "alicé <b>&</b>", "note": "", "flags": map[string]any{"ok": true, "n": 2}},
		{"seq": int64(1729670400123456790), "name": "bob", "note": "line\nbreak", "flags": nil},
	} {
		doc := d.NewDocument()
		for k, v := range vals {
			doc.Set(k, v)
		}
		if _, err := db.InsertOne("t1", doc); err != nil {
			t.Fatal(err)
		}
	}
	outDir := t.TempDir()

	path, err := DumpJSON(db, outDir, 3)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	raw := readDumpRaw(t, path)

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	canon, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(canon) != string(raw) {
		t.Fatalf("dump is not canonical JSON:\n got: %s\nwant: %s", raw, canon)
	}

	colls := decoded["collections"].(map[string]any)
	rows := colls["t1"].([]any)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	// clover iterates by doc id, not insertion order - find the row by name
	var alice map[string]any
	for _, r := range rows {
		if r.(map[string]any)["name"] == "alicé <b>&</b>" {
			alice = r.(map[string]any)
		}
	}
	if alice == nil {
		t.Fatal("row not found in dump")
	}
	if alice["seq"] != "1729670400123456789" {
		t.Fatalf("seq must travel as a string, got %v", alice["seq"])
	}
	if got := colls["empty"]; got == nil {
		t.Fatal("empty collection missing from dump")
	} else if arr, ok := got.([]any); !ok || len(arr) != 0 {
		t.Fatalf("empty collection must dump as [], got %v", got)
	}
}

// in-memory dump writer; reference output for TestDumpBytesMatchInMemoryWriter
func dumpInMemory(t *testing.T, db *clover.DB, keep int) string {
	t.Helper()
	names, err := db.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	dump := map[string]any{
		"format":      dumpFormat,
		"created":     "MASK",
		"collections": map[string]any{},
	}
	colls := dump["collections"].(map[string]any)
	for _, name := range names {
		if name == "_audit" || name == "_sessions" {
			continue
		}
		docs, err := db.FindAll(query.NewQuery(name))
		if err != nil {
			t.Fatal(err)
		}
		list := make([]map[string]any, 0, len(docs))
		for _, doc := range docs {
			m := doc.ToMap()
			if seq, ok := m["seq"].(int64); ok {
				m["seq"] = strconv.FormatInt(seq, 10)
			}
			list = append(list, m)
		}
		colls[name] = list
	}
	blob, err := json.Marshal(dump)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "ref.json.gz")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write(blob); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	f.Close()
	return out
}

func maskCreated(t *testing.T, raw []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m["created"] = "MASK"
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDumpBytesMatchInMemoryWriter(t *testing.T) {
	db, _ := openTempStore(t)
	if err := db.CreateCollection("t1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		doc := d.NewDocument()
		doc.Set("name", fmt.Sprintf("row-%d <&> é", i))
		doc.Set("seq", int64(1729670400123456789)+int64(i))
		doc.Set("meta", map[string]any{"ok": true})
		if _, err := db.InsertOne("t1", doc); err != nil {
			t.Fatal(err)
		}
	}
	outDir := t.TempDir()
	path, err := DumpJSON(db, outDir, 3)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	streamed := maskCreated(t, readDumpRaw(t, path))
	ref := dumpInMemory(t, db, 3)
	legacy := maskCreated(t, readDumpRaw(t, ref))
	if streamed != legacy {
		t.Fatalf("streamed bytes differ from the in-memory writer")
	}
}
