package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"nori/internal/auth"
	"nori/internal/store"
)

type Server struct {
	store        *store.Store
	auth         *auth.Service
	branding     Branding // effective branding (flags + stored overrides)
	flagBranding Branding // as given by -brand-* flags, used by the reset
	usage        *usageTracker
	audit        *auditLog
	trace        bool // log per-stage timings (auth / handler / total)
	rateLimit    int  // max API requests per visitor per minute; 0 = unlimited
	// statusProvider fills GET /api/status; main installs a closure over
	// its version, uptime, data dir and backup state
	statusProvider func() *Status
	// backupNow runs an immediate dump; nil or failing when -backup-dir is off
	backupNow func() (string, error)
	// importMu serializes imports: two concurrent mode=replace imports on
	// the same table would race on the shared staging collection
	importMu sync.Mutex
}

func NewServer(s *store.Store, a *auth.Service) *Server {
	sv := &Server{store: s, auth: a, usage: newUsageTracker(s.DB()), audit: newAuditLog(s.DB())}
	// default /api/status fills everything the store knows; main installs
	// a fuller provider via WithStatus (version, uptime, backup state)
	sv.statusProvider = func() *Status {
		return &Status{
			Version:       "dev",
			DataDir:       s.Dir(),
			DataBytes:     DirSize(s.Dir()),
			DiskFreeBytes: DiskFree(s.Dir()),
		}
	}
	return sv
}

// WithTrace enables per-stage timing logs (chain / auth / handler / total).
func (sv *Server) WithTrace(on bool) *Server {
	sv.trace = on
	return sv
}

// WithRateLimit caps API requests per visitor per minute (0 = unlimited).
func (sv *Server) WithRateLimit(perMinute int) *Server {
	sv.rateLimit = perMinute
	return sv
}

// rateLimit returns the middleware capping API requests per visitor.
// /api/health is exempt (machine probes must never 429).
func (sv *Server) rateLimitMW() func(http.Handler) http.Handler {
	if sv.rateLimit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	limited := httprate.LimitBy(sv.rateLimit, time.Minute,
		func(r *http.Request) (string, error) { return clientIP(r), nil },
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
	return func(next http.Handler) http.Handler {
		limitedMW := limited(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/health" || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			limitedMW.ServeHTTP(w, r)
		})
	}
}

// WithTrustProxy enables X-Forwarded-For client IP detection (rate
// limiting keys per visitor behind a reverse proxy).
func (sv *Server) WithTrustProxy(on bool) *Server {
	trustProxy = on
	return sv
}

// WithStatus installs the provider behind GET /api/status.
func (sv *Server) WithStatus(fn func() *Status) *Server {
	sv.statusProvider = fn
	return sv
}

// WithBackupNow installs the immediate-dump action behind POST /api/backup.
func (sv *Server) WithBackupNow(fn func() (string, error)) *Server {
	sv.backupNow = fn
	return sv
}

// WithBranding sets the white-label options (chain after NewServer).
func (sv *Server) WithBranding(b Branding) *Server {
	sv.flagBranding = b.withDefaults()
	sv.branding = b.withDefaults()
	return sv
}

