package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	clover "github.com/ostafen/clover/v2"
	"nori/internal/auth"
	"nori/internal/store"
)

// clientIP keys the login rate limiter. The visitor identity must come
// from the platform the connection provably traveled through, never from
// a header an off-platform client could have written.

func clientIPTestServer(t *testing.T, trustProxy bool) http.Handler {
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
	sv := NewServer(s, a).WithTrustProxy(trustProxy)
	root := chi.NewRouter()
	root.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(clientIP(r)))
	})
	root.Mount("/api", sv.Routes())
	return root
}

func getWithRemote(t *testing.T, h http.Handler, path, remote string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res.Result()
}

// On Fly (FLY_APP_NAME set, connection from Fly's internal proxy), the
// visitor is Fly-Client-IP.
func TestClientIPTargetsFlyClientIP(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "nori")
	old := trustProxy
	trustProxy = false
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, false)
	res := getWithRemote(t, h, "/whoami", "[fdaa:0:1::12]:443", map[string]string{
		"Fly-Client-IP": "203.0.113.7",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "203.0.113.7" {
		t.Fatalf("got %q, want Fly-Client-IP", got)
	}
}

// Off Fly, a client-supplied Fly-Client-IP is garbage and must be
// ignored: the connection is not from Fly's proxy, so the header proves
// nothing.
func TestClientIPIgnoresFlyHeaderOffFly(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "")
	old := trustProxy
	trustProxy = false
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, false)
	res := getWithRemote(t, h, "/whoami", "198.51.100.9:5555", map[string]string{
		"Fly-Client-IP": "203.0.113.7",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "198.51.100.9" {
		t.Fatalf("got %q, want RemoteAddr", got)
	}
}

// On Fly, a connection NOT from Fly's internal proxy cannot exist in
// production, but the code must not trust the header from one anyway.
func TestClientIPIgnoresFlyHeaderFromForeignSource(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "nori")
	old := trustProxy
	trustProxy = false
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, false)
	res := getWithRemote(t, h, "/whoami", "198.51.100.9:5555", map[string]string{
		"Fly-Client-IP": "203.0.113.7",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "198.51.100.9" {
		t.Fatalf("got %q, want RemoteAddr", got)
	}
}

// Behind Cloudflare on Fly (Fly-Client-IP is a CF edge address), the
// visitor is the origin-facing client IP that CF vouches for.
func TestClientIPTargetsCFConnectingIPBehindCF(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "nori")
	old := trustProxy
	trustProxy = false
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, false)
	res := getWithRemote(t, h, "/whoami", "[fdaa:0:1::12]:443", map[string]string{
		"Fly-Client-IP":    "108.162.200.1", // a published Cloudflare range
		"CF-Connecting-IP": "203.0.113.42",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "203.0.113.42" {
		t.Fatalf("got %q, want CF-Connecting-IP", got)
	}
}

// CF-Connecting-IP from a source that is not a Cloudflare edge is a
// forged header; fall back to the connection's own visitor.
func TestClientIIgnoresCFHeaderWithoutCFSource(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "nori")
	old := trustProxy
	trustProxy = false
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, false)
	res := getWithRemote(t, h, "/whoami", "[fdaa:0:1::12]:443", map[string]string{
		"Fly-Client-IP":    "203.0.113.7", // a plain visitor, not CF
		"CF-Connecting-IP": "203.0.113.42",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "203.0.113.7" {
		t.Fatalf("got %q, want Fly-Client-IP", got)
	}
}

// The classic case keeps working: behind the user's own proxy with
// -trust-proxy, the visitor is the first X-Forwarded-For entry.
func TestClientIPUsesXFFWhenTrustingProxy(t *testing.T) {
	t.Setenv("FLY_APP_NAME", "")
	old := trustProxy
	trustProxy = true
	t.Cleanup(func() { trustProxy = old })

	h := clientIPTestServer(t, true)
	res := getWithRemote(t, h, "/whoami", "10.0.0.5:6666", map[string]string{
		"X-Forwarded-For": "203.0.113.99",
	})
	if got := strings.TrimSpace(readBody(t, res)); got != "203.0.113.99" {
		t.Fatalf("got %q, want X-Forwarded-For", got)
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	buf := make([]byte, 128)
	n, _ := res.Body.Read(buf)
	res.Body.Close()
	return string(buf[:n])
}
