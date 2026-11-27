//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRemoteBackupEndToEnd(t *testing.T) {
	// Arrange: a file:// bucket, a passphrase, and a server with only
	// -backup-remote (dumps staged).
	bucketDir := t.TempDir()
	target := "file://" + bucketDir + "?prefix=prod/"
	stored := filepath.Join(bucketDir, "prod")
	t.Setenv("NORI_BACKUP_PASSPHRASE", "correct horse")
	dataDir := filepath.Join(t.TempDir(), "nori")
	staging := dataDir + ".backup-staging"
	s := startE2E(t, dataDir, "-backup-remote", target)

	if resp, raw := s.post(t, "/api/tables", `{"name":"People","columns":[{"name":"name","type":"text"},{"name":"age","type":"number"}]}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", resp.StatusCode, raw)
	}
	want := map[string]string{"amy": "30", "bob": "40"}
	for name, age := range want {
		if resp, raw := s.post(t, "/api/tables/People/rows", fmt.Sprintf(`{"name":%q,"age":%q}`, name, age)); resp.StatusCode != http.StatusCreated {
			t.Fatalf("insert %s: %d %s", name, resp.StatusCode, raw)
		}
	}
	glob := func(pattern string) []string {
		files, _ := filepath.Glob(pattern)
		return files
	}
	type remoteStatus struct {
		Target, LastKey, LastError string
		Encrypted                  bool
	}
	status := func() remoteStatus {
		_, raw := s.do(t, "GET", "/api/status", nil, "")
		var st struct {
			Backup struct {
				Remote remoteStatus `json:"remote"`
			} `json:"backup"`
		}
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatalf("status: %v (%s)", err, raw)
		}
		return st.Backup.Remote
	}

	// Act + Assert: with the bucket dir replaced by a file, the upload fails
	// with a 500, keeps the staged dump and shows in status.
	if err := os.RemoveAll(bucketDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bucketDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if resp, raw := s.post(t, "/api/backup", ``); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("backup into a broken bucket: %d %s", resp.StatusCode, raw)
	}
	if len(glob(filepath.Join(staging, "nori-dump-*"))) != 1 {
		t.Fatalf("staged dumps after a failed upload: %v", glob(filepath.Join(staging, "*")))
	}
	if st := status(); st.LastError == "" || st.Target != target || !st.Encrypted {
		t.Fatalf("status after a failed upload: %+v", st)
	}

	// Act + Assert: once the bucket is back, the next backup uploads
	// encrypted and clears staging.
	if err := os.Remove(bucketDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bucketDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resp, raw := s.post(t, "/api/backup", ``)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup: %d %s", resp.StatusCode, raw)
	}
	var res struct {
		RemoteKey string `json:"remoteKey"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || !strings.HasPrefix(res.RemoteKey, "nori-dump-") || !strings.HasSuffix(res.RemoteKey, ".json.gz.age") {
		t.Fatalf("backup result: %s", raw)
	}
	if b, _ := os.ReadFile(filepath.Join(stored, res.RemoteKey)); !strings.HasPrefix(string(b), "age-encryption.org/v1") {
		t.Fatalf("stored dump is not age-encrypted")
	}
	if left := glob(filepath.Join(staging, "nori-dump-*")); len(left) != 0 {
		t.Fatalf("staging not cleared after an upload: %v", left)
	}
	if st := status(); st.LastError != "" || st.LastKey != res.RemoteKey {
		t.Fatalf("status after an upload: %+v", st)
	}

	// Act + Assert: a graceful stop uploads one more dump.
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	s.waited = true
	if err := s.cmd.Wait(); err != nil {
		t.Fatalf("server exit: %v", err)
	}
	if dumps := glob(filepath.Join(stored, "nori-dump-*.age")); len(dumps) != 2 {
		t.Fatalf("remote dumps after shutdown: %v", dumps)
	}

	// Act + Assert: nori backups lists both, newest first.
	out, err := exec.Command(e2eBin, "backups", "-remote", target).Output()
	if err != nil {
		t.Fatalf("backups: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[1], res.RemoteKey) {
		t.Fatalf("backups output:\n%s", out)
	}

	// Act: restore the latest remote dump into a fresh dir and boot on it.
	freshDir := t.TempDir()
	restore := exec.Command(e2eBin, "restore", "-data", freshDir, "-remote", target, "-dump", "latest")
	restore.Stderr = os.Stderr
	if out, err := restore.Output(); err != nil {
		t.Fatalf("restore: %v (%s)", err, out)
	}
	s2 := startE2E(t, freshDir)

	// Assert: the rows came back.
	assertRows := func(srv *e2eServer) {
		t.Helper()
		got := srv.rows(t, "People")
		if len(got) != len(want) {
			t.Fatalf("restored %d rows, want %d", len(got), len(want))
		}
		for _, r := range got {
			if want[r["name"].(string)] != r["age"].(string) {
				t.Fatalf("restored row mismatch: %v", r)
			}
		}
	}
	assertRows(s2)
	s2.kill()

	// Act: a local .age file restores even with an unreachable remote in
	// the environment, so the remote is never touched.
	t.Setenv("NORI_BACKUP_REMOTE", "file:///no/such/bucket")
	local := filepath.Join(stored, res.RemoteKey)
	localDir := t.TempDir()
	restore = exec.Command(e2eBin, "restore", "-data", localDir, "-dump", local)
	restore.Stderr = os.Stderr
	out, err = restore.Output()
	if err != nil || !strings.Contains(string(out), "restored from "+local) {
		t.Fatalf("local restore: %v (%s)", err, out)
	}
	t.Setenv("NORI_BACKUP_REMOTE", "")

	// Assert
	assertRows(startE2E(t, localDir))
}

func TestRemoteBackupConfigErrors(t *testing.T) {
	for name, target := range map[string]string{
		"no scheme":         "bkt/prod",
		"unsupported":       "gs://bkt",
		"missing directory": "file:///no/such/bucket",
		"unknown option":    "s3://bkt?nope=1",
	} {
		t.Run(name, func(t *testing.T) {
			// Act
			cmd := exec.Command(e2eBin, "-addr", freePort(t), "-data", t.TempDir(), "-backup-remote", target)
			cmd.Env = append(os.Environ(), "ADMIN_PASS="+e2ePass)
			out, err := cmd.CombinedOutput()

			// Assert: nori refuses to start and says why.
			if err == nil || !strings.Contains(string(out), "remote backups:") {
				t.Fatalf("started anyway or no reason given: %v\n%s", err, out)
			}
		})
	}
}