func (sv *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, cors, sv.rateLimitMW(), sv.trackUsage, sv.traceStage)

	// stored branding overrides (set from the dashboard) win over flags
	if vals, ok, err := sv.store.GetSetting("branding"); err == nil && ok {
		if v := vals["name"]; v != "" {
			sv.branding.Name = v
		}
		if v := vals["tagline"]; v != "" {
			sv.branding.Tagline = v
		}
		if v := vals["color"]; v != "" {
			sv.branding.Color = v
		}
		if v := vals["logoUrl"]; v != "" {
			sv.branding.LogoURL = v
		}
	}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/branding", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, sv.branding.withDefaults())
	})
	r.With(sv.requireAdmin).Patch("/branding", sv.updateBranding)
	r.With(sv.requireAdmin).Delete("/branding", sv.resetBranding)
	r.With(sv.requireAdmin).Get("/settings/dates", sv.getDatesOrder)
	r.With(sv.requireAdmin).Put("/settings/dates", sv.updateDatesOrder)
	r.With(loginRateLimit).Post("/login", sv.login)
	r.With(sv.requireAny).Get("/me", sv.me)
	r.With(sv.requireAny).Post("/me/password", sv.changeOwnPassword)

	r.With(sv.requireAny).Post("/logout", sv.logout)
	r.With(sv.requireAny).Get("/tables", sv.listTables)
	r.With(sv.requireAdmin).Post("/tables", sv.createTable)
	r.Route("/tables/{t}", func(r chi.Router) {
		r.With(sv.requireViewer).Get("/rows", sv.listRows)
		r.With(sv.requireViewer).Get("/rows/{id}", sv.getRow)
		r.With(sv.requireViewer).Get("/export", sv.exportTable)

		r.Group(func(r chi.Router) {
			r.Use(sv.requireEditor)
			r.Post("/rows", sv.createRow)
			r.Patch("/rows/{id}", sv.updateRow)
			r.Delete("/rows/{id}", sv.deleteRow)
		})
		r.With(sv.requireAdmin).Post("/columns", sv.addColumn)
		r.With(sv.requireAdmin).Patch("/columns/{col}", sv.renameColumn)
		r.With(sv.requireAdmin).Delete("/columns/{col}", sv.deleteColumn)
		r.With(sv.requireAdmin).Patch("/", sv.renameTable)
		r.With(sv.requireAdmin).Delete("/", sv.deleteTable)
	})

	r.Route("/import", func(r chi.Router) {
		r.Use(sv.requireAdmin)
		r.Post("/", sv.importXlsx)
	})

	r.Route("/users", func(r chi.Router) {
		r.Use(sv.requireAdmin)
		r.Get("/", sv.listUsers)
		r.Post("/", sv.createUser)
		r.Patch("/{name}", sv.updateUser)
		r.Delete("/{name}", sv.deleteUser)
	})

	r.Route("/keys", func(r chi.Router) {
		r.Use(sv.requireAdmin)
		r.Get("/", sv.listKeys)
		r.Post("/", sv.createKey)
		r.Delete("/{id}", sv.deleteKey)
	})

	r.With(sv.requireAdmin).Get("/usage", sv.usageReport)
	r.With(sv.requireAdmin).Get("/audit", sv.listAudit)
	r.With(sv.requireAdmin).Get("/status", sv.status)
	r.With(sv.requireAdmin).Post("/backup", sv.runBackup)
	r.With(sv.requireAdmin).Get("/sample.xlsx", sv.sampleWorkbook)
	return r
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets the hardening headers on every response. The CSP
// shape is recorded in ARCHITECTURE.md.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src * data:; connect-src 'self'; font-src 'self'; object-src 'none'; "+
				"base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type ctxKey string

const userKey ctxKey = "user"
const traceKey ctxKey = "trace"

// traceInfo collects per-stage wall times for one request. Stages: the
// global middleware chain (Logger/cors/trackUsage), auth validation
// (requireRole), and the handler itself (store ops + JSON encode).
type traceInfo struct {
	start   time.Time
	chain   time.Duration // middleware above requireRole
	auth    time.Duration // token validation
	handler time.Duration // route handler (store ops + JSON encode)
}

func traceFrom(r *http.Request) *traceInfo {
	ti, _ := r.Context().Value(traceKey).(*traceInfo)
	return ti
}

// traceStage starts the trace and logs the stage breakdown after the
// response, when tracing is enabled.
func (sv *Server) traceStage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ti := &traceInfo{start: time.Now()}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceKey, ti)))
		if sv.trace {
			log.Printf("trace: %-6s %-52s chain=%-10s auth=%-10s handler=%-10s total=%s",
				r.Method, r.URL.RequestURI(),
				ti.chain.Round(10*time.Microsecond),
				ti.auth.Round(10*time.Microsecond),
				ti.handler.Round(10*time.Microsecond),
				time.Since(ti.start).Round(10*time.Microsecond))
		}
	})
}

