package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nori/internal/s3"
	"nori/internal/s3/s3test"
)

func newRemote(t *testing.T, srv *s3test.Server, keep int, passphrase string) *Remote {
	t.Helper()
	c, err := s3.New(srv.URL, "auto", s3.Credentials{AccessKeyID: "id", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return &Remote{Client: c, Bucket: "b", Prefix: "prod", Keep: keep, Passphrase: passphrase}
}

func fakeDump(t *testing.T, dir, stamp, content string) string {
	t.Helper()
	path := filepath.Join(dir, "nori-dump-"+stamp+".json.gz")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoteShipPrunesOldest(t *testing.T) {
	// Arrange: keep 2, a key that is not a dump under the same prefix.
	srv := s3test.New()
	defer srv.Close()
	r := newRemote(t, srv, 2, "")
	ctx := context.Background()
	dir := t.TempDir()
	if err := r.Client.PutFile(ctx, "b", "prod/notes.txt", fakeDump(t, dir, "20260101-000000.000000000", "x")); err != nil {
		t.Fatal(err)
	}

	// Act: ship three dumps.
	var key string
	for _, stamp := range []string{"20261101-010000.000000001", "20261102-010000.000000001", "20261103-010000.000000001"} {
		var err error
		key, err = r.Ship(ctx, fakeDump(t, dir, stamp, "dump "+stamp))
		if err != nil {
			t.Fatalf("ship %s: %v", stamp, err)
		}
	}

	// Assert: the two newest stay, other keys are untouched.
	if key != "prod/nori-dump-20261103-010000.000000001.json.gz" {
		t.Fatalf("key = %s", key)
	}
	want := "prod/nori-dump-20261102-010000.000000001.json.gz,prod/nori-dump-20261103-010000.000000001.json.gz,prod/notes.txt"
	if got := strings.Join(srv.Keys("b"), ","); got != want {
		t.Fatalf("keys = %s", got)
	}
}

func TestRemoteShipFailureKeepsRemoteSet(t *testing.T) {
	// Arrange: two dumps already uploaded with keep 1 would prune on success.
	srv := s3test.New()
	defer srv.Close()
	r := newRemote(t, srv, 0, "")
	ctx := context.Background()
	dir := t.TempDir()
	for _, stamp := range []string{"20261101-010000.000000001", "20261102-010000.000000001"} {
		if _, err := r.Ship(ctx, fakeDump(t, dir, stamp, stamp)); err != nil {
			t.Fatal(err)
		}
	}
	r.Keep = 1
	srv.FailNextPuts(3)

	// Act
	_, err := r.Ship(ctx, fakeDump(t, dir, "20261103-010000.000000001", "new"))

	// Assert: the upload failed and nothing was pruned.
	if err == nil {
		t.Fatal("ship succeeded with every PUT failing")
	}
	if n := len(srv.Keys("b")); n != 2 {
		t.Fatalf("remote dumps after a failed upload: %d, want 2", n)
	}
}

func TestRemoteEncryptedRoundTrip(t *testing.T) {
	// Arrange
	srv := s3test.New()
	defer srv.Close()
	r := newRemote(t, srv, 3, "correct horse")
	ctx := context.Background()
	content := "{\"tables\": \"all of them\"}"
	path := fakeDump(t, t.TempDir(), "20261126-120000.000000001", content)

	// Act: ship, then fetch the latest into another dir.
	key, err := r.Ship(ctx, path)
	if err != nil {
		t.Fatalf("ship: %v", err)
	}
	got, err := r.Fetch(ctx, "latest", t.TempDir())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Assert: stored encrypted, fetched plain, no .age left next to the dump.
	stored, _ := srv.Object("b", key)
	if !strings.HasSuffix(key, ".json.gz.age") || bytes.Contains(stored, []byte(content)) || !bytes.HasPrefix(stored, []byte("age-encryption.org/v1")) {
		t.Fatalf("stored object is not age-encrypted: %s", key)
	}
	if _, err := os.Stat(path + ".age"); !os.IsNotExist(err) {
		t.Fatalf("encrypted temp file left behind: %v", err)
	}
	if b, _ := os.ReadFile(got); string(b) != content || strings.HasSuffix(got, ".age") {
		t.Fatalf("fetched %s = %q", got, b)
	}

	// Act + Assert: a wrong or missing passphrase cannot fetch it.
	r.Passphrase = "wrong"
	if _, err := r.Fetch(ctx, "latest", t.TempDir()); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("fetch with wrong passphrase: %v", err)
	}
	r.Passphrase = ""
	if _, err := r.Fetch(ctx, "latest", t.TempDir()); err == nil || !strings.Contains(err.Error(), "NORI_BACKUP_PASSPHRASE") {
		t.Fatalf("fetch without passphrase: %v", err)
	}
}

func TestRemoteFetchByName(t *testing.T) {
	// Arrange: two plain dumps.
	srv := s3test.New()
	defer srv.Close()
	r := newRemote(t, srv, 0, "")
	ctx := context.Background()
	dir := t.TempDir()
	for _, stamp := range []string{"20261101-010000.000000001", "20261102-010000.000000001"} {
		if _, err := r.Ship(ctx, fakeDump(t, dir, stamp, "dump "+stamp)); err != nil {
			t.Fatal(err)
		}
	}

	// Act + Assert: an older dump by name.
	got, err := r.Fetch(ctx, "nori-dump-20261101-010000.000000001.json.gz", t.TempDir())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if b, _ := os.ReadFile(got); string(b) != "dump 20261101-010000.000000001" {
		t.Fatalf("fetched %q", b)
	}

	// Act + Assert: names that are not dumps are refused before any request.
	for _, bad := range []string{"../etc/passwd", "notes.txt", ""} {
		if _, err := r.Fetch(ctx, bad, t.TempDir()); err == nil {
			t.Errorf("fetch %q accepted", bad)
		}
	}
}

func TestParseTarget(t *testing.T) {
	for in, want := range map[string]string{
		"s3://b":            "b|",
		"s3://b/":           "b|",
		"s3://b/prod":       "b|prod",
		"s3://b/prod/nori/": "b|prod/nori",
	} {
		bucket, prefix, err := ParseTarget(in)
		if err != nil || bucket+"|"+prefix != want {
			t.Errorf("ParseTarget(%q) = %q, %q, %v", in, bucket, prefix, err)
		}
	}
	for _, bad := range []string{"b/prod", "s3://", "s3:///prod", "https://b/prod"} {
		if _, _, err := ParseTarget(bad); err == nil {
			t.Errorf("ParseTarget(%q) accepted", bad)
		}
	}
}
