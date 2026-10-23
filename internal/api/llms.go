package api

import (
	"fmt"
	"net/http"
	"strings"

	"nori/internal/store"
)

// LLMsTxt serves the agent handbook: what nori is, how to authenticate,
// the response grammar and the limits - plus a pointer to /openapi.json,
// which carries the full endpoint reference. The table listing is the
// instance-specific part.
func (sv *Server) LLMsTxt(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	user, err := sv.validateAny(token)
	if err != nil || user.Role == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tables, err := sv.store.ListTables()
	if err != nil {
		serverError(w, err)
		return
	}

	var b strings.Builder
	b.WriteString("# nori\n\n")
	b.WriteString("Import a spreadsheet, get an API. Every value is a string, same as a spreadsheet cell. You (an agent) authenticate with an API key: ask the admin to create one in the dashboard (Access > API keys), then send `Authorization: Bearer nri_...` on every request except /api/health. POST /api/login issues human sessions (the dashboard itself is admin-only; browser frontends sign in the same way) and is rate-limited - agents must not use it from code.\n\n")
	b.WriteString("## Endpoint reference\n\nFull request/response shapes, status codes and examples: GET /openapi.json (OpenAPI 3.1, same bearer auth).\n\n")
	b.WriteString("## Response grammar\n\nOne grammar everywhere: missing resource → 404, invalid input → 400, existing name on create → 409, server or storage failure → 500 (safe to retry), create → 201 with the resource, update → 200 with the updated resource, delete → {\"status\":\"ok\"}, errors → {\"error\":\"...\"}. Rows always carry every column (\"\" when empty). Writes made with an API key show as \"key:<name>\" in the audit trail.\n\n")
	b.WriteString("## Querying and updating rows\n\n")
	b.WriteString("- GET /api/tables/{t}/rows filters on any column name: a plain value is a case-insensitive contains-match; prefix it with an operator for more: eq:/ne: (exact), lt:/lte:/gt:/gte: (number, date, datetime columns), e.g. ?sku=eq:A100 or ?qty=lt:5. Repeat a column for a range: ?qty=gte:5&qty=lt:10 (repeated plain values must all match too). Compare with another column of the same row via col:<name>, e.g. ?qty=lt:col:reorder_level (same column type). All filters must match.\n")
	b.WriteString("- PATCH /api/tables/{t}/rows/{id} sets the columns you send. To change a number relative to its stored value without a read-modify-write race, send {\"qty\":{\"inc\":-1}}; the server adds the delta atomically.\n\n")
	b.WriteString("## Limits\n\n")
	fmt.Fprintf(&b, "- %d rows max and %d columns max per table; writes or imports beyond that are rejected with 400\n- import file max %d MB\n- import decompresses at most 64 MB of worksheet XML (zip-bomb guard)\n- reads are cached up to %d rows and %d MB across all tables; past either limit, the largest cached tables fall back to slower per-request scans (same results)\n", store.MaxTableRows, store.MaxTableCols, MaxImportBytes>>20, store.MaxCachedRowsTotal, store.MaxCacheBytes>>20)
	b.WriteString("\n## Tables\n\n")
	for _, t := range tables {
		parts := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			if t.Types[i] != "text" {
				parts[i] = fmt.Sprintf("%s(%s)", c, t.Types[i])
			} else {
				parts[i] = c
			}
		}
		n, _ := sv.store.CountRows(t.Name)
		fmt.Fprintf(&b, "- %s: %s (%d rows)\n", t.Name, strings.Join(parts, ", "), n)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
