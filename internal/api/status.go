package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"time"
)

// Status is the server report behind GET /api/status (admin): build
// version, uptime, storage footprint, and backup state. Values are
// collected by a provider func installed by main, so the api package
// stays independent of how the server was started.
type Status struct {
	Version       string        `json:"version"`
	UptimeSeconds int64         `json:"uptimeSeconds"`
	DataDir       string        `json:"dataDir"`
	DataBytes     int64         `json:"dataBytes"`
	DiskFreeBytes int64         `json:"diskFreeBytes"`
	Backup        *BackupStatus `json:"backup,omitempty"`
}

type BackupStatus struct {
	Dir          string `json:"dir"`
	EverySeconds int64  `json:"everySeconds"`
	LastAt       string `json:"lastAt,omitempty"` // RFC3339
	LastPath     string `json:"lastPath,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

func (sv *Server) status(w http.ResponseWriter, r *http.Request) {
	var st *Status
	if sv.statusProvider == nil {
		st = &Status{Version: "dev"}
	} else {
		st = sv.statusProvider()
	}
	writeJSON(w, http.StatusOK, st)
}

// runBackup runs an immediate dump via the action installed by main.
func (sv *Server) runBackup(w http.ResponseWriter, r *http.Request) {
	if sv.backupNow == nil {
		writeError(w, http.StatusBadRequest, "backups are disabled - start with -backup-dir")
		return
	}
	path, err := sv.backupNow()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// DirSize returns the total size of regular files under dir (best effort:
// unreadable entries count as zero).
func DirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// SinceRFC3339 formats a time for the API, empty when zero.
func SinceRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
