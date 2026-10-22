package store

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	d "github.com/ostafen/clover/v2/document"
)

func (s *Store) ListTables() ([]TableInfo, error) {
	names, err := s.db.ListCollections()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	tables := make([]TableInfo, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "_") {
			continue
		}
		cols, err := s.GetColumnsMeta(name)
		if err != nil {
			return nil, err
		}
		info := TableInfo{Name: name, Columns: make([]string, len(cols)), Types: make([]string, len(cols))}
		for i, c := range cols {
			info.Columns[i] = c.Name
			info.Types[i] = c.Type
		}
		tables = append(tables, info)
	}
	return tables, nil
}

func (s *Store) TableExists(name string) (bool, error) {
	if strings.HasPrefix(name, "_") {
		return false, nil
	}
	return s.db.HasCollection(name)
}

func (s *Store) GetColumns(table string) ([]string, error) {
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names, nil
}

// GetColumnsMeta returns columns with their type hints. Old metadata that
// stored bare names is read back as text columns.
func (s *Store) GetColumnsMeta(table string) ([]Column, error) {
	s.mu.RLock()
	cols, ok := s.colCache[table]
	s.mu.RUnlock()
	if ok {
		return cols, nil
	}
	doc, err := s.findMeta(table)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, ErrNotFound
	}
	raw, _ := doc.Get("columns").([]interface{})
	parsed := make([]Column, 0, len(raw))
	for _, v := range raw {
		switch item := v.(type) {
		case string:
			parsed = append(parsed, Column{Name: item, Type: TypeText})
		case map[string]interface{}:
			name, _ := item["name"].(string)
			colType, _ := item["type"].(string)
			if !validType(colType) {
				colType = TypeText
			}
			parsed = append(parsed, Column{Name: name, Type: colType})
		}
	}
	s.mu.Lock()
	s.colCache[table] = parsed
	s.mu.Unlock()
	return parsed, nil
}

// AddColumn appends a column; an empty type means text.
func (s *Store) AddColumn(table, column, colType string) error {
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(table); err != nil {
		return err
	}
	column = strings.TrimSpace(column)
	if column == "" {
		return invalidf("invalid column name %q (empty)", column)
	}
	if ReservedColName(column) {
		return invalidf("invalid column name %q (reserved)", column)
	}
	if !validName(column) {
		return invalidf("invalid column name %q (control characters)", column)
	}
	if colType == "" {
		colType = TypeText
	}
	if !validType(colType) {
		return invalidf("invalid column type %q", colType)
	}
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return err
	}
	for _, c := range cols {
		if c.Name == column {
			return conflictf("column %q already exists", column)
		}
	}
	if len(cols) >= MaxTableCols {
		return fmt.Errorf("%w: %d columns, max is %d", ErrTooManyCols, len(cols)+1, MaxTableCols)
	}
	return s.saveMeta(table, append(cols, Column{Name: column, Type: colType}))
}

// SetColumnType updates a column's type hint. With convert=true, every
// stored value of the column is rewritten to the new canonical form in a
// single batch; values that cannot be converted are left untouched.
func (s *Store) SetColumnType(table, column, colType string, convert bool) error {
	if !validType(colType) {
		return invalidf("invalid column type %q", colType)
	}
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(table); err != nil {
		return err
	}
	cols, err := s.GetColumnsMeta(table)
	if err != nil {
		return err
	}
	for i, c := range cols {
		if c.Name != column {
			continue
		}
		updated := make([]Column, len(cols))
		copy(updated, cols)
		updated[i].Type = colType
		if !convert {
			return s.saveMeta(table, updated)
		}
		// type and data must change together: the live meta is only saved
		// once the converted copy has been swapped in
		docs, err := s.copyDocsNormalized(table, column, colType)
		if err != nil {
			return err
		}
		return s.stageAndSwap(table, updated, docs, "")
	}
	return ErrNotFound
}

