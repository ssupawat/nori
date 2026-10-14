package api

import (
	"bytes"
	"net/http"

	"nori/internal/sample"
)

// sampleWorkbook streams the generated demo workbook, the no-file-at-hand
// path into the import loop. Same auth rule as every other write: admin.
func (sv *Server) sampleWorkbook(w http.ResponseWriter, r *http.Request) {
	f, err := sample.New()
	if err != nil {
		serverError(w, err)
		return
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="sample.xlsx"`)
	_, _ = w.Write(buf.Bytes())
}
