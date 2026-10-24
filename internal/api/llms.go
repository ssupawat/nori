package api

import (
	"fmt"
	"net/http"
	"strings"

	"nori/internal/auth"
	"nori/internal/store"
)

// LLMsTxt serves the agent handbook: what nori is, how to authenticate,
// the response grammar and the limits - plus a pointer to /openapi.json,
// which carries the full endpoint reference. The table listing is the
// instance-specific part.
// roleCanDo is the first line of the Access section, for the caller's role.
var roleCanDo = map[auth.Role]string{
	auth.Admin:   "You can do everything listed below.",
	auth.Editor:  "You can read and write rows in every table, but not change tables, users or keys.",
	auth.Viewer:  "You can read rows in every table, but not write.",
	auth.Builder: "You can read the schema and these docs, but every row read or write returns 403; ask the admin for a viewer or editor key when you need data.",
}

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
	b.WriteString("## Access\n\n")
	fmt.Fprintf(&b, "You are %s with the %s role. %s\n\n", user.Username, user.Role, roleCanDo[user.Role])
	b.WriteString("- admin: everything, including the dashboard, import and export, tables and columns, users and API keys.\n")
	b.WriteString("- editor: read, create, update and delete rows in every table; no changes to tables, columns, users or keys.\n")
	b.WriteString("- viewer: read rows and export tables, every table; no writes.\n")
	b.WriteString("- builder: table names, columns, types, /llms.txt and /openapi.json; every row read or write is refused with 403.\n")
	b.WriteString("- A role applies to every table and every row. There is no per-table or per-row access, so anyone who can read a table can read all of it; filters such as ?email=eq:ann@example.com narrow a result, they don't restrict access.\n")
	b.WriteString("- 401 means missing or invalid credentials; 403 means your role doesn't allow the request. Ask the admin for a key with another role instead of retrying.\n")
	b.WriteString("- API keys (nri_...) don't expire and the admin can revoke them at any time. Never put a key in browser code: browser frontends sign people in with POST /api/login and send that session token.\n\n")
	b.WriteString("## Endpoint reference\n\nFull request/response shapes and status codes: GET /openapi.json (OpenAPI 3.1, same bearer auth). Every operation has an operationId, and each table below has typed paths there (/api/tables/<name>/rows) with its columns, types and one filter parameter per column.\n\n")
	b.WriteString("## Response grammar\n\nOne grammar everywhere: missing resource → 404, invalid input → 400, existing name on create → 409, server or storage failure → 500 (safe to retry), create → 201 with the resource, update → 200 with the updated resource, delete → {\"status\":\"ok\"}, errors → {\"error\":\"...\"}. Rows always carry every column (\"\" when empty). Writes made with an API key show as \"key:<name>\" in the audit trail.\n\n")
	b.WriteString("## Querying and updating rows\n\n")
	b.WriteString("- GET /api/tables/{t}/rows filters on any column name: a plain value is a case-insensitive contains-match; prefix it with an operator for more: eq:/ne: (exact), lt:/lte:/gt:/gte: (number, date, datetime columns), e.g. ?sku=eq:A100 or ?qty=lt:5. Repeat a column for a range: ?qty=gte:5&qty=lt:10 (repeated plain values must all match too). Compare with another column of the same row via col:<name>, e.g. ?qty=lt:col:reorder_level (same column type). All filters must match.\n")
	b.WriteString("- Lists are paged: limit (default 50, max 1000) and offset. The response's total counts every matching row, so keep requesting with a higher offset until offset >= total.\n")
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
