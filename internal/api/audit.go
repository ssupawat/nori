package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
)

const auditCollection = "_audit"

// Tunables are package vars so tests can shrink them.
var (
	auditFlushInterval = 30 * time.Second
	auditFlushSize     = 100   // flush early once the queue holds this many
	auditMaxEntries    = 10000 // retention cap for the _audit collection
	auditQueueMax      = 10000 // in-memory queue cap while the DB rejects writes
)

// auditEntry records who changed what. For create it carries the row's
// values, for update only the changed fields (new values), for delete the
// row as it was.
type auditEntry struct {
	At     string            `json:"at"` // UTC RFC3339Nano; sorts chronologically
	User   string            `json:"user"`
	Action string            `json:"action"`
	Table  string            `json:"table"`
	RowID  string            `json:"rowId,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

type auditLog struct {
	db      *clover.DB
	mu      sync.Mutex
	queue   []auditEntry
	stop    chan struct{}
	kick    chan struct{} // wakes the flush loop early; buffered so add never blocks
	failing atomic.Bool   // last flush failed: wait for the ticker instead of retrying per write
	total   atomic.Int64  // approximate _audit document count
	dropped atomic.Int64  // events discarded while the DB rejected writes
}

// Dropped reports how many audit events were discarded because the queue
// hit its cap during a persistent write failure (e.g. disk full).
func (a *auditLog) Dropped() int64 { return a.dropped.Load() }

func newAuditLog(db *clover.DB) *auditLog {
	a := &auditLog{db: db, stop: make(chan struct{}), kick: make(chan struct{}, 1)}
	if exists, err := db.HasCollection(auditCollection); err == nil && !exists {
		if err := db.CreateCollection(auditCollection); err != nil {
			return a // audit degrades to a no-op queue if the collection is unavailable
		}
	}
	if n, err := db.Count(query.NewQuery(auditCollection)); err == nil {
		a.total.Store(int64(n))
	}
	go a.loop()
	return a
}

// add queues an event for the background flush loop and never does I/O
// itself: a full queue only wakes the loop. When the DB rejects writes and
// the queue hits its cap, the oldest queued event is dropped so a long
// outage cannot grow memory without bound.
func (a *auditLog) add(e auditEntry) {
	a.mu.Lock()
	for len(a.queue) >= auditQueueMax {
		a.queue = a.queue[1:]
		a.dropped.Add(1)
	}
	a.queue = append(a.queue, e)
	over := len(a.queue) >= auditFlushSize
	a.mu.Unlock()
	if over && !a.failing.Load() {
		select {
		case a.kick <- struct{}{}:
		default: // a wake-up is already pending
		}
	}
}

func (a *auditLog) loop() {
	ticker := time.NewTicker(auditFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.flush()
		case <-a.kick:
			a.flush()
		case <-a.stop:
			a.flush()
			return
		}
	}
}

// flush inserts queued events in one batch transaction and applies the
// retention cap. If the insert fails, the events go back to the front of
// the queue for the next flush.
func (a *auditLog) flush() {
	a.mu.Lock()
	if len(a.queue) == 0 {
		a.mu.Unlock()
		return
	}
	batch := a.queue
	a.queue = nil
	a.mu.Unlock()

	docs := make([]*d.Document, 0, len(batch))
	for _, e := range batch {
		doc := d.NewDocument()
		doc.Set("at", e.At)
		doc.Set("user", e.User)
		doc.Set("action", e.Action)
		doc.Set("table", e.Table)
		if e.RowID != "" {
			doc.Set("rowId", e.RowID)
		}
		if len(e.Values) > 0 {
			doc.Set("values", e.Values)
		}
		docs = append(docs, doc)
	}
	if err := a.db.Insert(auditCollection, docs...); err != nil {
		a.mu.Lock()
		a.queue = append(batch, a.queue...)
		a.mu.Unlock()
		a.failing.Store(true)
		return
	}
	a.failing.Store(false)
	a.total.Add(int64(len(docs)))
	a.enforceRetention()
}

// enforceRetention trims the collection down to auditMaxEntries, oldest
// entries first ("at" is UTC RFC3339Nano, so string order is time order).
func (a *auditLog) enforceRetention() {
	n, err := a.db.Count(query.NewQuery(auditCollection))
	if err != nil || n <= auditMaxEntries {
		return
	}
	docs, err := a.db.FindAll(query.NewQuery(auditCollection))
	if err != nil {
		return
	}
	sort.Slice(docs, func(i, j int) bool {
		a1, _ := docs[i].Get("at").(string)
		a2, _ := docs[j].Get("at").(string)
		return a1 < a2
	})
	keep := auditMaxEntries * 9 / 10
	for _, doc := range docs[:n-keep] {
		_ = a.db.DeleteById(auditCollection, doc.ObjectId())
	}
	a.total.Store(int64(keep))
}

// listAudit answers GET /api/audit (admin): newest-first audit entries,
// optionally scoped to a table and/or a single row.
func (sv *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	rowID := r.URL.Query().Get("rowId")
	actor := r.URL.Query().Get("user")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	q := query.NewQuery(auditCollection).MatchFunc(func(doc *d.Document) bool {
		if table != "" {
			if t, _ := doc.Get("table").(string); t != table {
				return false
			}
		}
		if rowID != "" {
			if rid, _ := doc.Get("rowId").(string); rid != rowID {
				return false
			}
		}
		if actor != "" {
			if u, _ := doc.Get("user").(string); u != actor {
				return false
			}
		}
		return true
	})
	docs, err := sv.audit.db.FindAll(q)
	if err != nil {
		serverError(w, err)
		return
	}
	sort.Slice(docs, func(i, j int) bool {
		a1, _ := docs[i].Get("at").(string)
		a2, _ := docs[j].Get("at").(string)
		return a1 > a2
	})
	if len(docs) > limit {
		docs = docs[:limit]
	}

	entries := make([]auditEntry, 0, len(docs))
	for _, doc := range docs {
		e := auditEntry{}
		if v, err := json.Marshal(doc.ToMap()); err == nil {
			_ = json.Unmarshal(v, &e)
		}
		entries = append(entries, e)
	}
	writeJSON(w, http.StatusOK, entries)
}
