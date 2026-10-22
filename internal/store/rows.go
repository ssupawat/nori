package store

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

func (s *Store) GetRows(table string, rq RowQuery) ([]map[string]string, int, error) {
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return nil, 0, err
	}
	columns := make([]string, len(cols))
	types := map[string]string{}
	for i, c := range cols {
		columns[i] = c.Name
		types[c.Name] = c.Type
	}
	if rq.Sort != "" && !slices.Contains(columns, rq.Sort) {
		return nil, 0, invalidf("unknown sort column %q", rq.Sort)
	}
	for col := range rq.Filters {
		if !slices.Contains(columns, col) {
			return nil, 0, invalidf("unknown filter column %q", col)
		}
	}
	limit := rq.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := rq.Offset
	if offset < 0 {
		offset = 0
	}

	entries, err := s.rowEntries(table)
	if err != nil {
		return nil, 0, err
	}

	var work []rowDoc
	if rq.Q != "" || len(rq.Filters) > 0 {
		work = filterEntries(entries, columns, rq)
	} else if rq.Sort != "" {
		work = make([]rowDoc, len(entries))
		copy(work, entries)
	} else {
		work = entries
	}
	if rq.Sort != "" {
		sortEntries(work, rq, types[rq.Sort])
	}
	total := len(work)

	if offset >= total {
		return []map[string]string{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// clone each cached row: filling the missing-column defaults must not
	// write into the shared cache (concurrent readers would race on it)
	rows := make([]map[string]string, 0, end-offset)
	for _, e := range work[offset:end] {
		rows = append(rows, withAllColumns(e.row, columns))
	}
	return rows, total, nil
}

// withAllColumns returns a copy of row holding every column, empty where
// the stored document lacks the field (rows written before the column
// existed). It copies so cached rows are never written into.
func withAllColumns(row map[string]string, columns []string) map[string]string {
	out := make(map[string]string, len(columns)+1)
	for k, v := range row {
		out[k] = v
	}
	for _, c := range columns {
		if _, ok := out[c]; !ok {
			out[c] = ""
		}
	}
	return out
}

// fillColumns is withAllColumns for a table looked up by name.
func (s *Store) fillColumns(table string, row map[string]string) (map[string]string, error) {
	columns, err := s.GetColumns(table)
	if err != nil {
		return nil, err
	}
	return withAllColumns(row, columns), nil
}

func filterEntries(entries []rowDoc, columns []string, rq RowQuery) []rowDoc {
	q := strings.ToLower(rq.Q)
	filters := make(map[string]string, len(rq.Filters))
	for col, want := range rq.Filters {
		filters[col] = strings.ToLower(want)
	}
	out := make([]rowDoc, 0, len(entries))
	for _, e := range entries {
		if !matchRow(e, columns, q, filters) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// matchRow matches against the pre-lowered copy (e.lrow).
func matchRow(e rowDoc, columns []string, q string, filters map[string]string) bool {
	if q != "" {
		found := false
		for _, col := range columns {
			if strings.Contains(e.lrow[col], q) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for col, want := range filters {
		if !strings.Contains(e.lrow[col], want) {
			return false
		}
	}
	return true
}

// sortEntries orders matched rows. Sort keys are computed once per row
// (not per comparison); unparsable values in number columns sort after all
// numbers, in string order.
func sortEntries(entries []rowDoc, rq RowQuery, sortType string) {
	if rq.Sort == "" {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
		return
	}
	type item struct {
		entry rowDoc
		num   float64
		has   bool
		sval  string
	}
	items := make([]item, len(entries))
	for i, e := range entries {
		it := item{entry: e, sval: e.row[rq.Sort]}
		if sortType == TypeNumber {
			cleaned := strings.ReplaceAll(it.sval, ",", "")
			if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
				it.num, it.has = f, true
			}
		}
		items[i] = it
	}
	less := func(a, b item) bool {
		if sortType == TypeNumber {
			switch {
			case a.has && b.has:
				return a.num < b.num
			case a.has:
				return true
			case b.has:
				return false
			}
			return a.sval < b.sval
		}
		return a.sval < b.sval
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if less(a, b) {
			return true
		}
		if less(b, a) {
			return false
		}
		return a.entry.seq < b.entry.seq
	})
	if rq.Order == "desc" {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	for i := range entries {
		entries[i] = items[i].entry
	}
}

func (s *Store) GetRow(table, id string) (map[string]string, error) {
	if err := s.checkTable(table); err != nil {
		return nil, err
	}
	doc, err := s.db.FindById(table, id)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, ErrNotFound
	}
	return s.fillColumns(table, docToRow(doc))
}

// InsertRow creates a row. Values are normalized (and strictly validated)
// against each column's type before storage.
func (s *Store) InsertRow(table string, values map[string]string) (map[string]string, error) {
	s.tablesMu.RLock()
	defer s.tablesMu.RUnlock()
	if err := s.checkTable(table); err != nil {
		return nil, err
	}
	if n, err := s.CountRows(table); err != nil {
		return nil, err
	} else if n >= MaxTableRows {
		return nil, fmt.Errorf("%w: %q holds %d rows, max is %d", ErrTableFull, table, n, MaxTableRows)
	}
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return nil, err
	}
	types := map[string]string{}
	for _, c := range cols {
		types[c.Name] = c.Type
	}
	for k, v := range values {
		norm, err := s.NormalizeColumn(types[k], v)
		if err != nil {
			return nil, invalidf("column %q: %v", k, err)
		}
		values[k] = norm
	}
	doc := d.NewDocument()
	for k, v := range values {
		doc.Set(k, v)
	}
	// keep sequences strictly increasing even on coarse clocks
	s.mu.Lock()
	seq := time.Now().UnixNano()
	if seq <= s.lastSeq {
		seq = s.lastSeq + 1
	}
	s.lastSeq = seq
	s.mu.Unlock()
	doc.Set("seq", seq)
	id, err := s.db.InsertOne(table, doc)
	if err != nil {
		return nil, err
	}
	row := map[string]string{"id": id}
	for k, v := range values {
		row[k] = v
	}
	s.patchInsert(table, seq, row)
	return withAllColumns(row, columnNames(cols)), nil
}

func columnNames(cols []Column) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// UpdateRow merges values into an existing row and returns (new row,
// old row). Old and new are captured under one lock, so callers get a
// consistent before/after pair even under concurrent updates.
func (s *Store) UpdateRow(table, id string, values map[string]string) (map[string]string, map[string]string, error) {
	s.tablesMu.RLock()
	defer s.tablesMu.RUnlock()
	if err := s.checkTable(table); err != nil {
		return nil, nil, err
	}
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return nil, nil, err
	}
	types := map[string]string{}
	names := make([]string, len(cols))
	for i, c := range cols {
		types[c.Name] = c.Type
		names[i] = c.Name
	}
	for k := range values {
		if k == "id" || !slices.Contains(names, k) {
			return nil, nil, invalidf("unknown column %q", k)
		}
	}
	for k, v := range values {
		norm, err := s.NormalizeColumn(types[k], v)
		if err != nil {
			return nil, nil, invalidf("column %q: %v", k, err)
		}
		values[k] = norm
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldDoc, err := s.db.FindById(table, id)
	if err != nil {
		return nil, nil, err
	}
	if oldDoc == nil {
		return nil, nil, ErrNotFound
	}
	old := docToRow(oldDoc)
	if err := s.db.UpdateById(table, id, func(doc *d.Document) *d.Document {
		for k, v := range values {
			doc.Set(k, v)
		}
		return doc
	}); err != nil {
		return nil, nil, err
	}
	row := map[string]string{"id": id}
	for k, v := range old {
		row[k] = v
	}
	for k, v := range values {
		row[k] = v
	}
	s.patchUpdate(table, id, row)
	return withAllColumns(row, names), old, nil
}

func (s *Store) DeleteRow(table, id string) error {
	s.tablesMu.RLock()
	defer s.tablesMu.RUnlock()
	if err := s.checkTable(table); err != nil {
		return err
	}
	doc, err := s.db.FindById(table, id)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	if err := s.db.DeleteById(table, id); err != nil {
		return err
	}
	s.patchDelete(table, id)
	return nil
}

// stagingPrefix marks the temporary collection a replace-import builds
// before it swaps the live table, so a crash mid-import never destroys the
// old data: recovery either finishes the swap or abandons the staging copy.
const stagingPrefix = "_staging_"

// ReplaceTable atomically replaces a table with the given columns and rows
// (used by import mode=replace). The new dataset is fully written into a
// staging collection first; the swap then drops the old table and copies
// staging in. If the process dies anywhere in between, Open() finishes or
// rolls back the swap from the staging copy — the old data survives unless
// the new data is complete enough to replace it.
func (s *Store) ReplaceTable(name string, columns []Column, rows []map[string]string) error {
	if strings.HasPrefix(name, "_") || strings.TrimSpace(name) == "" || !validName(name) {
		return invalidf("invalid table name %q", name)
	}
	for _, c := range columns {
		if ReservedColName(c.Name) || !validName(c.Name) {
			return invalidf("invalid column name %q (reserved or control characters)", c.Name)
		}
	}
	if len(rows) > MaxTableRows {
		return fmt.Errorf("%w: import has %d rows, max is %d", ErrTableFull, len(rows), MaxTableRows)
	}
	if len(columns) > MaxTableCols {
		return fmt.Errorf("%w: import has %d columns, max is %d", ErrTooManyCols, len(columns), MaxTableCols)
	}
	s.normalizeRows(columns, rows)

	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	staging := stagingPrefix + name
	if exists, err := s.db.HasCollection(staging); err == nil && exists {
		if err := s.db.DropCollection(staging); err != nil {
			return err
		}
	}
	if err := s.db.CreateCollection(staging); err != nil {
		return err
	}
	if err := s.saveMeta(staging, columns); err != nil {
		return err
	}
	if err := s.insertAll(staging, rows); err != nil {
		s.db.DropCollection(staging)
		s.invalidateRows(staging)
		return err
	}
	// from here the staging copy is complete: mark the swap, then commit
	if err := s.markSwap(name, false, ""); err != nil {
		return err
	}
	return s.finishSwap(name, staging, false, "")
}

// swapManifest is the settings key recording the one pending swap.
// tablesMu lets only one swap run at a time, so one key is enough.
const swapManifest = "import_staging"

// markSwap records a complete staging copy as ready to swap in. preserve
// and from travel with it so recovery repeats the same swap: keep row ids
// for in-place rewrites, and drop the old name for a rename.
func (s *Store) markSwap(name string, preserveIDs bool, from string) error {
	return s.SetSetting(swapManifest, map[string]string{
		"table":    name,
		"phase":    "swap",
		"preserve": strconv.FormatBool(preserveIDs),
		"from":     from,
	})
}

// finishSwap moves the staged dataset into the live table. It only runs
// once the staging copy is complete, so a crash at any point is repaired
// by recoverStaging on the next start. On failure it returns the error and
// KEEPS the staging collection and the manifest, so recovery can retry —
// cleanup happens only on success. preserveIDs keeps the staged rows'
// clover ids and seq values (used when rewriting a table in place, e.g.
// column renames); imports pass false to mint fresh ids. A non-empty from
// is the old name of a renamed table, dropped once the new one is filled.
func (s *Store) finishSwap(name, staging string, preserveIDs bool, from string) error {
	cols, err := s.GetColumnsMeta(staging)
	if err != nil {
		return fmt.Errorf("swap %q: read staged columns: %w", name, err)
	}
	exists, err := s.TableExists(name)
	if err != nil {
		return fmt.Errorf("swap %q: check target: %w", name, err)
	}
	if exists {
		if err := s.db.DropCollection(name); err != nil {
			return fmt.Errorf("swap %q: drop old table: %w", name, err)
		}
		s.invalidateRows(name)
		s.invalidateMetaCache(name)
	}
	if err := s.db.CreateCollection(name); err != nil {
		return fmt.Errorf("swap %q: create table: %w", name, err)
	}
	if err := s.saveMeta(name, cols); err != nil {
		return fmt.Errorf("swap %q: save columns: %w", name, err)
	}
	entries, err := s.allRows(staging)
	if err != nil {
		return fmt.Errorf("swap %q: read staged rows: %w", name, err)
	}
	sortEntries(entries, RowQuery{}, "") // preserve insertion order
	rows := make([]map[string]string, len(entries))
	for i, e := range entries {
		rows[i] = e.row
	}
	if preserveIDs {
		docs := make([]*d.Document, 0, len(entries))
		for _, e := range entries {
			doc := d.NewDocument()
			doc.Set(d.ObjectIdField, e.row["id"])
			doc.Set("seq", e.seq)
			for k, v := range e.row {
				if k == "id" {
					continue
				}
				doc.Set(k, v)
			}
			docs = append(docs, doc)
		}
		if err := s.db.Insert(name, docs...); err != nil {
			return fmt.Errorf("swap %q: insert rows: %w", name, err)
		}
	} else if err := s.insertAll(name, rows); err != nil {
		return fmt.Errorf("swap %q: insert rows: %w", name, err)
	}
	s.invalidateRows(name)
	if from != "" {
		if err := s.dropTable(from); err != nil {
			return fmt.Errorf("swap %q: drop renamed table %q: %w", name, from, err)
		}
	}
	// success only now: the swap is durable, drop the scaffolding
	s.db.DropCollection(staging)
	s.deleteMeta(staging)
	s.DeleteSetting(swapManifest)
	s.invalidateRows(staging)
	s.invalidateMetaCache(staging)
	return nil
}

// recoverStaging repairs an interrupted replace-import at startup: finish a
// marked swap from the complete staging copy, or drop an orphaned one.
func (s *Store) recoverStaging() {
	vals, ok, err := s.GetSetting(swapManifest)
	if err == nil && ok && vals["table"] != "" {
		name := vals["table"]
		staging := stagingPrefix + name
		exists, _ := s.db.HasCollection(staging)
		if exists {
			fmt.Printf("recovery: finishing interrupted rewrite of table %q\n", name)
			if err := s.finishSwap(name, staging, vals["preserve"] == "true", vals["from"]); err != nil {
				// keep the manifest and staging copy so the next start
				// retries the swap instead of losing the staged data
				fmt.Printf("recovery: swap of %q failed: %v\n", name, err)
				return
			}
		} else {
			s.DeleteSetting(swapManifest)
		}
		return
	}
	// no manifest: drop staging copies left by an aborted build
	cols, err := s.db.ListCollections()
	if err != nil {
		return
	}
	for _, c := range cols {
		if strings.HasPrefix(c, stagingPrefix) {
			s.db.DropCollection(c)
		}
	}
	// and sweep meta docs for staging collections that no longer exist
	// (left behind by imports that finished before meta cleanup existed)
	if metaDocs, err := s.db.FindAll(query.NewQuery(metaCollection)); err == nil {
		for _, doc := range metaDocs {
			name, _ := doc.Get("table").(string)
			if !strings.HasPrefix(name, stagingPrefix) {
				continue
			}
			if exists, _ := s.db.HasCollection(name); !exists {
				s.db.DeleteById(metaCollection, doc.ObjectId())
			}
		}
	}
}

// AppendTable creates the table if missing, merges new columns, and inserts
// rows (used by import mode=append).
func (s *Store) AppendTable(name string, columns []Column, rows []map[string]string) error {
	if strings.HasPrefix(name, "_") || strings.TrimSpace(name) == "" || !validName(name) {
		return invalidf("invalid table name %q", name)
	}
	for _, c := range columns {
		if ReservedColName(c.Name) || !validName(c.Name) {
			return invalidf("invalid column name %q (reserved or control characters)", c.Name)
		}
	}
	if len(columns) > MaxTableCols {
		return fmt.Errorf("%w: import has %d columns, max is %d", ErrTooManyCols, len(columns), MaxTableCols)
	}
	s.normalizeRows(columns, rows)
	// exclusive: it may create the table and rewrites its metadata
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	exists, err := s.TableExists(name)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.db.CreateCollection(name); err != nil {
			return err
		}
		if err := s.saveMeta(name, columns); err != nil {
			return err
		}
	} else {
		current, err := s.GetColumnsMeta(name)
		if err != nil {
			return err
		}
		merged := append([]Column{}, current...)
		for _, c := range columns {
			if !containsColumn(merged, c.Name) {
				merged = append(merged, c)
			}
		}
		if len(merged) > MaxTableCols {
			return fmt.Errorf("%w: merge would give %d columns, max is %d", ErrTooManyCols, len(merged), MaxTableCols)
		}
		if err := s.saveMeta(name, merged); err != nil {
			return err
		}
	}
	existing, err := s.allRows(name)
	if err != nil {
		return err
	}
	if len(existing)+len(rows) > MaxTableRows {
		return fmt.Errorf("%w: append would give %d rows, max is %d", ErrTableFull, len(existing)+len(rows), MaxTableRows)
	}
	var maxSeq int64
	for _, e := range existing {
		if e.seq > maxSeq {
			maxSeq = e.seq
		}
	}
	defer s.invalidateRows(name)
	return s.insertAllFrom(name, rows, maxSeq)
}

func (s *Store) insertAll(table string, rows []map[string]string) error {
	return s.insertAllFrom(table, rows, 0)
}

// reservedDocKeys are storage-internal document keys; row data must never
// land on them (imports rename such columns, API writes reject them — this
// guard keeps the documents consistent regardless).
var reservedDocKeys = map[string]bool{"seq": true, "_id": true, "id": true}

func (s *Store) insertAllFrom(table string, rows []map[string]string, seqBase int64) error {
	if len(rows) == 0 {
		return nil
	}
	docs := make([]*d.Document, 0, len(rows))
	for i, row := range rows {
		doc := d.NewDocument()
		for k, v := range row {
			if reservedDocKeys[k] {
				continue
			}
			doc.Set(k, v)
		}
		doc.Set("seq", seqBase+int64(i)+1)
		docs = append(docs, doc)
	}
	return s.db.Insert(table, docs...)
}

func (s *Store) CountRows(table string) (int, error) {
	entries, err := s.rowEntries(table)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// normalizeRows converts row values to the canonical form of each column
// type in place; values that do not fit their column type are left as-is
// (imports carry mixed legacy data).
func (s *Store) normalizeRows(columns []Column, rows []map[string]string) {
	for _, row := range rows {
		for _, c := range columns {
			if c.Type == TypeText {
				continue
			}
			if v, ok := row[c.Name]; ok {
				if norm, err := s.NormalizeColumn(c.Type, v); err == nil {
					row[c.Name] = norm
				}
			}
		}
	}
}
