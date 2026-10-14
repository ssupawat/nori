//go:build unix

package main

// End-to-end tests over the real binary: boot it, use it over HTTP, kill
// it with SIGKILL, and restore a dump into a fresh dir. Unit tests cover
// the pieces; these cover the wiring (flags, embedded UI, real ports) and
// the crash paths only a real process can take.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nori/internal/sheetimport"
)

var e2eBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nori-e2e-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	e2eBin = filepath.Join(dir, "nori")
	build := exec.Command("go", "build", "-o", e2eBin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

const e2ePass = "e2epass123"

// freePort reserves an ephemeral port, then releases it for the server.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

type e2eServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	base   string
	token  string
	waited bool
}

func startE2E(t *testing.T, dataDir string, extraArgs ...string) *e2eServer {
	t.Helper()
	addr := freePort(t)
	args := []string{"-addr", addr, "-data", dataDir}
	args = append(args, extraArgs...)
	cmd := exec.Command(e2eBin, args...)
	cmd.Env = append(os.Environ(), "ADMIN_PASS="+e2ePass)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	s := &e2eServer{t: t, cmd: cmd, base: "http://" + addr}
	t.Cleanup(s.kill)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(s.base + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("server never became healthy")
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.token = s.login()
	return s
}

// kill terminates the process the hard way: SIGKILL, no cleanup, exactly
// the state a crash leaves.
func (s *e2eServer) kill() {
	if s.waited || s.cmd.Process == nil {
		return
	}
	s.waited = true
	_ = s.cmd.Process.Signal(syscall.SIGKILL)
	_, _ = s.cmd.Process.Wait()
}

func (s *e2eServer) login() string {
	s.t.Helper()
	// Bounded retry - a real credential bug still fails all three attempts.
	var last *http.Response
	var raw []byte
	for i := 0; i < 3; i++ {
		last, raw = s.post(s.t, "/api/login", fmt.Sprintf(`{"username":"admin","password":%q}`, e2ePass))
		if last.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if last.StatusCode != http.StatusOK {
		s.t.Fatalf("login: %d %s", last.StatusCode, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		s.t.Fatalf("login json: %v", err)
	}
	return out.Token
}

func (s *e2eServer) do(t *testing.T, method, path string, body io.Reader, contentType string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, s.base+path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	resp.Body.Close()
	return resp, raw
}

func (s *e2eServer) post(t *testing.T, path, jsonBody string) (*http.Response, []byte) {
	return s.do(t, "POST", path, strings.NewReader(jsonBody), "application/json")
}

// importXlsx builds a workbook with one sheet and imports it.
func (s *e2eServer) importXlsx(t *testing.T, sheet string, cols []string, rows [][]string, mode string) (*http.Response, []byte) {
	t.Helper()
	var wb bytes.Buffer
	if err := sheetimport.Write(&wb, sheet, cols, rows); err != nil {
		t.Fatalf("build workbook: %v", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "import.xlsx")
	if _, err := fw.Write(wb.Bytes()); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if err := mw.WriteField("mode", mode); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	mw.Close()
	return s.do(t, "POST", "/api/import", &body, mw.FormDataContentType())
}

// tryImportXlsx is importXlsx for goroutines off the test goroutine: it
// reports every failure through the error instead of t.Fatal.
func (s *e2eServer) tryImportXlsx(sheet string, cols []string, rows [][]string, mode string) error {
	var wb bytes.Buffer
	if err := sheetimport.Write(&wb, sheet, cols, rows); err != nil {
		return fmt.Errorf("build workbook: %w", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "import.xlsx")
	if _, err := fw.Write(wb.Bytes()); err != nil {
		return fmt.Errorf("multipart: %w", err)
	}
	if err := mw.WriteField("mode", mode); err != nil {
		return fmt.Errorf("multipart: %w", err)
	}
	mw.Close()
	req, err := http.NewRequest("POST", s.base+"/api/import", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err // expected when the server is SIGKILLed mid-request
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("import: %d %s", resp.StatusCode, raw)
	}
	return nil
}

// rows returns every row of a table; rows written before a column existed
// read with every column filled.
func (s *e2eServer) rows(t *testing.T, table string) []map[string]any {
	t.Helper()
	resp, raw := s.do(t, "GET", "/api/tables/"+table+"/rows?limit=10000", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rows of %s: %d %s", table, resp.StatusCode, raw)
	}
	var page struct {
		Rows  []map[string]any `json:"rows"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("rows json: %v (%s)", err, raw)
	}
	if page.Total != len(page.Rows) {
		t.Fatalf("expected all rows in one page: total %d, got %d", page.Total, len(page.Rows))
	}
	return page.Rows
}

func TestVersionFlag(t *testing.T) {
	// Act
	out, err := exec.Command(e2eBin, "-version").Output()
	if err != nil {
		t.Fatalf("-version: %v", err)
	}

	// Assert
	if got := strings.TrimSpace(string(out)); got == "" {
		t.Fatal("-version printed nothing")
	}
}

func TestKill9RowWritesSurvive(t *testing.T) {
	// Arrange: a running server with a table of ten rows, plus a check
	// that the embedded dashboard ships with the binary.
	dir := t.TempDir()
	s := startE2E(t, dir)

	resp, raw := s.do(t, "GET", "/", nil, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `id="app"`) {
		t.Fatalf("dashboard not served: %d", resp.StatusCode)
	}

	if resp, raw = s.post(t, "/api/tables", `{"name":"People","columns":[{"name":"name","type":"text"},{"name":"age","type":"number"}]}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", resp.StatusCode, raw)
	}
	for i := 1; i <= 10; i++ {
		body := fmt.Sprintf(`{"name":"person%d","age":"%d"}`, i, i*10)
		if resp, raw = s.post(t, "/api/tables/People/rows", body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("insert %d: %d %s", i, resp.StatusCode, raw)
		}
	}
	before := s.rows(t, "People")

	// Act: SIGKILL, then boot a fresh server over the same data dir.
	s.kill()
	s2 := startE2E(t, dir)

	// Assert: every row came back exactly as it was.
	after := s2.rows(t, "People")
	if len(after) != len(before) {
		t.Fatalf("rows lost across SIGKILL: before %d, after %d", len(before), len(after))
	}
	for i := range before {
		if before[i]["id"] != after[i]["id"] || before[i]["name"] != after[i]["name"] {
			t.Fatalf("row %d changed across SIGKILL: %v -> %v", i, before[i], after[i])
		}
	}
}

// A SIGKILL during a whole-table import must leave the table whole: the
// crash recovery either finishes the swap (new data) or abandons it (old
// data) - never a half table.
func TestKill9MidImportLeavesWholeTable(t *testing.T) {
	// Arrange: a server whose Big table holds three rows, and a 30k-row
	// workbook waiting to replace it.
	dir := t.TempDir()
	s := startE2E(t, dir)
	if resp, raw := s.importXlsx(t, "Big", []string{"n"}, [][]string{{"1"}, {"2"}, {"3"}}, "replace"); resp.StatusCode != http.StatusOK {
		t.Fatalf("seed import: %d %s", resp.StatusCode, raw)
	}

	big := make([][]string, 30000)
	for i := range big {
		big[i] = []string{fmt.Sprint(i)}
	}

	// Act: start the replace import and SIGKILL the server mid-flight.
	// The request runs on this goroutine without t.Fatal (Fatal is only
	// legal on the test goroutine and would strand the channel read below).
	importDone := make(chan error, 1)
	go func() {
		importDone <- s.tryImportXlsx("Big", []string{"n"}, big, "replace")
	}()
	time.Sleep(400 * time.Millisecond) // land mid-import: parse or swap
	s.kill()
	<-importDone // the in-flight request fails or completes; either is fine

	// Assert: recovery left the table whole - old data or new data.
	s2 := startE2E(t, dir)
	rows := s2.rows(t, "Big")
	if n := len(rows); n != 3 && n != 30000 {
		t.Fatalf("table left half-imported: %d rows", n)
	}
}

func TestRestoreEndToEnd(t *testing.T) {
	// Arrange: a running server with a table of three known rows and a
	// dump taken from it.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	s := startE2E(t, dataDir, "-backup-dir", backupDir)

	if resp, raw := s.post(t, "/api/tables", `{"name":"People","columns":[{"name":"name","type":"text"},{"name":"age","type":"number"}]}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create table: %d %s", resp.StatusCode, raw)
	}
	want := map[string]string{"amy": "30", "bob": "40", "cid": "50"}
	for name, age := range want {
		body := fmt.Sprintf(`{"name":%q,"age":%q}`, name, age)
		if resp, raw := s.post(t, "/api/tables/People/rows", body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("insert %s: %d %s", name, resp.StatusCode, raw)
		}
	}

	resp, raw := s.post(t, "/api/backup", ``)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup: %d %s", resp.StatusCode, raw)
	}
	var dump struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &dump); err != nil || dump.Path == "" {
		t.Fatalf("backup path: %v (%s)", err, raw)
	}
	s.kill()

	// Act: restore the dump into a fresh dir and boot a server on it.
	freshDir := t.TempDir()
	restore := exec.Command(e2eBin, "restore", "-data", freshDir, "-dump", dump.Path)
	restore.Stderr = os.Stderr
	if out, err := restore.Output(); err != nil {
		t.Fatalf("restore: %v (%s)", err, out)
	}
	s2 := startE2E(t, freshDir)

	// Assert: the restored rows match, and the audit trail starts empty
	// (dumps skip the audit trail).
	got := s2.rows(t, "People")
	if len(got) != len(want) {
		t.Fatalf("restored %d rows, want %d", len(got), len(want))
	}
	for _, r := range got {
		name, _ := r["name"].(string)
		age, _ := r["age"].(string)
		if want[name] != age {
			t.Fatalf("restored row mismatch: %v (want age %s)", r, want[name])
		}
	}

	resp, raw = s2.do(t, "GET", "/api/audit", nil, "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("restored audit trail should be empty: %d %s", resp.StatusCode, raw)
	}
}
