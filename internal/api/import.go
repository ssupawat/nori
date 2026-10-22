package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"nori/internal/sheetimport"
)

// MaxImportBytes caps the multipart upload body of POST /api/import.
var MaxImportBytes int64 = 64 << 20

// errStopImport stops ParseStream after a table has already failed and
// the HTTP error response is written.
var errStopImport = errors.New("stop import")

func (sv *Server) importXlsx(w http.ResponseWriter, r *http.Request) {
	// imports are heavy and rare — serialize them so concurrent
	// mode=replace imports of one table cannot race on staging
	sv.importMu.Lock()
	defer sv.importMu.Unlock()

	// cap before any body read: FormValue parses the whole multipart body
	r.Body = http.MaxBytesReader(w, r.Body, MaxImportBytes)
	mode := r.FormValue("mode")
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" {
		writeError(w, http.StatusBadRequest, `mode must be "replace" or "append"`)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file too large, max is %d MB", MaxImportBytes>>20))
			return
		}
		writeError(w, http.StatusBadRequest, "multipart field \"file\" with an .xlsx file is required")
		return
	}
	defer file.Close()

	summary := make([]map[string]any, 0, 4)
	err = sheetimport.ParseStream(file, func(tb sheetimport.TableStream) error {
		// Sample (first page) drives column type detection only; tb.Rows
		// yields every row, sample included.
		cols := sv.store.DetectColumns(tb.Columns, tb.Sample)
		var (
			n   int
			err error
		)
		if mode == "replace" {
			n, err = sv.store.ReplaceTableStream(tb.Name, cols, tb.Rows)
		} else {
			n, err = sv.store.AppendTableStream(tb.Name, cols, tb.Rows)
		}
		if mapStoreErr(w, err) {
			return errStopImport
		}
		sv.auditEvent(r, "import", tb.Name, "", map[string]string{"mode": mode, "rows": strconv.Itoa(n)})
		summary = append(summary, map[string]any{
			"name":    tb.Name,
			"columns": tb.Columns,
			"rows":    n,
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStopImport) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(summary) == 0 {
		writeError(w, http.StatusBadRequest, "no sheets with data found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "tables": summary})
}
