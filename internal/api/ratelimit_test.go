package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	clover "github.com/ostafen/clover/v2"
	"nori/internal/auth"
	"nori/internal/store"
)

// The generic rate limiter is the egress fuse: it caps request volume
// per visitor across the API, so a flood cannot turn small responses
// into a large bill. Keyed per visitor (the same identity the login
// limiter uses), health checks exempt, off unless configured.

func newRateLimitServer(t *testing.T, perMinute int) http.Handler {
	t.Helper()
	db, err := clover.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	sv := NewServer(s, a).WithRateLimit(perMinute)
	root := chi.NewRouter()
	root.Mount("/api", sv.Routes())
	return root
}

func flood(t *testing.T, h http.Handler, n int, remote string) []int {
	t.Helper()
	codes := make([]int, 0, n)
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/tables", nil)
		req.RemoteAddr = remote
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		codes = append(codes, res.Code)
	}
	return codes
}

// A visitor exceeding the limit gets 429 with an empty body: the egress
// fuse stops serving before it starts billing bytes.
func TestRateLimitBlocksFlood(t *testing.T) {
	h := newRateLimitServer(t, 3)
	codes := flood(t, h, 6, "192.0.2.10:1000")
	passed, limited := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusUnauthorized:
			passed++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	if passed != 3 || limited != 3 {
		t.Fatalf("limit 3: got %d passed / %d limited, want 3/3 (codes: %v)", passed, limited, codes)
	}
}

// Each visitor has their own bucket: a flood from one IP does not touch
// another visitor's allowance.
func TestRateLimitKeysPerVisitor(t *testing.T) {
	h := newRateLimitServer(t, 3)
	flood(t, h, 6, "192.0.2.10:1000") // exhaust visitor A
	req := httptest.NewRequest(http.MethodGet, "/api/tables", nil)
	req.RemoteAddr = "192.0.2.11:1000" // visitor B, fresh bucket
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code == http.StatusTooManyRequests {
		t.Fatal("visitor B must not inherit visitor A's flood")
	}
}

// Health checks are exempt: Fly probes every 30s per machine and must
// never trip the fuse, or machines flap.
func TestRateLimitHealthExempt(t *testing.T) {
	h := newRateLimitServer(t, 3)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.RemoteAddr = "192.0.2.10:1000"
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("health check %d: got %d, want 200", i+1, res.Code)
		}
	}
}

// Unconfigured server: unlimited, exactly as before this feature.
func TestRateLimitDisabledByDefault(t *testing.T) {
	db, err := clover.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	sv := NewServer(s, a) // no WithRateLimit
	root := chi.NewRouter()
	root.Mount("/api", sv.Routes())

	codes := flood(t, root, 20, "192.0.2.10:1000")
	for _, c := range codes {
		if c == http.StatusTooManyRequests {
			t.Fatal("default build must not rate limit")
		}
	}
}
