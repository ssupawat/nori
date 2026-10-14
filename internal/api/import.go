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

	tables, err := sheetimport.Parse(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, tb := range tables {
		cols := sv.store.DetectColumns(tb.Columns, tb.Rows)
		if mode == "replace" {
			err = sv.store.ReplaceTable(tb.Name, cols, tb.Rows)
		} else {
			err = sv.store.AppendTable(tb.Name, cols, tb.Rows)
		}
		if mapStoreErr(w, err) {
			return
		}
	}
	summary := make([]map[string]any, 0, len(tables))
	for _, tb := range tables {
		sv.auditEvent(r, "import", tb.Name, "", map[string]string{"mode": mode, "rows": strconv.Itoa(len(tb.Rows))})
		summary = append(summary, map[string]any{
			"name":    tb.Name,
			"columns": tb.Columns,
			"rows":    len(tb.Rows),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "tables": summary})
}
