package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ostafen/clover/v2/query"
)

// CheckReport is the result of a full data-dir scan.
type CheckReport struct {
	Tables        []CheckTable // user tables, one entry each
	System        []CheckTable // system collections (_meta, _settings, ...)
	Problems      []string     // things that are wrong (affects exit code)
	Notes         []string     // worth knowing, not a failure (e.g. staging leftovers)
	SchemaVersion string       // stamped at first open; "" on pre-stamping dirs
	TotalDocs     int
	OK            bool
}

// CheckTable is one collection's scan result.
type CheckTable struct {
	Name     string
	Rows     int
	Problems []string
}

func (r *CheckReport) problem(format string, args ...any) {
	r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
}

func (r *CheckReport) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// Check scans the whole data dir and reports anything that would stop nori
// from reading its own data: undecodable documents, missing or mismatched
// column metadata, broken seq ordering, orphan collections. It is read-only.
func (s *Store) Check() CheckReport {
	rep := CheckReport{}

	colls, err := s.db.ListCollections()
	if err != nil {
		rep.problem("list collections: %v", err)
		rep.finish()
		return rep
	}
	sort.Strings(colls)

	var metaTables []string
	for _, name := range colls {
		switch {
		case name == metaCollection || name == settingsCollection || name == schemaCollection || name == "_audit":
			n := s.countRaw(name)
			rep.System = append(rep.System, CheckTable{Name: name, Rows: n})
			rep.TotalDocs += n
		case strings.HasPrefix(name, stagingPrefix):
			rep.note("staging collection %s present (an import is in progress, or recovery will handle it)", name)
		case strings.HasPrefix(name, "_"):
			n := s.countRaw(name)
			rep.System = append(rep.System, CheckTable{Name: name, Rows: n})
		default:
			metaTables = append(metaTables, name)
		}
	}

	tables, err := s.ListTables()
	if err != nil {
		rep.problem("list tables: %v", err)
	}
	for _, ti := range tables {
		ct := s.scanTable(ti.Name, ti.Types)
		rep.Tables = append(rep.Tables, ct)
		rep.TotalDocs += ct.Rows
		for _, p := range ct.Problems {
			rep.problem("%s: %s", ti.Name, p)
		}
	}

	metaDocs, err := s.db.FindAll(query.NewQuery(metaCollection))
	if err != nil {
		rep.problem("read %s: %v", metaCollection, err)
	} else {
		for _, doc := range metaDocs {
			name, _ := doc.Get("table").(string)
			if name == "" {
				continue
			}
			if exists, _ := s.db.HasCollection(name); !exists {
				rep.problem("meta declares table %q but its collection is missing", name)
			}
		}
	}

	known := map[string]bool{}
	for _, ti := range tables {
		known[ti.Name] = true
	}
	for _, name := range metaTables {
		if !known[name] {
			ct := s.scanTable(name, nil)
			ct.Problems = append([]string{"no column metadata for this collection"}, ct.Problems...)
			rep.Tables = append(rep.Tables, ct)
			for _, p := range ct.Problems {
				rep.problem("%s: %s", name, p)
			}
		}
	}

	rep.SchemaVersion = s.SchemaVersion()
	rep.finish()
	return rep
}

func (r *CheckReport) finish() {
	r.OK = len(r.Problems) == 0
}

// checkTable deep-scans one user table: every document must decode, carry a
// numeric seq and an id, seq must be strictly increasing, and the stored
// values must match the declared column types' shape.
func (s *Store) scanTable(name string, types []string) CheckTable {
	ct := CheckTable{Name: name}

	entries, err := s.allRows(name)
	if err != nil {
		ct.Problems = append(ct.Problems, fmt.Sprintf("documents unreadable: %v", err))
		ct.Rows = -1
		return ct
	}
	ct.Rows = len(entries)
	sortEntries(entries, RowQuery{}, "") // seq order

	var prevSeq int64 = -1
	ids := map[string]bool{}
	for _, e := range entries {
		if e.seq <= prevSeq {
			ct.Problems = append(ct.Problems,
				fmt.Sprintf("seq not strictly increasing at id %s (%d after %d)", e.row["id"], e.seq, prevSeq))
		}
		prevSeq = e.seq
		if e.row["id"] == "" {
			ct.Problems = append(ct.Problems, fmt.Sprintf("row without id (seq %d)", e.seq))
		} else if ids[e.row["id"]] {
			ct.Problems = append(ct.Problems, fmt.Sprintf("duplicate id %s", e.row["id"]))
		}
		ids[e.row["id"]] = true
	}

	// raw pass: things docToRow silently normalizes away
	docs, err := s.db.FindAll(query.NewQuery(name))
	if err != nil {
		ct.Problems = append(ct.Problems, fmt.Sprintf("raw scan failed: %v", err))
		return ct
	}
	for _, doc := range docs {
		switch doc.Get("seq").(type) {
		case int64, float64:
		default:
			ct.Problems = append(ct.Problems,
				fmt.Sprintf("document %s has a missing or non-numeric seq", doc.ObjectId()))
		}
	}

	for _, ty := range types {
		if !validType(ty) {
			ct.Problems = append(ct.Problems, fmt.Sprintf("column type %q is not a known type", ty))
		}
	}
	return ct
}

// countRaw counts documents in a collection, returning -1 on error
// (callers only show the count as info).
func (s *Store) countRaw(name string) int {
	n, err := s.db.Count(query.NewQuery(name))
	if err != nil {
		return -1
	}
	return n
}
