package backup

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

// dumpFormat is the dump file's schema version. Format 2 encodes seq as a
// string: int64 seq values (UnixNano) exceed JSON's exact float64 range and
// were silently rounded by format 1 dumps.
const dumpFormat = 2

// DumpJSON writes a logical dump of every collection (data tables, meta,
// settings, users) as gzipped JSON. It runs inside the server process via
// the public clover API, so it is safe while the server serves traffic.
// Individual collections are read in separate read transactions, so the
// dump is per-collection consistent rather than a single point in time.
// Callers hold store.PauseRewrites so no table is mid-swap while it reads.
func DumpJSON(db *clover.DB, outDir string, keep int) (string, error) {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	names, err := db.ListCollections()
	if err != nil {
		return "", err
	}
	sort.Strings(names)

	dump := map[string]any{
		"format":      dumpFormat,
		"created":     time.Now().UTC().Format(time.RFC3339),
		"collections": map[string]any{},
	}
	colls := dump["collections"].(map[string]any)
	skip := map[string]bool{
		"_audit":    true, // trail starts empty after a restore
		"_sessions": true, // restored deployments start logged out
	}
	for _, name := range names {
		if skip[name] {
			continue
		}
		docs, err := db.FindAll(query.NewQuery(name))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		list := make([]map[string]any, 0, len(docs))
		for _, doc := range docs {
			m := doc.ToMap()
			// seq is an int64 (UnixNano) - JSON numbers would round it
			// through float64 on decode, so it travels as a string
			if seq, ok := m["seq"].(int64); ok {
				m["seq"] = strconv.FormatInt(seq, 10)
			}
			list = append(list, m)
		}
		colls[name] = list
	}

	blob, err := json.Marshal(dump)
	if err != nil {
		return "", err
	}
	out := filepath.Join(outDir, fmt.Sprintf("nori-dump-%s.json.gz", time.Now().UTC().Format("20060102-150405.000000000")))
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write(blob); err != nil {
		f.Close()
		os.Remove(out)
		return "", err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(out)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(out)
		return "", err
	}

	keepOld(outDir, keep)
	return out, nil
}

// keepOld removes the oldest dumps beyond the retention count.
func keepOld(outDir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := filepath.Glob(filepath.Join(outDir, "nori-dump-*.json.gz"))
	if err != nil || len(entries) <= keep {
		return
	}
	sort.Strings(entries) // timestamps sort lexicographically
	for _, old := range entries[:len(entries)-keep] {
		os.Remove(old)
	}
}

// IsFresh reports whether a database holds no documents at all, meaning a
// restore may write into it. Restore refuses non-fresh data dirs so a
// restore can never silently mix with live data.
func IsFresh(db *clover.DB) (bool, error) {
	names, err := db.ListCollections()
	if err != nil {
		return false, err
	}
	for _, name := range names {
		if name == "_schema" {
			continue // the format stamp exists on every dir, even fresh ones
		}
		n, err := db.Count(query.NewQuery(name))
		if err != nil {
			return false, err
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}

// RestoreJSON rebuilds collections from a DumpJSON file, preserving ids.
// System collections (_users, _meta, _settings) already exist in a fresh
// data dir, so their documents are inserted into the existing collection;
// data tables are created if missing. Refuses to restore into a data dir
// where the target collection already holds documents.
func RestoreJSON(db *clover.DB, dumpPath string) (map[string]int, error) {
	f, err := os.Open(dumpPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzipped dump: %w", err)
	}
	var dump struct {
		Format      *int                        `json:"format"`
		Created     string                      `json:"created"`
		Collections map[string][]map[string]any `json:"collections"`
	}
	if err := json.NewDecoder(gz).Decode(&dump); err != nil {
		return nil, fmt.Errorf("read dump: %w", err)
	}
	switch {
	case dump.Format == nil || *dump.Format < dumpFormat:
		return nil, fmt.Errorf("dump was created by an older nori version (numeric seq); regenerate it with the current version")
	case *dump.Format > dumpFormat:
		return nil, fmt.Errorf("dump format %d was created by a newer nori version (this one reads format %d); restore it with that version", *dump.Format, dumpFormat)
	}

	counts := map[string]int{}
	for _, name := range sortedKeys(dump.Collections) {
		exists, err := db.HasCollection(name)
		if err != nil {
			return nil, err
		}
		if !exists {
			if err := db.CreateCollection(name); err != nil {
				return nil, err
			}
		}
		list := dump.Collections[name]
		docs := make([]*d.Document, 0, len(list))
		for _, m := range list {
			doc := d.NewDocument()
			for k, v := range m {
				if k == d.ObjectIdField {
					doc.Set(d.ObjectIdField, v)
					continue
				}
				if k == "seq" {
					// format 2 carries seq as a string to survive JSON's
					// float64 rounding of int64 values
					str, ok := v.(string)
					if !ok {
						return nil, fmt.Errorf("restore %s: seq must be a string (format %d)", name, dumpFormat)
					}
					n, err := strconv.ParseInt(str, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("restore %s: bad seq %q: %w", name, str, err)
					}
					doc.Set("seq", n)
					continue
				}
				doc.Set(k, v)
			}
			docs = append(docs, doc)
		}
		if len(docs) > 0 {
			if err := db.Insert(name, docs...); err != nil {
				return nil, err
			}
		}
		counts[name] = len(docs)
	}
	return counts, nil
}

func sortedKeys(m map[string][]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // system collections first ("_" sorts before letters)
	return keys
}
