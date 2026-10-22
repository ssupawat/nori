package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	_ "net/http/pprof" // registers debug handlers on DefaultServeMux (-pprof)
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"nori/internal/api"
	"nori/internal/auth"
	"nori/internal/backup"
	"nori/internal/store"
)

// The UI build output (npm run build in ui/). Only ui/dist/.gitkeep is
// tracked; without a UI build the binary serves a notice instead.
//
//go:embed all:ui/dist
var webFiles embed.FS

// version is injected at build time (goreleaser): -X main.version=v1.0.0
var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./data", "data directory")
	adminUser := flag.String("admin-user", envOr("ADMIN_USER", "admin"), "bootstrap admin username (first run)")
	adminPass := flag.String("admin-pass", os.Getenv("ADMIN_PASS"), "bootstrap admin password (first run)")
	brandName := flag.String("brand-name", envOr("BRAND_NAME", ""), "white-label: brand name shown in the UI")
	brandTagline := flag.String("brand-tagline", envOr("BRAND_TAGLINE", ""), "white-label: tagline on the login page")
	brandColor := flag.String("brand-color", envOr("BRAND_COLOR", ""), "white-label: accent color (hex, e.g. #2563eb)")
	brandLogo := flag.String("brand-logo-url", envOr("BRAND_LOGO_URL", ""), "white-label: logo image URL (empty = generated art mark)")
	pprofAddr := flag.String("pprof", "", "serve pprof debug endpoints on this address (e.g. 127.0.0.1:6060)")
	trace := flag.Bool("trace", false, "log per-stage API timings (chain/auth/handler/total)")
	trustProxy := flag.Bool("trust-proxy", false, "trust X-Forwarded-For for client IPs (enable behind a reverse proxy)")
	backupDir := flag.String("backup-dir", "", "directory for scheduled JSON dumps (empty = disabled)")
	backupEvery := flag.Duration("backup-every", 24*time.Hour, "time between scheduled dumps")
	backupKeep := flag.Int("backup-keep", 7, "number of scheduled dumps to keep")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// subcommands inherit the global -data value, so both
	// "nori -data X check" and "nori check -data X" work
	if len(flag.Args()) > 0 {
		sub := flag.Args()[0]
		rest := flag.Args()[1:]
		switch sub {
		case "restore":
			os.Exit(runRestoreCmd(rest, *dataDir))
		case "check":
			os.Exit(runCheckCmd(rest, *dataDir))
		case "compact":
			os.Exit(runCompactCmd(rest, *dataDir))
		}
	}

	if *pprofAddr != "" {
		go func() {
			log.Printf("pprof on http://%s/debug/pprof", *pprofAddr)
			log.Fatal(http.ListenAndServe(*pprofAddr, nil))
		}()
	}

	s, code := openStore(*dataDir)
	if s == nil {
		os.Exit(code)
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	startedAt := time.Now()
	// last scheduled/shutdown dump outcome, surfaced by GET /api/status
	var backupMu sync.Mutex
	var lastBackupAt time.Time
	var lastBackupPath, lastBackupErr string
	recordDump := func(path string, err error) {
		backupMu.Lock()
		lastBackupAt, lastBackupPath, lastBackupErr = time.Now(), path, ""
		if err != nil {
			lastBackupErr = err.Error()
		}
		backupMu.Unlock()
	}

	// scheduled logical dumps (safe while serving traffic): every interval,
	// and once more on shutdown
	if *backupDir != "" {
		// seed "last dump" from the newest file already in the dir, so
		// /api/status is accurate right after a restart
		if entries, err := os.ReadDir(*backupDir); err == nil {
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !strings.HasPrefix(name, "nori-dump-") || !strings.HasSuffix(name, ".json.gz") {
					continue
				}
				if info, err := e.Info(); err == nil && info.ModTime().After(lastBackupAt) {
					lastBackupAt, lastBackupPath = info.ModTime(), filepath.Join(*backupDir, name)
				}
			}
		}
		dump := func(when string) {
			path, err := dumpJSON(s, *backupDir, *backupKeep)
			recordDump(path, err)
			if err != nil {
				log.Printf("scheduled backup failed (%s): %v", when, err)
				return
			}
			log.Printf("backup written (%s): %s", when, path)
		}
		go func() {
			ticker := time.NewTicker(*backupEvery)
			defer ticker.Stop()
			for range ticker.C {
				dump("interval")
			}
		}()

	}

	a, err := auth.Open(s.DB())
	if err != nil {
		log.Fatalf("open auth: %v", err)
	}

	// the admin password is required only when there is no admin yet: a
	// generated one printed to stdout is useless the moment the terminal
	// scrolls (containers, services) and leaves an account nobody can sign into
	password := *adminPass
	hasAdmin, err := a.HasAdmin()
	if err != nil {
		log.Fatalf("check admin: %v", err)
	}
	if !hasAdmin && password == "" {
		fmt.Fprintln(os.Stderr, "nori: ADMIN_PASS (or -admin-pass) is required on first run: set it to bootstrap the admin account")
		os.Exit(2)
	}
	if password != "" {
		created, err := a.EnsureAdmin(*adminUser, password)
		if err != nil {
			log.Fatalf("bootstrap admin: %v", err)
		}
		if created {
			log.Printf("created admin user %q", *adminUser)
		}
	}

	webFS, err := fs.Sub(webFiles, "ui/dist")
	if err != nil {
		log.Fatalf("embed web: %v", err)
	}

	r := chi.NewRouter()
	r.Use(api.SecurityHeaders)
	apiSrv := api.NewServer(s, a).WithBranding(api.Branding{
		Name:    *brandName,
		Tagline: *brandTagline,
		Color:   *brandColor,
		LogoURL: *brandLogo,
	}).WithTrace(*trace).WithTrustProxy(*trustProxy).WithStatus(func() *api.Status {
		st := &api.Status{
			Version:       version,
			UptimeSeconds: int64(time.Since(startedAt).Seconds()),
			DataDir:       *dataDir,
			DataBytes:     api.DirSize(*dataDir),
			DiskFreeBytes: api.DiskFree(*dataDir),
		}
		if *backupDir != "" {
			backupMu.Lock()
			st.Backup = &api.BackupStatus{
				Dir:          *backupDir,
				EverySeconds: int64((*backupEvery).Seconds()),
				LastAt:       api.SinceRFC3339(lastBackupAt),
				LastPath:     lastBackupPath,
				LastError:    lastBackupErr,
			}
			backupMu.Unlock()
		}
		return st
	}).WithBackupNow(func() (string, error) {
		// serialized with the scheduled dumps through backupMu, so two
		// triggers never write (or prune) at the same time
		backupMu.Lock()
		defer backupMu.Unlock()
		if *backupDir == "" {
			return "", errors.New("backups are disabled - start with -backup-dir")
		}
		path, err := dumpJSON(s, *backupDir, *backupKeep)
		lastBackupAt, lastBackupPath, lastBackupErr = time.Now(), path, ""
		if err != nil {
			lastBackupErr = err.Error()
		}
		return path, err
	})
	r.Get("/llms.txt", apiSrv.LLMsTxt)
	r.Get("/openapi.json", apiSrv.OpenAPISpec)
	r.Mount("/api", apiSrv.Routes())
	r.Handle("/*", spaHandler(webFS))

	srv := &http.Server{Addr: *addr, Handler: r}
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		<-sigs
		log.Println("shutting down (in-flight requests draining)...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Printf("nori listening on %s (data: %s)", *addr, *dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	// drained: in-flight requests are done, now it is safe to dump
	if *backupDir != "" {
		path, err := dumpJSON(s, *backupDir, *backupKeep)
		recordDump(path, err)
		if err == nil {
			log.Printf("backup written (shutdown): %s", path)
		}
	}
}

