package store

import (
	"fmt"
	"sort"
	"strings"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

// CompactStat describes one copied collection.
type CompactStat struct {
	Collection string
	Docs       int
}

// CompactInto copies every collection of the store into a fresh clover
// database at destDir, preserving document ids, seq values and fields
// verbatim. Staging collections (incomplete imports) are skipped and
// reported. The destination database is closed before returning; the
// caller swaps directories. This is nori's VACUUM: bbolt never returns
// freed pages, so reclaiming space means rebuilding the file.
func (s *Store) CompactInto(destDir string) (stats []CompactStat, skipped []string, err error) {
	dest, err := clover.Open(destDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", destDir, err)
	}
	defer dest.Close()

	names, err := s.db.ListCollections()
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(names)

	for _, name := range names {
		if strings.HasPrefix(name, stagingPrefix) {
			skipped = append(skipped, name)
			continue
		}
		docs, err := s.db.FindAll(query.NewQuery(name))
		if err != nil {
			return nil, skipped, fmt.Errorf("read %s: %w", name, err)
		}
		if err := dest.CreateCollection(name); err != nil {
			return nil, skipped, fmt.Errorf("create %s: %w", name, err)
		}
		out := make([]*d.Document, 0, len(docs))
		for _, doc := range docs {
			out = append(out, cloneDoc(doc))
		}
		if len(out) > 0 {
			if err := dest.Insert(name, out...); err != nil {
				return nil, skipped, fmt.Errorf("insert %s: %w", name, err)
			}
		}
		stats = append(stats, CompactStat{Collection: name, Docs: len(out)})
	}
	return stats, skipped, nil
}

// cloneDoc rebuilds a document with its original clover id and every field,
// so the copy is byte-for-byte the same content on fresh pages.
func cloneDoc(doc *d.Document) *d.Document {
	out := d.NewDocument()
	out.Set(d.ObjectIdField, doc.ObjectId())
	for k, v := range doc.ToMap() {
		if k == d.ObjectIdField {
			continue
		}
		out.Set(k, v)
	}
	return out
}
