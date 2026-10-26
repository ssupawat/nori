package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"

	"nori/internal/auth"
)

// Login rate limiting is per client IP (X-Forwarded-For first), so
// deployments behind a reverse proxy get per-visitor windows, not one
// shared window for the proxy.
var maxLoginsPerMinute = 10

// trustProxy: trust X-Forwarded-For for client IP detection. Off by default
// so a directly-exposed server cannot have its per-IP rate limits bypassed
// by header spoofing; turn on only behind a reverse proxy.
var trustProxy = false

func loginRateLimit(next http.Handler) http.Handler {
	return httprate.LimitBy(
		maxLoginsPerMinute,
		time.Minute,
		func(r *http.Request) (string, error) { return clientIP(r), nil },
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusTooManyRequests, "too many login attempts, try again in a minute")
		}),
	)(next)
}

func clientIP(r *http.Request) string {
	if ip := platformIP(r); ip != "" {
		return ip
	}
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

// platformIP returns the visitor IP vouched for by the platform proxy,
// when the connection provably came from it. On Fly, the edge sets
// Fly-Client-IP; when that address is one of Cloudflare's, the request
// transited Cloudflare and CF-Connecting-IP is the real visitor.
func platformIP(r *http.Request) string {
	if os.Getenv("FLY_APP_NAME") == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !flyInternal(host) {
		return ""
	}
	fly := strings.TrimSpace(r.Header.Get("Fly-Client-IP"))
	if fly == "" {
		return ""
	}
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" && isCloudflareIP(fly) {
		return cf
	}
	return fly
}

// flyInternal reports whether addr is one of Fly's internal proxy
// addresses (ULA fd00::/8), i.e. the connection provably arrived
// through Fly's edge.
func flyInternal(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && ip.To4() == nil && ip.IsPrivate()
}

func (sv *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	full, key, err := sv.auth.CreateAPIKey(body.Name, auth.Role(body.Role))
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrKeyExists):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		serverError(w, err)
		return
	}
	// the only response that ever carries the full key
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     key.ID,
		"name":   key.Name,
		"prefix": key.Prefix,
		"role":   key.Role,
		"key":    full,
	})
}

func (sv *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := sv.auth.ListAPIKeys()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (sv *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := sv.auth.DeleteAPIKey(id); err != nil {
		if errors.Is(err, auth.ErrKeyNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