func spaHandler(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "This nori binary was built without the dashboard.")
			fmt.Fprintln(w, "Build it with: (cd ui && npm ci && npm run build) && go build .")
			fmt.Fprintln(w, "The API under /api works as usual.")
		})
	}
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// runRestoreCmd implements `nori restore`: rebuild a data dir from a
// scheduled dump. It only writes into an empty data dir, so the intended
// flow is: stop the server, move the old data dir away, restore, start.
func runRestoreCmd(args []string, inheritData string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	dataDir := fs.String("data", inheritData, "data directory to build")
	dumpPath := fs.String("dump", "", "dump file to restore from (nori-dump-*.json.gz)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dumpPath == "" {
		fmt.Fprintln(os.Stderr, "usage: nori restore -data ./data -dump ./backups/nori-dump-....json.gz")
		return 2
	}
	if _, err := os.Stat(*dumpPath); err != nil {
		fmt.Fprintf(os.Stderr, "dump not found: %s\n", *dumpPath)
		return 1
	}
	s, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()
	fresh, err := backup.IsFresh(s.DB())
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect data dir: %v\n", err)
		return 1
	}
	if !fresh {
		fmt.Fprintf(os.Stderr, "data dir %s is not empty; move it away first (restore never mixes with live data)\n", *dataDir)
		return 1
	}
	counts, err := backup.RestoreJSON(s.DB(), *dumpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		return 1
	}
	names := make([]string, 0, len(counts))
	for name, n := range counts {
		names = append(names, fmt.Sprintf("%s (%d docs)", name, n))
	}
	fmt.Printf("restored from %s:\n  %s\n", *dumpPath, strings.Join(names, "\n  "))
	return 0
}

