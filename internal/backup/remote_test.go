package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileRemote opens a file:// bucket under a temp dir; objects land in
// <dir>/prod/.
func fileRemote(t *testing.T, passphrase string) (*Remote, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := OpenRemote(context.Background(), "file://"+dir+"?prefix=prod/", passphrase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}

func fakeDump(t *testing.T, dir, stamp, content string) string {
	t.Helper()
	path := filepath.Join(dir, "nori-dump-"+stamp+".json.gz")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoteShipListFetch(t *testing.T) {
	// Arrange: two dumps shipped, and a file that is not a dump.
	r, dir := fileRemote(t, "")
	ctx := context.Background()
	local := t.TempDir()
	for _, stamp := range []string{"20261101-010000.000000001", "20261102-010000.000000001"} {
		name, err := r.Ship(ctx, fakeDump(t, local, stamp, "dump "+stamp))
		if err != nil || name != "nori-dump-"+stamp+".json.gz" {
			t.Fatalf("ship %s: %q %v", stamp, name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "prod", "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	dumps, err := r.List(ctx)

	// Assert: newest first, the other file left out.
	if err != nil || len(dumps) != 2 || dumps[0].Key != "nori-dump-20261102-010000.000000001.json.gz" || dumps[0].Size != 30 {
		t.Fatalf("list = %+v, %v", dumps, err)
	}

	// Act + Assert: latest and an older dump by name.
	for want, name := range map[string]string{
		"dump 20261102-010000.000000001": "latest",
		"dump 20261101-010000.000000001": "nori-dump-20261101-010000.000000001.json.gz",
	} {
		_, path, err := r.Fetch(ctx, name, t.TempDir())
		if b, _ := os.ReadFile(path); err != nil || string(b) != want {
			t.Fatalf("fetch %s = %q, %v", name, b, err)
		}
	}

	// Act + Assert: names that are not dumps are refused.
	for _, bad := range []string{"../etc/passwd", "notes.txt", ""} {
		if _, _, err := r.Fetch(ctx, bad, t.TempDir()); err == nil {
			t.Errorf("fetch %q accepted", bad)
		}
	}
}

func TestRemoteEncryptedRoundTrip(t *testing.T) {
	// Arrange
	r, dir := fileRemote(t, "correct horse")
	ctx := context.Background()
	content := "{\"tables\": \"all of them\"}"

	// Act
	name, err := r.Ship(ctx, fakeDump(t, t.TempDir(), "20261126-120000.000000001", content))
	if err != nil {
		t.Fatalf("ship: %v", err)
	}
	fetchedName, path, err := r.Fetch(ctx, "latest", t.TempDir())

	// Assert: stored encrypted, fetched plain.
	stored, _ := os.ReadFile(filepath.Join(dir, "prod", name))
	if !strings.HasSuffix(name, ".json.gz.age") || !bytes.HasPrefix(stored, []byte("age-encryption.org/v1")) || bytes.Contains(stored, []byte(content)) {
		t.Fatalf("stored object %s is not age-encrypted", name)
	}
	if b, _ := os.ReadFile(path); err != nil || fetchedName != name || string(b) != content || strings.HasSuffix(path, ".age") {
		t.Fatalf("fetch: %s %s %q %v", fetchedName, path, b, err)
	}

	// Act + Assert: a wrong or missing passphrase cannot read it.
	r.Passphrase = "wrong"
	if _, _, err := r.Fetch(ctx, "latest", t.TempDir()); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("fetch with wrong passphrase: %v", err)
	}
	r.Passphrase = ""
	if _, _, err := r.Fetch(ctx, "latest", t.TempDir()); err == nil || !strings.Contains(err.Error(), "NORI_BACKUP_PASSPHRASE") {
		t.Fatalf("fetch without passphrase: %v", err)
	}
}

func TestRemoteShipFailure(t *testing.T) {
	// Arrange: the bucket dir replaced by a regular file.
	r, dir := fileRemote(t, "")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := r.Ship(context.Background(), fakeDump(t, t.TempDir(), "20261126-120000.000000001", "x"))

	// Assert
	if err == nil {
		t.Fatal("ship succeeded into a bucket that cannot be written")
	}
}

func TestOpenRemote(t *testing.T) {
	ctx := context.Background()
	// Act + Assert: s3 URLs open without contacting the bucket.
	if r, err := OpenRemote(ctx, "s3://bkt?prefix=prod/&endpoint=https://acct.r2.cloudflarestorage.com&region=auto&use_path_style=true", ""); err != nil {
		t.Fatalf("s3 URL: %v", err)
	} else {
		r.Close()
	}
	// Act + Assert: other schemes, missing dirs and unknown options are refused.
	for _, bad := range []string{"bkt/prod", "gs://bkt", "file:///no/such/dir", "s3://bkt?nope=1"} {
		if _, err := OpenRemote(ctx, bad, ""); err == nil {
			t.Errorf("OpenRemote(%q) accepted", bad)
		}
	}
}
