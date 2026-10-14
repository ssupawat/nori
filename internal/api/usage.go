package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	clover "github.com/ostafen/clover/v2"
	d "github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"

	"nori/internal/store"
)

const (
	usageWindow     = 24 * time.Hour
	usageCollection = "_usage"
	flushInterval   = time.Minute
)

// usageTracker counts API requests per method+path over a rolling 24h
// window. Counts live in memory in hourly buckets and are flushed to the
// _usage collection as hourly aggregates (path + hour + count) so the
// window survives restarts.
type usageTracker struct {
	mu        sync.Mutex
	db        *clover.DB
	buckets   map[string]map[int64]int64 // path -> unixHour -> count
	dirty     map[pathHour]bool          // entries changed since last flush
	lastPrune time.Time
	disabled  bool // set after the first persistence error; counting goes memory-only
}

type pathHour struct {
	path string
	hour int64
}

func newUsageTracker(db *clover.DB) *usageTracker {
	u := &usageTracker{
		db:        db,
		buckets:   map[string]map[int64]int64{},
		dirty:     map[pathHour]bool{},
		lastPrune: time.Now(),
	}
	u.load()
	go u.flushLoop()
	return u
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

func (u *usageTracker) load() {
	if err := ensureCollection(u.db, usageCollection); err != nil {
		u.disabled = true
		return
	}
	cutoff := time.Now().Add(-usageWindow).Unix() / 3600
	q := query.NewQuery(usageCollection).MatchFunc(func(doc *d.Document) bool {
		return docInt(doc, "hour") >= cutoff
	})
	docs, err := u.db.FindAll(q)
	if err != nil {
		u.disabled = true
		return
	}
	for _, doc := range docs {
		path, _ := doc.Get("path").(string)
		hour := docInt(doc, "hour")
		count := docInt(doc, "count")
		if path == "" || hour == 0 {
			continue
		}
		if u.buckets[path] == nil {
			u.buckets[path] = map[int64]int64{}
		}
		u.buckets[path][hour] += count
	}
}

// docInt reads an integer field, which msgpack may hand back as int64 or
// float64 depending on the write path.
func docInt(doc *d.Document, field string) int64 {
	if n, ok := doc.Get(field).(int64); ok {
		return n
	}
	f, _ := doc.Get(field).(float64)
	return int64(f)
}

func (u *usageTracker) hit(key string, now time.Time) {
	hour := now.Unix() / 3600
	u.mu.Lock()
	if u.buckets[key] == nil {
		u.buckets[key] = map[int64]int64{}
	}
	u.buckets[key][hour]++
	u.dirty[pathHour{key, hour}] = true
	if now.Sub(u.lastPrune) > time.Hour {
		u.pruneLocked(now)
		u.lastPrune = now
	}
	u.mu.Unlock()
}

func (u *usageTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-usageWindow).Unix() / 3600
	for path, b := range u.buckets {
		for h := range b {
			if h < cutoff {
				delete(b, h)
			}
		}
		if len(b) == 0 {
			delete(u.buckets, path)
		}
	}
}

func (u *usageTracker) since(window time.Duration) (int64, map[string]int64) {
	cutoff := time.Now().Add(-window).Unix() / 3600
	u.mu.Lock()
	defer u.mu.Unlock()
	var total int64
	byPath := map[string]int64{}
	for path, b := range u.buckets {
		var n int64
		for h, c := range b {
			if h >= cutoff {
				n += c
			}
		}
		if n > 0 {
			total += n
			byPath[path] = n
		}
	}
	return total, byPath
}

// hourly returns per-hour request totals across all paths, oldest first
// (the last bucket is the current, still-running hour). Full-hour buckets,
// so the sum can sit slightly below the rolling 24h total.
func (u *usageTracker) hourly(n int) []int64 {
	now := time.Now().Unix() / 3600
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]int64, n)
	for _, b := range u.buckets {
		for h, c := range b {
			if h > now-int64(n) && h <= now {
				out[n-1-int(now-h)] += c
			}
		}
	}
	return out
}

// flushLoop persists dirty hourly buckets every minute.
func (u *usageTracker) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for range ticker.C {
		u.flush()
		if u.isDisabled() {
			return
		}
	}
}

func (u *usageTracker) isDisabled() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.disabled
}

func (u *usageTracker) flush() {
	u.mu.Lock()
	if u.disabled || len(u.dirty) == 0 {
		u.mu.Unlock()
		return
	}
	todo := make([]pathHour, 0, len(u.dirty))
	for ph := range u.dirty {
		todo = append(todo, ph)
	}
	u.mu.Unlock()

	for _, e := range todo {
		if err := u.upsert(e.path, e.hour); err != nil {
			u.mu.Lock()
			u.disabled = true
			u.mu.Unlock()
			return
		}
		u.mu.Lock()
		delete(u.dirty, e)
		u.mu.Unlock()
	}
}

func (u *usageTracker) upsert(path string, hour int64) error {
	u.mu.Lock()
	count := u.buckets[path][hour]
	u.mu.Unlock()
	q := query.NewQuery(usageCollection).MatchFunc(func(doc *d.Document) bool {
		p, _ := doc.Get("path").(string)
		return p == path && docInt(doc, "hour") == hour
	})
	doc, err := u.db.FindFirst(q)
	if err != nil {
		return err
	}
	if doc == nil {
		nd := d.NewDocument()
		nd.Set("path", path)
		nd.Set("hour", hour)
		nd.Set("count", count)
		_, err = u.db.InsertOne(usageCollection, nd)
		return err
	}
	return u.db.UpdateById(usageCollection, doc.ObjectId(), func(d *d.Document) *d.Document {
		d.Set("count", count)
		return d
	})
}

// trackUsage records the request after the handler ran, so chi's route
// pattern is populated. /health is excluded to keep the signal clean, and
// so are the dashboard's own calls (X-Requested-With: nori-dashboard),
// so the numbers reflect the API's real consumers.
func (sv *Server) trackUsage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Header.Get("X-Requested-With") == "nori-dashboard" {
			return
		}
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern := rc.RoutePattern()
			if pattern == "" || pattern == "/health" {
				return
			}
			key := r.Method + " " + pattern
			if t := chi.URLParam(r, "t"); t != "" {
				key = strings.Replace(key, "{t}", t, 1)
			}
			sv.usage.hit(key, time.Now())
		}
	})
}

func (sv *Server) usageReport(w http.ResponseWriter, r *http.Request) {
	total, byPath := sv.usage.since(usageWindow)
	tables, err := sv.store.ListTables()
	if err != nil {
		serverError(w, err)
		return
	}
	caps := make([]map[string]any, 0, len(tables))
	for _, t := range tables {
		n, _ := sv.store.CountRows(t.Name)
		caps = append(caps, map[string]any{"name": t.Name, "rows": n})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requests24h":  total,
		"byPath":       byPath,
		"byHour":       sv.usage.hourly(24),
		"tables":       caps,
		"maxRows":      store.MaxTableRows,
		"auditDropped": sv.audit.Dropped(),
	})
}