func (sv *Server) requireViewer(next http.Handler) http.Handler {
	return sv.requireRole(auth.Viewer, auth.Editor, auth.Admin)(next)
}

// requireAny includes builder: roles that may see the contract (table
// names, columns, types, docs) but never a row of data.
func (sv *Server) requireAny(next http.Handler) http.Handler {
	return sv.requireRole(auth.Builder, auth.Viewer, auth.Editor, auth.Admin)(next)
}

func (sv *Server) requireEditor(next http.Handler) http.Handler {
	return sv.requireRole(auth.Editor, auth.Admin)(next)
}

func (sv *Server) requireAdmin(next http.Handler) http.Handler {
	return sv.requireRole(auth.Admin)(next)
}

func (sv *Server) requireRole(allowed ...auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ti := traceFrom(r)
			var authStart, handlerStart time.Time
			if ti != nil {
				authStart = time.Now()
				ti.chain = authStart.Sub(ti.start)
			}
			token := bearerToken(r)
			user, err := sv.validateAny(token)
			if ti != nil {
				ti.auth = time.Since(authStart)
			}
			if err != nil {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			for _, role := range allowed {
				if user.Role == role {
					if ti != nil {
						handlerStart = time.Now()
					}
					next.ServeHTTP(w, r.WithContext(contextWithUser(r, user)))
					if ti != nil {
						ti.handler = time.Since(handlerStart)
					}
					return
				}
			}
			writeError(w, http.StatusForbidden, "insufficient role")
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func currentUser(r *http.Request) auth.User {
	user, _ := r.Context().Value(userKey).(auth.User)
	return user
}

func contextWithUser(r *http.Request, u auth.User) context.Context {
	return context.WithValue(r.Context(), userKey, u)
}
func (sv *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	token, user, err := sv.auth.Login(body.Username, body.Password)
	if err != nil {
		sv.audit.add(auditEntry{At: nowRFC3339(), User: body.Username, Action: "login_failed", Values: map[string]string{"ip": clientIP(r)}})
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	sv.audit.add(auditEntry{At: nowRFC3339(), User: user.Username, Action: "login", Values: map[string]string{"ip": clientIP(r)}})
	writeJSON(w, http.StatusOK, map[string]string{
		"token":    token,
		"username": user.Username,
		"role":     string(user.Role),
	})
}

// me reports who the caller is (any role, session or API key).
func (sv *Server) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username, "role": string(u.Role)})
}

func (sv *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := sv.auth.Logout(bearerToken(r)); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// mapStoreErr writes the API grammar's status for a store error: 404
// missing, 409 existing name, 400 invalid input, 500 anything else (a
// storage failure is not the caller's fault, so the body is generic).
func mapStoreErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if storeErrStatus(err) == http.StatusInternalServerError {
		serverError(w, err)
		return true
	}
	writeError(w, storeErrStatus(err), err.Error())
	return true
}

// serverError logs the real cause for the operator and answers with a
// generic body, so internals (paths, engine errors) never reach clients.
func serverError(w http.ResponseWriter, err error) {
	log.Printf("500: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func storeErrStatus(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, store.ErrInvalid), errors.Is(err, store.ErrTableFull), errors.Is(err, store.ErrTooManyCols):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// auditEvent records an action by the signed-in user. Table and rowID are
// empty when they do not apply; login events are recorded separately,
// because their actor is the attempted name rather than a session.
func (sv *Server) auditEvent(r *http.Request, action, table, rowID string, values map[string]string) {
	sv.audit.add(auditEntry{At: nowRFC3339(), User: currentUser(r).Username, Action: action, Table: table, RowID: rowID, Values: values})
}

// rowValuesOnly strips the synthetic id key so audit payloads hold just
// the stored columns.
func rowValuesOnly(row map[string]string) map[string]string {
	out := make(map[string]string, len(row))
	for k, v := range row {
		if k != "id" {
			out[k] = v
		}
	}
	return out
}

// DiskFree reports available bytes on the filesystem holding dir. Zero
// means unknown (the UI hides the figure).
func DiskFree(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
