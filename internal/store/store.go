package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

const (
	metaCollection     = "_meta"
	settingsCollection = "_settings"
	defaultLimit       = 50
	maxLimit           = 1000
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid input")
	ErrConflict = errors.New("already exists")
)

// kindError carries a readable message while matching one sentinel under
// errors.Is, so the API layer can pick a status without parsing text.
type kindError struct {
	kind error
	msg  string
}

func (e kindError) Error() string        { return e.msg }
func (e kindError) Is(target error) bool { return target == e.kind }

func invalidf(format string, a ...any) error {
	return kindError{ErrInvalid, fmt.Sprintf(format, a...)}
}

func conflictf(format string, a ...any) error {
	return kindError{ErrConflict, fmt.Sprintf(format, a...)}
}

type TableInfo struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Types   []string `json:"types"`
}

type RowQuery struct {
	Limit   int
	Offset  int
	Q       string
	Sort    string
	Order   string // "asc" or "desc"
	Filters map[string]string
}

type Store struct {
	db  *clover.DB
	dir string
	mu  sync.RWMutex
	// tablesMu orders writes against whole-table rewrites: row writes
	// share it, rewrites (swaps, renames, drops) hold it alone, so a
	// rewrite never snapshots a table while a row write lands on it.
	tablesMu  sync.RWMutex
	colCache  map[string][]Column
	rowCache  map[string][]rowDoc // decoded rows in seq order
	lastSeq   int64               // guarded by mu; keeps InsertRow sequences strictly increasing
	dateOrder atomic.Value        // string: OrderDMY or OrderMDY
	beYear    atomic.Value        // bool: parsed years >= 2400 are Buddhist Era
}

// MaxCachedRowsTotal is the process-wide cache budget across tables; when
// a load or an insert pushes the total past it, whole cached tables are
// evicted largest first (an evicted table just re-scans per request). The
// per-table cap is MaxTableRows. A var so tests can shrink it.
var MaxCachedRowsTotal = 400_000

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	db, err := clover.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	for _, c := range []string{metaCollection, settingsCollection, schemaCollection} {
		if err := ensureCollection(db, c); err != nil {
			return nil, err
		}
	}
	s := &Store{db: db, dir: dir, colCache: map[string][]Column{}, rowCache: map[string][]rowDoc{}}
	s.setDateOrder(OrderDMY)
	s.beYear.Store(true)
	if vals, ok, err := s.GetSetting("dates"); err == nil && ok {
		if order := vals["order"]; order == OrderDMY || order == OrderMDY {
			s.setDateOrder(order)
		}
		if be := vals["beYear"]; be == "false" {
			s.beYear.Store(false)
		}
	}
	s.ensureSchemaVersion()
	s.recoverStaging()
	return s, nil
}

// schemaCollection holds the on-disk format stamp. It is separate from
// _settings so stamping a fresh dir does not make it look non-empty to
// IsFresh (restore refuses non-fresh dirs).
const schemaCollection = "_schema"

// engineID records the storage engine that writes the data dir; future
// upgrades can detect files written by other encodings (clover is a
// pre-release dependency).
const engineID = "clover/2.0.0-alpha.3"
const schemaVersion = "1"

// ensureSchemaVersion stamps the data dir with the schema version and
// engine that created it. The first Open writes it; later opens keep it.
func (s *Store) ensureSchemaVersion() {
	if s.SchemaVersion() != "" {
		return
	}
	doc := d.NewDocument()
	doc.Set("version", schemaVersion)
	doc.Set("engine", engineID)
	if err := s.db.Insert(schemaCollection, doc); err != nil {
		fmt.Printf("warning: could not stamp schema version: %v\n", err)
	}
}

// SchemaVersion reports the stamped on-disk format version ("" on dirs
// created before stamping existed).
func (s *Store) SchemaVersion() string {
	docs, err := s.db.FindAll(query.NewQuery(schemaCollection))
	if err != nil || len(docs) == 0 {
		return ""
	}
	v, _ := docs[0].Get("version").(string)
	return v
}

func (s *Store) setDateOrder(order string) {
	s.dateOrder.Store(order)
}

// DateOrder is the active interpretation for ambiguous slash dates.
func (s *Store) DateOrder() string {
	if order, ok := s.dateOrder.Load().(string); ok {
		return order
	}
	return OrderDMY
}

