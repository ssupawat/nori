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

	"nori/internal/s3/s3test"
)

func TestRemoteBackupEndToEnd(t *testing.T) {
	// Arrange: a fake bucket, credentials and a passphrase in the
	// environment, and a server with only -backup-remote (dumps staged).
	fake := s3test.New("bkt")
	defer fake.Close()
	t.Setenv("NORI_BACKUP_ACCESS_KEY_ID", "test-id")
	t.Setenv("NORI_BACKUP_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("NORI_BACKUP_PASSPHRASE", "correct horse")
	remoteArgs := []string{"-backup-remote", "s3://bkt/prod", "-backup-endpoint", fake.URL}
	dataDir := filepath.Join(t.TempDir(), "nori")
	staging := dataDir + ".backup-staging"
	s := startE2E(t, dataDir, remoteArgs...)

	if resp, raw := s.post(t, "/api/tables", `{"name":"People","columns":[{"name":"name","type":"text"},{"name":"age","type":"number"}]}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", resp.StatusCode, raw)
	}
	want := map[string]string{"amy": "30", "bob": "40"}
	for name, age := range want {
		if resp, raw := s.post(t, "/api/tables/People/rows", fmt.Sprintf(`{"name":%q,"age":%q}`, name, age)); resp.StatusCode != http.StatusCreated {
			t.Fatalf("insert %s: %d %s", name, resp.StatusCode, raw)
		}
	}
	staged := func() []string {
		files, _ := filepath.Glob(filepath.Join(staging, "nori-dump-*"))
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

	// Act + Assert: a failed upload is a 500, keeps the staged dump, shows in status.
	fake.FailNextPuts(3)
	if resp, raw := s.post(t, "/api/backup", ``); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("backup with a failing bucket: %d %s", resp.StatusCode, raw)
	}
	if len(staged()) != 1 || len(fake.Keys("bkt")) != 0 {
		t.Fatalf("after a failed upload: staged %v, remote %v", staged(), fake.Keys("bkt"))
	}
	if st := status(); st.LastError == "" || st.Target != "s3://bkt/prod" || !st.Encrypted {
		t.Fatalf("status after a failed upload: %+v", st)
	}

	// Act + Assert: the next backup uploads encrypted and clears staging.
	resp, raw := s.post(t, "/api/backup", ``)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup: %d %s", resp.StatusCode, raw)
	}
	var res struct {
		RemoteKey string `json:"remoteKey"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || !strings.HasPrefix(res.RemoteKey, "prod/nori-dump-") || !strings.HasSuffix(res.RemoteKey, ".json.gz.age") {
		t.Fatalf("backup result: %s", raw)
	}
	if len(staged()) != 0 {
		t.Fatalf("staging not cleared after an upload: %v", staged())
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
	if keys := fake.Keys("bkt"); len(keys) != 2 {
		t.Fatalf("remote dumps after shutdown: %v", keys)
	}

	// Act + Assert: nori backups lists both, newest first.
	out, err := exec.Command(e2eBin, append([]string{"backups"}, "-remote", "s3://bkt/prod", "-endpoint", fake.URL)...).Output()
	if err != nil {
		t.Fatalf("backups: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[1], strings.TrimPrefix(res.RemoteKey, "prod/")) {
		t.Fatalf("backups output:\n%s", out)
	}

	// Act: restore the latest remote dump into a fresh dir and boot on it.
	freshDir := t.TempDir()
	restore := exec.Command(e2eBin, "restore", "-data", freshDir, "-remote", "s3://bkt/prod", "-endpoint", fake.URL, "-dump", "latest")
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

	// Act: a local .age file restores even with a remote configured in
	// the environment.
	stored, _ := fake.Object("bkt", res.RemoteKey)
	local := filepath.Join(t.TempDir(), filepath.Base(res.RemoteKey))
	if err := os.WriteFile(local, stored, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NORI_BACKUP_REMOTE", "s3://bkt/prod")
	t.Setenv("NORI_BACKUP_ENDPOINT", fake.URL)
	gets := fake.Requests("GET")
	localDir := t.TempDir()
	restore = exec.Command(e2eBin, "restore", "-data", localDir, "-dump", local)
	restore.Stderr = os.Stderr
	out, err = restore.Output()
	if err != nil || !strings.Contains(string(out), "restored from "+local) {
		t.Fatalf("local restore: %v (%s)", err, out)
	}
	if fake.Requests("GET") != gets {
		t.Fatalf("a local restore fetched from the bucket")
	}

	// Assert
	assertRows(startE2E(t, localDir))
}

func TestRemoteBackupConfigErrors(t *testing.T) {
	// Arrange: no credentials in the environment.
	t.Setenv("NORI_BACKUP_ACCESS_KEY_ID", "")
	t.Setenv("NORI_BACKUP_SECRET_ACCESS_KEY", "")
	for name, args := range map[string][]string{
		"bad target":     {"-backup-remote", "bkt/prod", "-backup-endpoint", "https://x"},
		"no endpoint":    {"-backup-remote", "s3://bkt/prod"},
		"no credentials": {"-backup-remote", "s3://bkt/prod", "-backup-endpoint", "https://x"},
	} {
		t.Run(name, func(t *testing.T) {
			// Act
			cmd := exec.Command(e2eBin, append([]string{"-addr", freePort(t), "-data", t.TempDir()}, args...)...)
			cmd.Env = append(os.Environ(), "ADMIN_PASS="+e2ePass)
			out, err := cmd.CombinedOutput()

			// Assert: nori refuses to start and says why.
			if err == nil || !strings.Contains(string(out), "remote backups:") {
				t.Fatalf("started anyway or no reason given: %v\n%s", err, out)
			}
		})
	}
}