// openStore opens a data dir for the server and the subcommands. A dir
// another nori holds locked is reported with its own message.
func openStore(dataDir string) (*store.Store, int) {
	if err := probeDataLock(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "data dir %s is already in use by another nori instance\n", dataDir)
		return nil, 1
	}
	s, err := store.Open(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		return nil, 1
	}
	return s, 0
}

// runCheckCmd implements nori check: a read-only scan of the whole data
// dir that reports anything which would stop nori from reading its own
// data. Exit 1 when problems were found.
func runCheckCmd(args []string, inheritData string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	dataDir := fs.String("data", inheritData, "data directory to check")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, code := openStore(*dataDir)
	if s == nil {
		return code
	}
	defer s.Close()

	rep := s.Check()
	if rep.SchemaVersion != "" {
		fmt.Printf("  schema version %s\n", rep.SchemaVersion)
	}
	for _, ct := range rep.Tables {
		status := "ok"
		if len(ct.Problems) > 0 {
			status = strings.Join(ct.Problems, "; ")
		}
		fmt.Printf("  %-20s %5d rows  %s\n", ct.Name, ct.Rows, status)
	}
	for _, ct := range rep.System {
		fmt.Printf("  %-20s %5d docs  (system)\n", ct.Name, ct.Rows)
	}
	for _, n := range rep.Notes {
		fmt.Println("note:", n)
	}
	for _, p := range rep.Problems {
		fmt.Println("PROBLEM:", p)
	}
	fmt.Printf("total %d docs - %s\n", rep.TotalDocs, map[bool]string{true: "OK", false: "PROBLEMS FOUND"}[rep.OK])
	if !rep.OK {
		return 1
	}
	return 0
}

// runCompactCmd implements nori compact: rebuild the data file from live
// content so freed pages are actually returned to the filesystem.
func runCompactCmd(args []string, inheritData string) int {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	dataDir := fs.String("data", inheritData, "data directory to compact")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, code := openStore(*dataDir)
	if s == nil {
		return code
	}

	tmp := *dataDir + ".compact-tmp"
	if err := os.RemoveAll(tmp); err != nil {
		fmt.Fprintf(os.Stderr, "clear %s: %v\n", tmp, err)
		return 1
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", tmp, err)
		return 1
	}
	stats, skipped, err := s.CompactInto(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		s.Close()
		fmt.Fprintf(os.Stderr, "compact failed: %v\n", err)
		return 1
	}
	for _, st := range stats {
		fmt.Printf("  %-20s %5d docs\n", st.Collection, st.Docs)
	}
	for _, name := range skipped {
		fmt.Printf("  skipped %s (incomplete import; recovery handles it)\n", name)
	}
	if err := s.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close store: %v\n", err)
		return 1
	}

	// swap directories: the old dir is kept as a timestamped backup
	bak := fmt.Sprintf("%s.bak-%s", *dataDir, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(*dataDir, bak); err != nil {
		os.RemoveAll(tmp)
		fmt.Fprintf(os.Stderr, "move old dir: %v\n", err)
		return 1
	}
	if err := os.Rename(tmp, *dataDir); err != nil {
		os.Rename(bak, *dataDir)
		fmt.Fprintf(os.Stderr, "move compacted dir: %v\n", err)
		return 1
	}

	oldSize, newSize := api.DirSize(bak), api.DirSize(*dataDir)
	fmt.Printf("compact done: %s -> %s (was %s)\n", humanBytes(oldSize), humanBytes(newSize), bak)

	// prove the rebuilt dir reads back clean before declaring victory
	s2, code := openStore(*dataDir)
	if s2 == nil {
		return code
	}
	rep := s2.Check()
	s2.Close()
	if !rep.OK {
		fmt.Fprintf(os.Stderr, "WARNING: compacted dir failed check - restore with:\n  mv %s %s\n", bak, *dataDir)
		return 1
	}
	fmt.Printf("verify: OK - delete %s when satisfied\n", bak)
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// dumpJSON writes a dump while whole-table rewrites are paused, so the
// dump never catches a table between a swap's drop and its reinsert.
func dumpJSON(s *store.Store, dir string, keep int) (string, error) {
	resume := s.PauseRewrites()
	defer resume()
	return backup.DumpJSON(s.DB(), dir, keep)
}

// probeDataLock reports whether the clover data file in dir is locked by
// another process, before store.Open blocks forever waiting on bbolt's
// flock. A missing data file means a fresh dir - nothing to contend with.
func probeDataLock(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, "data.db"), os.O_RDWR, 0o644)
	if err != nil {
		return nil // no data file yet: fresh dir
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("locked")
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return nil
}