// SetDateOrder validates and persists the ambiguous-slash-date order;
// it takes effect immediately for imports and writes.
func (s *Store) SetDateOrder(order string) error {
	if order != OrderDMY && order != OrderMDY {
		return invalidf("date order must be %q or %q", OrderDMY, OrderMDY)
	}
	if err := s.saveDatesSetting(order, s.BuddhistYears()); err != nil {
		return err
	}
	s.setDateOrder(order)
	return nil
}

// BuddhistYears reports whether parsed years >= 2400 are read as Buddhist
// Era and converted back to CE (2567 -> 2024).
func (s *Store) BuddhistYears() bool {
	if on, ok := s.beYear.Load().(bool); ok {
		return on
	}
	return true
}

// SetBuddhistYears persists the Buddhist Era interpretation; it takes
// effect immediately for imports and writes.
func (s *Store) SetBuddhistYears(on bool) error {
	if err := s.saveDatesSetting(s.DateOrder(), on); err != nil {
		return err
	}
	s.beYear.Store(on)
	return nil
}

// saveDatesSetting writes the "dates" settings map, preserving whichever
// of the two options the caller did not change.
func (s *Store) saveDatesSetting(order string, beYear bool) error {
	be := "false"
	if beYear {
		be = "true"
	}
	return s.SetSetting("dates", map[string]string{"order": order, "beYear": be})
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB exposes the underlying clover database (used by auth).
func (s *Store) DB() *clover.DB {
	return s.db
}

// Dir is the data directory the store was opened with.
func (s *Store) Dir() string {
	return s.dir
}

func ensureCollection(db *clover.DB, name string) error {
	exists, err := db.HasCollection(name)
	if err != nil {
		return fmt.Errorf("check collection: %w", err)
	}
	if !exists {
		return db.CreateCollection(name)
	}
	return nil
}

// rowEntries returns a table's decoded rows in seq order, from the cache
// when it fits; larger tables fall back to a per-request scan.
func (s *Store) rowEntries(table string) ([]rowDoc, error) {
	s.mu.RLock()
	entries, ok := s.rowCache[table]
	s.mu.RUnlock()
	if ok {
		return entries, nil
	}
	entries, err := s.allRows(table)
	if err != nil {
		return nil, err
	}
	sortEntries(entries, RowQuery{}, "")
	if len(entries) <= MaxTableRows && len(entries) <= MaxCachedRowsTotal {
		s.mu.Lock()
		s.evictCacheFor(len(entries))
		s.rowCache[table] = entries
		s.mu.Unlock()
	}
	return entries, nil
}

// PauseRewrites blocks whole-table rewrites until the returned func is
// called; row writes keep flowing. Backups hold it so a dump never reads
// a table between a swap's drop and its reinsert.
func (s *Store) PauseRewrites() (resume func()) {
	s.tablesMu.RLock()
	return s.tablesMu.RUnlock
}

// evictCacheFor makes room for a table of the given size under the global
// cache budget, evicting whole cached tables largest-first. Caller holds
// s.mu. Eviction never removes data - an uncached table simply re-scans.
func (s *Store) evictCacheFor(incoming int) {
	total := incoming
	for _, entries := range s.rowCache {
		total += len(entries)
	}
	for total > MaxCachedRowsTotal {
		biggest, biggestLen := "", 0
		for name, entries := range s.rowCache {
			if len(entries) > biggestLen {
				biggest, biggestLen = name, len(entries)
			}
		}
		if biggest == "" {
			return
		}
		total -= biggestLen
		delete(s.rowCache, biggest)
	}
}

func (s *Store) invalidateRows(table string) {
	s.mu.Lock()
	delete(s.rowCache, table)
	s.mu.Unlock()
}

// patchInsert / patchUpdate / patchDelete keep a cached table's entries in
// sync after a single-row write, so reads never pay a full re-decode. They
// are no-ops when the table is not cached (oversized tables stay uncached).
// The cache invariant is seq order; inserts find their spot by seq.
func (s *Store) patchInsert(table string, seq int64, row map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, ok := s.rowCache[table]
	if !ok {
		return
	}
	entry := rowDoc{seq: seq, row: row, lrow: lowerRow(row)}
	i := sort.Search(len(entries), func(i int) bool { return entries[i].seq > entry.seq })
	entries = append(entries, rowDoc{})
	copy(entries[i+1:], entries[i:])
	entries[i] = entry
	if len(entries) > MaxTableRows {
		delete(s.rowCache, table)
		return
	}
	s.rowCache[table] = entries
	s.evictCacheFor(0)
}

// patchUpdate updates one cached entry in place. Caller must hold s.mu
// (UpdateRow holds it across its capture-and-apply sequence).
func (s *Store) patchUpdate(table, id string, row map[string]string) {
	entries, ok := s.rowCache[table]
	if !ok {
		return
	}
	for i, e := range entries {
		if e.row["id"] == id {
			entries[i].row = row
			entries[i].lrow = lowerRow(row)
			return
		}
	}
}

func (s *Store) patchDelete(table, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, ok := s.rowCache[table]
	if !ok {
		return
	}
	for i, e := range entries {
		if e.row["id"] == id {
			s.rowCache[table] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

func (s *Store) invalidateMetaCache(table string) {
	s.mu.Lock()
	delete(s.colCache, table)
	s.mu.Unlock()
}

func (s *Store) findMeta(table string) (*d.Document, error) {
	q := query.NewQuery(metaCollection).MatchFunc(func(doc *d.Document) bool {
		name, _ := doc.Get("table").(string)
		return name == table
	})
	return s.db.FindFirst(q)
}

func (s *Store) saveMeta(table string, columns []Column) error {
	defer func() {
		s.mu.Lock()
		s.colCache[table] = columns
		s.mu.Unlock()
	}()
	existing, err := s.findMeta(table)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.db.UpdateById(metaCollection, existing.ObjectId(), func(doc *d.Document) *d.Document {
			doc.Set("columns", columns)
			return doc
		})
	}
	doc := d.NewDocument()
	doc.Set("table", table)
	doc.Set("columns", columns)
	_, err = s.db.InsertOne(metaCollection, doc)
	return err
}
func (s *Store) checkTable(name string) error {
	exists, err := s.TableExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// rowDoc pairs a row with its insertion sequence, which preserves the
// original import order (clover ids are random UUIDs). lrow is the lowered
// copy used by contains-search, built once per decode instead of per request.
type rowDoc struct {
	seq  int64
	row  map[string]string
	lrow map[string]string
}

func lowerRow(row map[string]string) map[string]string {
	l := make(map[string]string, len(row))
	for k, v := range row {
		l[k] = strings.ToLower(v)
	}
	return l
}

func (s *Store) allRows(table string) ([]rowDoc, error) {
	docs, err := s.db.FindAll(query.NewQuery(table))
	if err != nil {
		return nil, err
	}
	entries := make([]rowDoc, 0, len(docs))
	for _, doc := range docs {
		row := docToRow(doc)
		entries = append(entries, rowDoc{seq: docSeq(doc), row: row, lrow: lowerRow(row)})
	}
	return entries, nil
}

func docSeq(doc *d.Document) int64 {
	switch v := doc.Get("seq").(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func docToRow(doc *d.Document) map[string]string {
	row := map[string]string{"id": doc.ObjectId()}
	for k, v := range doc.ToMap() {
		if k == "seq" || k == "_id" {
			continue
		}
		if str, ok := v.(string); ok {
			row[k] = str
		} else if v != nil {
			row[k] = fmt.Sprint(v)
		}
	}
	return row
}
func containsColumn(list []Column, name string) bool {
	for _, c := range list {
		if c.Name == name {
			return true
		}
	}
	return false
}

// GetSetting returns a string map stored under key in the _settings
// collection. ok is false when the key was never set.
func (s *Store) GetSetting(key string) (map[string]string, bool, error) {
	doc, err := findSettingDoc(s.db, key)
	if err != nil {
		return nil, false, err
	}
	if doc == nil {
		return nil, false, nil
	}
	raw, _ := doc.Get("value").(map[string]interface{})
	vals := make(map[string]string, len(raw))
	for k, v := range raw {
		if str, ok := v.(string); ok {
			vals[k] = str
		}
	}
	return vals, true, nil
}

func (s *Store) SetSetting(key string, vals map[string]string) error {
	existing, err := findSettingDoc(s.db, key)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.db.UpdateById(settingsCollection, existing.ObjectId(), func(doc *d.Document) *d.Document {
			doc.Set("value", vals)
			return doc
		})
	}
	doc := d.NewDocument()
	doc.Set("key", key)
	doc.Set("value", vals)
	_, err = s.db.InsertOne(settingsCollection, doc)
	return err
}

func (s *Store) DeleteSetting(key string) error {
	existing, err := findSettingDoc(s.db, key)
	if err != nil || existing == nil {
		return err
	}
	return s.db.DeleteById(settingsCollection, existing.ObjectId())
}

func findSettingDoc(db *clover.DB, key string) (*d.Document, error) {
	q := query.NewQuery(settingsCollection).MatchFunc(func(doc *d.Document) bool {
		k, _ := doc.Get("key").(string)
		return k == key
	})
	return db.FindFirst(q)
}