// copyDocsNormalized snapshots a table, normalizing one column's values to
// the canonical form of the given type (values that fail stay as-is).
func (s *Store) copyDocsNormalized(table, column, colType string) ([]*d.Document, error) {
	entries, err := s.allRows(table)
	if err != nil {
		return nil, err
	}
	sortEntries(entries, RowQuery{}, "")
	docs := make([]*d.Document, 0, len(entries))
	for _, e := range entries {
		doc := d.NewDocument()
		doc.Set(d.ObjectIdField, e.row["id"])
		doc.Set("seq", e.seq)
		for k, v := range e.row {
			if k == "id" {
				continue
			}
			if k == column {
				if norm, err := s.NormalizeColumn(colType, v); err == nil {
					doc.Set(k, norm)
					continue
				}
			}
			doc.Set(k, v)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (s *Store) DeleteTable(name string) error {
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(name); err != nil {
		return err
	}
	return s.dropTable(name)
}

// dropTable removes a table's collection, then its metadata (in that
// order: metadata without a collection is harmless, a collection without
// metadata breaks listing). A missing collection is not an error, so a
// retried swap can call it again.
func (s *Store) dropTable(name string) error {
	if exists, err := s.db.HasCollection(name); err != nil {
		return err
	} else if exists {
		if err := s.db.DropCollection(name); err != nil {
			return err
		}
	}
	s.invalidateRows(name)
	return s.deleteMeta(name)
}

func (s *Store) deleteMeta(table string) error {
	s.invalidateMetaCache(table)
	meta, err := s.findMeta(table)
	if err != nil {
		return err
	}
	if meta != nil {
		return s.db.DeleteById(metaCollection, meta.ObjectId())
	}
	return nil
}

// CreateTable creates an empty table with typed columns.
func (s *Store) CreateTable(name string, cols []Column) error {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "_") || name == "" {
		return invalidf("invalid table name %q", name)
	}
	if !validName(name) {
		return invalidf("invalid table name %q (control characters)", name)
	}
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	exists, err := s.TableExists(name)
	if err != nil {
		return err
	}
	if exists {
		return conflictf("table %q already exists", name)
	}
	if len(cols) == 0 {
		return invalidf("table needs at least one column")
	}
	if len(cols) > MaxTableCols {
		return fmt.Errorf("%w: %d columns, max is %d", ErrTooManyCols, len(cols), MaxTableCols)
	}
	seen := map[string]bool{}
	for i := range cols {
		cols[i].Name = strings.TrimSpace(cols[i].Name)
		if cols[i].Name == "" || ReservedColName(cols[i].Name) {
			return invalidf("invalid column name %q (reserved)", cols[i].Name)
		}
		if !validName(cols[i].Name) {
			return invalidf("invalid column name %q (control characters)", cols[i].Name)
		}
		if !validType(cols[i].Type) {
			cols[i].Type = TypeText
		}
		if seen[cols[i].Name] {
			return invalidf("duplicate column %q", cols[i].Name)
		}
		seen[cols[i].Name] = true
	}
	if err := s.db.CreateCollection(name); err != nil {
		return err
	}
	return s.saveMeta(name, cols)
}

// RenameTable stages every row (ids and order preserved via seq/_id) under
// the new name, then swaps it in and drops the old table - the same
// crash-safe pattern as every whole-table rewrite.
func (s *Store) RenameTable(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if strings.HasPrefix(newName, "_") || newName == "" {
		return invalidf("invalid table name %q", newName)
	}
	if !validName(newName) {
		return invalidf("invalid table name %q (control characters)", newName)
	}
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(oldName); err != nil {
		return err
	}
	if newName == oldName {
		return nil
	}
	exists, err := s.TableExists(newName)
	if err != nil {
		return err
	}
	if exists {
		return conflictf("table %q already exists", newName)
	}

	columns, err := s.GetColumnsMeta(oldName)
	if err != nil {
		return err
	}
	docs, err := s.copyDocs(oldName, [2]string{})
	if err != nil {
		return err
	}
	return s.stageAndSwap(newName, columns, docs, oldName)
}

// copyDocs snapshots a table's documents, applying the optional column
// rename (oldCol -> newCol) to every document; an empty newCol with a
// non-empty oldCol drops that column instead. Ids and seq are preserved.
func (s *Store) copyDocs(table string, colRename [2]string) ([]*d.Document, error) {
	entries, err := s.allRows(table)
	if err != nil {
		return nil, err
	}
	sortEntries(entries, RowQuery{}, "")
	docs := make([]*d.Document, 0, len(entries))
	for _, e := range entries {
		doc := d.NewDocument()
		doc.Set(d.ObjectIdField, e.row["id"])
		doc.Set("seq", e.seq)
		for k, v := range e.row {
			if k == "id" {
				continue
			}
			if colRename[0] != "" && k == colRename[0] {
				if colRename[1] != "" {
					doc.Set(colRename[1], v)
				}
				continue
			}
			doc.Set(k, v)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// RenameColumn rewrites the whole table in a single batch (same pattern as
// RenameTable) with the column key renamed, then updates the metadata. Row
// ids are preserved.
func (s *Store) RenameColumn(table, oldCol, newCol string) error {
	newCol = strings.TrimSpace(newCol)
	if newCol == "" || ReservedColName(newCol) {
		return invalidf("invalid column name %q (reserved)", newCol)
	}
	if !validName(newCol) {
		return invalidf("invalid column name %q (control characters)", newCol)
	}
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(table); err != nil {
		return err
	}
	columns, err := s.GetColumnsMeta(table)
	if err != nil {
		return err
	}
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = c.Name
	}
	if !slices.Contains(names, oldCol) {
		return ErrNotFound
	}
	if oldCol == newCol {
		return nil
	}
	if slices.Contains(names, newCol) {
		return conflictf("column %q already exists", newCol)
	}

	docs, err := s.copyDocs(table, [2]string{oldCol, newCol})
	if err != nil {
		return err
	}
	renamed := make([]Column, len(columns))
	for i, c := range columns {
		if c.Name == oldCol {
			renamed[i] = Column{Name: newCol, Type: c.Type}
		} else {
			renamed[i] = c
		}
	}
	return s.stageAndSwap(table, renamed, docs, "")
}

// DeleteColumn drops a column and its values from every row, rewriting the
// table in a single batch like RenameColumn. Row ids are preserved. The
// last column cannot be dropped: a table always has at least one.
func (s *Store) DeleteColumn(table, column string) error {
	s.tablesMu.Lock()
	defer s.tablesMu.Unlock()
	if err := s.checkTable(table); err != nil {
		return err
	}
	columns, err := s.GetColumnsMeta(table)
	if err != nil {
		return err
	}
	if !containsColumn(columns, column) {
		return ErrNotFound
	}
	if len(columns) == 1 {
		return invalidf("cannot delete the last column of %q", table)
	}
	docs, err := s.copyDocs(table, [2]string{column, ""})
	if err != nil {
		return err
	}
	kept := make([]Column, 0, len(columns)-1)
	for _, c := range columns {
		if c.Name != column {
			kept = append(kept, c)
		}
	}
	return s.stageAndSwap(table, kept, docs, "")
}

// stageAndSwap rewrites a table's documents: the rewritten copy lands in a
// staging collection first and the live table is only dropped once that
// copy is complete. Ids and seq values are preserved; a crash before the
// swap is repaired by recoverStaging on the next start. A non-empty from
// names a table the swap replaces under a new name (rename). Caller holds
// tablesMu.
func (s *Store) stageAndSwap(name string, cols []Column, docs []*d.Document, from string) error {
	staging := stagingPrefix + name
	if exists, err := s.db.HasCollection(staging); err == nil && exists {
		if err := s.db.DropCollection(staging); err != nil {
			return err
		}
	}
	if err := s.db.CreateCollection(staging); err != nil {
		return err
	}
	if err := s.saveMeta(staging, cols); err != nil {
		s.db.DropCollection(staging)
		return err
	}
	if len(docs) > 0 {
		if err := s.db.Insert(staging, docs...); err != nil {
			s.db.DropCollection(staging)
			return err
		}
	}
	if err := s.markSwap(name, true, from); err != nil {
		s.db.DropCollection(staging)
		return err
	}
	return s.finishSwap(name, staging, true, from)
}

// ReservedColName reports names that collide with storage-internal keys:
// id holds the row UUID, seq keeps insertion order, _id is the clover key.
func ReservedColName(name string) bool {
	return name == "id" || name == "seq" || name == "_id"
}

// validName rejects control characters (which break JSON display and
// response headers) in user-provided table and column names.
func validName(name string) bool {
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
