package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nori/internal/s3/s3test"
)

func newTestClient(t *testing.T, srv *s3test.Server) *Client {
	t.Helper()
	c, err := New(srv.URL, "auto", Credentials{AccessKeyID: "id", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClientObjectRoundTrip(t *testing.T) {
	// Arrange
	srv := s3test.New("b")
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()
	data := []byte("hello backup")

	// Act + Assert: put, head, get.
	if err := c.PutFile(ctx, "b", "prod/x y.json.gz", writeFile(t, data)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if size, err := c.Head(ctx, "b", "prod/x y.json.gz"); err != nil || size != int64(len(data)) {
		t.Fatalf("head: %d %v", size, err)
	}
	rc, err := c.Get(ctx, "b", "prod/x y.json.gz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("get = %q", got)
	}

	// Act + Assert: delete, then head and get say not found, and a second delete is fine.
	if err := c.Delete(ctx, "b", "prod/x y.json.gz"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Head(ctx, "b", "prod/x y.json.gz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("head after delete: %v", err)
	}
	if _, err := c.Get(ctx, "b", "prod/x y.json.gz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := c.Delete(ctx, "b", "prod/x y.json.gz"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestClientMultipart(t *testing.T) {
	// Arrange: 5 MB parts, an 11 MB file.
	srv := s3test.New("b")
	defer srv.Close()
	c := newTestClient(t, srv)
	c.PartSize = 5 << 20
	data := randomBytes(t, 11<<20)

	// Act
	err := c.PutFile(context.Background(), "b", "big", writeFile(t, data))

	// Assert: three parts joined in order.
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got, _ := srv.Object("b", "big"); !bytes.Equal(got, data) {
		t.Fatalf("object differs: %d bytes", len(got))
	}
	if srv.Requests("PUT") != 3 {
		t.Fatalf("PUT requests = %d, want 3 parts", srv.Requests("PUT"))
	}
}

func TestClientRetriesServerErrors(t *testing.T) {
	// Arrange
	srv := s3test.New("b")
	defer srv.Close()
	c := newTestClient(t, srv)
	data := []byte("retry me")

	// Act + Assert: two failures are retried away.
	srv.FailNextPuts(2)
	if err := c.PutFile(context.Background(), "b", "k", writeFile(t, data)); err != nil {
		t.Fatalf("put with two failures: %v", err)
	}

	// Act + Assert: three failures in a row fail the upload.
	srv.FailNextPuts(3)
	err := c.PutFile(context.Background(), "b", "k2", writeFile(t, data))
	if err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Fatalf("put with three failures: %v", err)
	}
	if _, ok := srv.Object("b", "k2"); ok {
		t.Fatalf("failed upload left an object")
	}
}

func TestClientList(t *testing.T) {
	// Arrange: three objects under a prefix, one outside it.
	srv := s3test.New("b")
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()
	for _, k := range []string{"p/a", "p/b", "p/c", "q/z"} {
		if err := c.PutFile(ctx, "b", k, writeFile(t, []byte(k))); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	objs, err := c.List(ctx, "b", "p/")

	// Assert
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var keys []string
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	if strings.Join(keys, ",") != "p/a,p/b,p/c" || objs[0].Size != 3 || objs[0].LastModified.IsZero() {
		t.Fatalf("list = %+v", objs)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	for _, tc := range []struct{ endpoint, region, id, secret string }{
		{"ftp://x", "auto", "a", "b"},
		{"https://x", "", "a", "b"},
		{"https://x", "auto", "", "b"},
		{"https://x", "auto", "a", ""},
	} {
		if _, err := New(tc.endpoint, tc.region, Credentials{tc.id, tc.secret}); err == nil {
			t.Errorf("New(%+v) accepted", tc)
		}
	}
}
