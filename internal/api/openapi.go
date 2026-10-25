package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"nori/internal/auth"
	"nori/internal/store"
)

// The OpenAPI document is built from the helpers below and served at
// /openapi.json, with the typed per-table paths of openapi_tables.go added
// for the tables that exist at request time. TestOpenAPISpecCoversRoutes
// pins route coverage against the real router. When a handler changes,
// change it here.

// specJSON encodes the spec with the given tables' typed paths.
func specJSON(tables []store.TableInfo) ([]byte, error) {
	doc := openAPI()
	addTablePaths(doc, tables)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- builders -------------------------------------------------------------

func obj(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func sref(schema string) map[string]any {
	return obj("$ref", "#/components/schemas/"+schema)
}

func rref(response string) map[string]any {
	return obj("$ref", "#/components/responses/"+response)
}

func jsonResponse(schema map[string]any) map[string]any {
	return obj("content", obj("application/json", obj("schema", schema)))
}

func errBody() map[string]any { return jsonResponse(sref("Error")) }

// response builds one status; schema nil means no body.
func response(status, description string, schema map[string]any) map[string]any {
	out := obj("description", description)
	if schema != nil {
		out["content"] = obj("application/json", obj("schema", schema))
	}
	return map[string]any{status: out}
}

// errResponse builds an inline error response for statuses the shared
// components don't cover (409, 429).
func errResponse(status, description string) map[string]any {
	out := obj("description", description)
	for k, v := range errBody() {
		out[k] = v
	}
	return map[string]any{status: out}
}

// plain builds a status with a description and no body.
func plain(status, description string) map[string]any {
	return map[string]any{status: obj("description", description)}
}

// refs maps statuses to the shared response components.
func refs(statusAndNames ...string) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(statusAndNames); i += 2 {
		out[statusAndNames[i]] = rref(statusAndNames[i+1])
	}
	return out
}

func merge(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func op(id, tag, summary string) map[string]any {
	return obj("operationId", id, "tags", []string{tag}, "summary", summary)
}

func pathParams(names ...string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, obj("name", n, "in", "path", "required", true, "schema", obj("type", "string")))
	}
	return out
}

func queryParam(name, description string) map[string]any {
	return obj("name", name, "in", "query", "schema", obj("type", "string"), "description", description)
}

func requestBody(schema map[string]any) map[string]any {
	return obj("required", true, "content", obj("application/json", obj("schema", schema)))
}

// validateAny accepts both auth lanes: human session tokens and machine
// API keys (nri_...). Machine-readable endpoints that serve agents use it.
func (sv *Server) validateAny(token string) (auth.User, error) {
	if strings.HasPrefix(token, auth.KeyPrefix) {
		return sv.auth.ValidateAPIKey(token)
	}
	return sv.auth.Validate(token)
}

// ---- the document ---------------------------------------------------------

func openAPI() map[string]any {
	strEnum := []string{"text", "number", "date", "datetime"}
	roleEnum := []string{"admin", "editor", "viewer", "builder"}
	keyRoleEnum := []string{"admin", "editor", "viewer", "builder"}

	schemas := obj(
		"Error", obj("type", "object", "properties", obj("error", obj("type", "string")), "required", []string{"error"}),
		"Column", obj("type", "object", "properties", obj(
			"name", obj("type", "string"),
			"type", obj("type", "string", "enum", strEnum),
		)),
		"Table", obj("type", "object", "properties", obj(
			"name", obj("type", "string"),
			"columns", obj("type", "array", "items", obj("type", "string")),
			"types", obj("type", "array", "items", obj("type", "string", "enum", strEnum)),
		)),
		"Row", obj("type", "object",
			"description", "Flat object: 'id' plus one string value per column.",
			"additionalProperties", obj("type", "string"),
			"properties", obj("id", obj("type", "string", "description", "server-generated row id")),
		),
		"RowPatch", obj("type", "object",
			"description", "Columns to change. A value is the new string, or, for a number column, {\"inc\": n} to add n to the stored number atomically.",
			"additionalProperties", obj("oneOf", []any{
				obj("type", "string"),
				obj("type", "object",
					"properties", obj("inc", obj("oneOf", []any{obj("type", "number"), obj("type", "string")},
						"description", "amount to add; negative to subtract; an empty cell counts as 0")),
					"required", []string{"inc"},
					"additionalProperties", false),
			}),
		),
		"RowsPage", obj("type", "object", "properties", obj(
			"columns", obj("type", "array", "items", obj("type", "string")),
			"types", obj("type", "array", "items", obj("type", "string")),
			"total", obj("type", "integer", "description", "rows matching the query, before pagination"),
			"limit", obj("type", "integer"),
			"offset", obj("type", "integer"),
			"rows", obj("type", "array", "items", sref("Row")),
		)),
		"User", obj("type", "object", "properties", obj(
			"username", obj("type", "string"),
			"role", obj("type", "string", "enum", roleEnum),
			"createdAt", obj("type", "string", "format", "date-time", "description", "absent for accounts created before it was recorded"),
			"lastLoginAt", obj("type", "string", "format", "date-time", "description", "absent until the first sign-in"),
			"failedLogins", obj("type", "integer", "description", "wrong passwords since the last successful sign-in"),
			"lastFailedAt", obj("type", "string", "format", "date-time"),
		)),
		"ApiKeyInfo", obj("type", "object", "properties", obj(
			"id", obj("type", "string"),
			"name", obj("type", "string"),
			"prefix", obj("type", "string", "description", "first characters of the key, for identification"),
			"role", obj("type", "string", "enum", roleEnum),
			"createdAt", obj("type", "string"),
			"lastUsedAt", obj("type", "string"),
		)),
		"Branding", obj("type", "object", "properties", obj(
			"name", obj("type", "string"),
			"tagline", obj("type", "string"),
			"color", obj("type", "string", "description", "hex accent color, e.g. #2563eb"),
			"logoUrl", obj("type", "string"),
		)),
		"Usage", obj("type", "object", "properties", obj(
			"requests24h", obj("type", "integer"),
			"byPath", obj("type", "object", "additionalProperties", obj("type", "integer")),
			"byHour", obj("type", "array",
				"description", "per-hour request totals across all paths, oldest first (last bucket = current hour)",
				"items", obj("type", "integer")),
			"tables", obj("type", "array", "items", obj("type", "object", "properties", obj(
				"name", obj("type", "string"),
				"rows", obj("type", "integer"),
			))),
			"maxRows", obj("type", "integer", "description", "max rows per table"),
		)),
		"AuditEntry", obj("type", "object", "properties", obj(
			"at", obj("type", "string", "description", "when it happened (UTC)"),
			"user", obj("type", "string"),
			"action", obj("type", "string",
				"description", "create_row, update_row, delete_row, create_table, rename_table, delete_table, add_column, rename_column, set_column_type, import, create_user, delete_user, set_password, set_date_order, login, login_failed. Entries recorded by older versions may use the unqualified create/update/delete for row actions."),
			"table", obj("type", "string"),
			"rowId", obj("type", "string"),
			"values", obj("type", "object", "additionalProperties", obj("type", "string"),
				"description", "new values for create/update, the removed row for delete, changed fields only for update"),
		), "required", []string{"at", "user", "action", "table"}),
		"Status", obj("type", "object", "properties", obj(
			"version", obj("type", "string"),
			"uptimeSeconds", obj("type", "integer"),
			"dataDir", obj("type", "string"),
			"dataBytes", obj("type", "integer", "description", "total size of the data directory"),
			"diskFreeBytes", obj("type", "integer", "description", "available bytes on the filesystem holding the data dir; 0 = unknown"),
			"backup", obj("allOf", []any{sref("BackupStatus")}, "description", "absent when backups are disabled"),
		)),
		"BackupStatus", obj("type", "object", "properties", obj(
			"dir", obj("type", "string"),
			"everySeconds", obj("type", "integer"),
			"lastAt", obj("type", "string", "description", "RFC3339 time of the last dump"),
			"lastPath", obj("type", "string"),
			"lastError", obj("type", "string", "description", "set when the last dump failed"),
		)),
		"TableColumns", obj("type", "object", "properties", obj(
			"name", obj("type", "string"),
			"columns", obj("type", "array", "items", sref("Column")),
		), "required", []string{"name", "columns"}),
		"ColumnList", obj("type", "object", "properties", obj(
			"columns", obj("type", "array", "items", sref("Column")),
		), "required", []string{"columns"}),
	)

	responses := obj(
		"BadRequest", merge(obj("description", "Invalid input"), errBody()),
		"Unauthorized", merge(obj("description", "Missing or invalid credentials"), errBody()),
		"Forbidden", merge(obj("description", "Insufficient role"), errBody()),
		"NotFound", merge(obj("description", "Not found"), errBody()),
	)

	tableParam := pathParams("t")

	paths := obj(
		"/api/health", obj("get", merge(op("getHealth", "System", "Health check (public)"), obj(
			"security", []any{},
			"responses", response("200", "OK", obj("type", "object", "properties", obj("status", obj("type", "string")))),
		))),
		"/api/branding", obj(
			"get", merge(op("getBranding", "Admin", "White-label branding (public)"), obj(
				"security", []any{},
				"responses", response("200", "OK", sref("Branding")),
			)),
			"patch", merge(op("updateBranding", "Admin", "Set branding override (admin)"), obj(
				"requestBody", merge(obj("required", true), jsonResponse(sref("Branding"))),
				"responses", merge(
					response("200", "Effective branding", sref("Branding")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
				),
			)),
			"delete", merge(op("resetBranding", "Admin", "Reset branding to startup flags (admin)"), obj(
				"responses", merge(
					response("200", "Effective branding", sref("Branding")),
					refs("401", "Unauthorized", "403", "Forbidden"),
				),
			)),
		),
		"/api/login", obj("post", merge(op("login", "Auth", "Human login - rate-limited, agents should use API keys instead"), obj(
			"security", []any{},
			"requestBody", requestBody(obj(
				"type", "object",
				"properties", obj("username", obj("type", "string"), "password", obj("type", "string")),
				"required", []string{"username", "password"},
			)),
			"responses", merge(
				response("200", "OK", obj("type", "object", "properties", obj(
					"token", obj("type", "string"),
					"username", obj("type", "string"),
					"role", obj("type", "string"),
				))),
				refs("400", "BadRequest", "401", "Unauthorized"),
				errResponse("429", "Too many attempts from this IP"),
			),
		))),
		"/api/logout", obj("post", merge(op("logout", "Auth", "Revoke the current session"), obj(
			"responses", merge(plain("200", "OK"), refs("401", "Unauthorized")),
		))),
		"/api/tables", obj(
			"get", merge(op("listTables", "Tables", "List tables"), obj(
				"responses", merge(
					response("200", "OK", obj("type", "array", "items", sref("Table"))),
					refs("401", "Unauthorized"),
				),
			)),
			"post", merge(op("createTable", "Tables", "Create an empty table (admin)"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj("name", obj("type", "string"), "columns", obj("type", "array", "items", sref("Column"))),
					"required", []string{"name", "columns"},
				)),
				"responses", merge(
					response("201", "Created", sref("TableColumns")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
					errResponse("409", "Table already exists"),
				),
			)),
		),
		"/api/tables/{t}", obj(
			"parameters", tableParam,
			"patch", merge(op("renameTable", "Tables", "Rename table (admin)"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj("name", obj("type", "string")),
					"required", []string{"name"},
				)),
				"responses", merge(
					response("200", "OK", sref("TableColumns")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
					errResponse("409", "A table with the new name already exists"),
				),
			)),
			"delete", merge(op("deleteTable", "Tables", "Drop table and all rows (admin)"), obj(
				"responses", merge(plain("200", "OK"),
					refs("401", "Unauthorized", "403", "Forbidden", "404", "NotFound")),
			)),
		),
		"/api/tables/{t}/columns", obj(
			"parameters", tableParam,
			"post", merge(op("addColumn", "Tables", "Add a column (admin)"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj("name", obj("type", "string"), "type", obj("type", "string", "enum", strEnum)),
					"required", []string{"name"},
				)),
				"responses", merge(
					response("201", "Created", sref("ColumnList")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
					errResponse("409", "Column already exists"),
				),
			)),
		),
		"/api/tables/{t}/columns/{col}", obj(
			"parameters", pathParams("t", "col"),
			"patch", merge(op("updateColumn", "Tables", "Rename a column and/or change its type (admin)"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj(
						"name", obj("type", "string"),
						"type", obj("type", "string", "enum", strEnum),
						"convert", obj("type", "boolean", "description", "rewrite existing values to the new canonical form"),
					),
				)),
				"responses", merge(
					response("200", "OK", sref("ColumnList")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
					errResponse("409", "A column with the new name already exists"),
				),
			)),
			"delete", merge(op("deleteColumn", "Tables", "Drop a column and its values from every row (admin)"), obj(
				"responses", merge(
					response("200", "OK", sref("ColumnList")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
		),
		"/api/tables/{t}/rows", obj(
			"parameters", tableParam,
			"get", merge(op("listRows", "Rows", "List rows (paginated, searchable, sortable)"), obj(
				"description", "Any column name is also a query parameter: a case-insensitive contains-filter on that column (e.g. email=user001). A value may start with an operator: eq:/ne: (exact), lt:/lte:/gt:/gte: (number, date, datetime columns; e.g. qty=lt:5), contains: (the default, spelled out). Repeat a column for a range (qty=gte:5&qty=lt:10); repeated plain values must all match too. An operand col:<name> compares with that column of the same row (qty=lt:col:reorder_level; both columns of one type). All filters must match. Each table also has typed paths (/api/tables/<name>/rows) with one parameter per column.",
				"parameters", []any{
					obj("name", "limit", "in", "query", "schema", obj("type", "integer", "default", 50, "maximum", 1000), "description", "page size"),
					obj("name", "offset", "in", "query", "schema", obj("type", "integer", "default", 0)),
					queryParam("q", "case-insensitive contains-match across all columns"),
					obj("name", "sort", "in", "query", "schema", obj("type", "string"), "description", "column name to sort by"),
					obj("name", "order", "in", "query", "schema", obj("type", "string", "enum", []string{"asc", "desc"}, "default", "asc")),
				},
				"responses", merge(
					response("200", "OK", sref("RowsPage")),
					refs("400", "BadRequest", "401", "Unauthorized", "404", "NotFound"),
				),
			)),
			"post", merge(op("createRow", "Rows", "Create a row (editor+)"), obj(
				"requestBody", obj("required", true, "description", "column values; validated against column types",
					"content", obj("application/json", obj("schema", sref("Row")))),
				"responses", merge(
					response("201", "Created row (with id)", sref("Row")),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
		),
		"/api/tables/{t}/rows/{id}", obj(
			"parameters", pathParams("t", "id"),
			"get", merge(op("getRow", "Rows", "Get one row"), obj(
				"responses", merge(
					response("200", "OK", sref("Row")),
					refs("401", "Unauthorized", "404", "NotFound"),
				),
			)),
			"patch", merge(op("updateRow", "Rows", "Partial update of a row (editor+). Send only the columns to change; a number column may take {\"inc\": n} instead of a value to add n to the stored number atomically (negative to subtract), e.g. {\"qty\": {\"inc\": -1}}"), obj(
				"requestBody", requestBody(sref("RowPatch")),
				"responses", merge(
					plain("200", "Updated row"),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
			"delete", merge(op("deleteRow", "Rows", "Delete a row (editor+)"), obj(
				"responses", merge(plain("200", "OK"),
					refs("401", "Unauthorized", "403", "Forbidden", "404", "NotFound")),
			)),
		),
		"/api/tables/{t}/export", obj(
			"parameters", tableParam,
			"get", merge(op("exportTable", "Import/Export", "Download the table as .xlsx"), obj(
				"responses", merge(
					map[string]any{"200": obj("description", "xlsx file", "content", map[string]any{
						"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": obj(),
					})},
					refs("401", "Unauthorized", "404", "NotFound"),
				),
			)),
		),
		"/api/import", obj("post", merge(op("importXlsx", "Import/Export", "Import .xlsx files (admin)"), obj(
			"requestBody", obj("required", true, "content", obj("multipart/form-data", obj("schema", obj(
				"type", "object",
				"properties", obj(
					"file", obj("type", "string", "format", "binary", "description", ".xlsx file, max 64MB; each sheet becomes a table"),
					"mode", obj("type", "string", "enum", []string{"replace", "append"}, "default", "replace"),
				),
				"required", []string{"file"},
			)))),
			"responses", merge(
				plain("200", "Import summary"),
				refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
				plain("413", "File too large"),
			),
		))),
		"/api/users/", obj(
			"get", merge(op("listUsers", "Users", "List users (admin)"), obj(
				"responses", merge(
					response("200", "OK", obj("type", "array", "items", sref("User"))),
					refs("401", "Unauthorized", "403", "Forbidden"),
				),
			)),
			"post", merge(op("createUser", "Users", "Create a user (admin)"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj(
						"username", obj("type", "string"),
						"password", obj("type", "string", "minLength", 8,
							"description", "optional - when omitted a random password is generated and returned once in the response"),
						"role", obj("type", "string", "enum", keyRoleEnum),
					),
					"required", []string{"username", "role"},
				)),
				"responses", merge(
					response("201", "Created; 'password' present exactly once when it was generated",
						obj("type", "object", "properties", obj(
							"username", obj("type", "string"),
							"role", obj("type", "string"),
							"password", obj("type", "string", "description", "only present when it was generated"),
						))),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
					plain("409", "Username taken"),
				),
			)),
		),
		"/api/users/{name}", obj(
			"parameters", pathParams("name"),
			"delete", merge(op("deleteUser", "Users", "Delete a user and their sessions (admin)"), obj(
				"responses", merge(plain("200", "OK"),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden")),
			)),
			"patch", merge(op("updateUser", "Users", "Change a user's password (revokes that user's sessions) and/or role (admin)"), obj(
				"security", []any{obj("bearerAuth", []any{})},
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj(
						"password", obj("type", "string", "minLength", 8),
						"role", obj("type", "string", "enum", []string{"admin", "editor", "viewer", "builder"}, "description", "applies to live sessions at once; the last admin cannot be demoted, nor your own account"),
					),
				)),
				"responses", merge(
					plain("200", "Updated; a password change revokes all sessions of that user"),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
		),
		"/api/keys/", obj(
			"get", merge(op("listApiKeys", "API keys", "List API keys - never includes full keys (admin)"), obj(
				"responses", merge(
					response("200", "OK", obj("type", "array", "items", sref("ApiKeyInfo"))),
					refs("401", "Unauthorized", "403", "Forbidden"),
				),
			)),
			"post", merge(op("createApiKey", "API keys", "Create an API key (admin) - the full key is returned only in this response"), obj(
				"requestBody", requestBody(obj(
					"type", "object",
					"properties", obj("name", obj("type", "string"), "role", obj("type", "string", "enum", keyRoleEnum)),
					"required", []string{"name", "role"},
				)),
				"responses", merge(
					response("201", "Created; contains 'key' (nri_...) exactly once", obj("type", "object", "properties", obj(
						"id", obj("type", "string"),
						"name", obj("type", "string"),
						"prefix", obj("type", "string"),
						"role", obj("type", "string"),
						"key", obj("type", "string"),
					))),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
					errResponse("409", "An API key with this name already exists"),
				),
			)),
		),
		"/api/keys/{id}", obj(
			"parameters", pathParams("id"),
			"delete", merge(op("revokeApiKey", "API keys", "Revoke an API key immediately (admin)"), obj(
				"responses", merge(plain("200", "OK"), refs("401", "Unauthorized", "403", "Forbidden")),
			)),
		),
		"/api/usage", obj("get", merge(op("getUsage", "Admin", "Request counts (24h) and table sizes vs caps (admin)"), obj(
			"responses", merge(
				response("200", "OK", sref("Usage")),
				refs("401", "Unauthorized", "403", "Forbidden"),
			),
		))),
		"/api/settings/dates", obj(
			"get", merge(op("getDateSettings", "Admin", "How ambiguous slash dates (3/4/2026) are read: dmy or mdy (admin)"), obj(
				"responses", merge(
					response("200", "OK", obj("type", "object", "properties", obj(
						"order", obj("type", "string", "enum", []string{"dmy", "mdy"}),
						"buddhistYears", obj("type", "boolean", "description", "years >= 2400 are read as Buddhist Era and converted to CE (2567 -> 2024)"),
					))),
					plain("401", "Unauthorized"),
					plain("403", "Forbidden"),
				),
			)),
			"put", merge(op("updateDateSettings", "Admin", "Set the ambiguous-slash-date order; takes effect immediately (admin)"), obj(
				"requestBody", obj("required", false, "content", obj("application/json", obj("schema", obj(
					"type", "object",
					"properties", obj(
						"order", obj("type", "string", "enum", []string{"dmy", "mdy"}),
						"buddhistYears", obj("type", "boolean", "description", "years >= 2400 are read as Buddhist Era and converted to CE (2567 -> 2024)"),
					),
				)))),
				"responses", merge(
					response("200", "OK", obj("type", "object", "properties", obj(
						"order", obj("type", "string", "enum", []string{"dmy", "mdy"}),
						"buddhistYears", obj("type", "boolean", "description", "years >= 2400 are read as Buddhist Era and converted to CE (2567 -> 2024)"),
					))),
					plain("400", "Invalid order"),
					plain("401", "Unauthorized"),
					plain("403", "Forbidden"),
				),
			)),
		),
		"/llms.txt", obj("get", merge(op("getLlmsTxt", "System", "Plain-text API handbook written for AI agents"), obj(
			"responses", merge(
				map[string]any{"200": obj("description", "text/plain", "content", obj("text/plain", obj()))},
				refs("401", "Unauthorized"),
			),
		))),
		"/api/audit", obj("get", merge(obj(
			"operationId", "listAudit",
			"summary", "Audit trail: who changed what, newest first",
			"security", []any{obj("bearerAuth", []any{})},
			"parameters", []any{
				queryParam("table", "only entries for this table"),
				queryParam("rowId", "only entries for this row"),
				queryParam("user", "only entries made by this actor (a username, or key:<name> for an API key)"),
				obj("name", "limit", "in", "query", "schema", obj("type", "integer", "default", 100, "maximum", 1000)),
			},
			"responses", merge(
				response("200", "Audit entries, newest first", obj("type", "array", "items", sref("AuditEntry"))),
				refs("401", "Unauthorized", "403", "Forbidden"),
			),
			"tags", []string{"Admin"},
		))),
		"/api/status", obj("get", merge(op("getStatus", "Admin", "Server version, uptime, data size, free disk and backup state (admin)"), obj(
			"responses", merge(
				response("200", "OK", sref("Status")),
				refs("401", "Unauthorized", "403", "Forbidden"),
			),
		))),
		"/api/backup", obj("post", merge(
			op("createBackup", "Admin", "Write a backup dump now (admin)"),
			obj(
				"description", "Disabled unless the server was started with -backup-dir.",
				"responses", merge(
					response("200", "Dump written", obj("type", "object", "properties", obj("path", obj("type", "string")))),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden"),
				),
			),
		)),
		"/api/me", obj("get", merge(op("getMe", "Auth", "Who the caller is (any role, session or API key)"), obj(
			"responses", merge(
				response("200", "OK", obj("type", "object", "properties", obj(
					"username", obj("type", "string"),
					"role", obj("type", "string", "enum", roleEnum),
				))),
				refs("401", "Unauthorized"),
			),
		))),
		"/api/me/password", obj("post", merge(op("changeOwnPassword", "Auth", "Change your own password (any role)"), obj(
			"description", "Revokes every other session of the user; the calling session stays signed in.",
			"requestBody", requestBody(obj(
				"type", "object",
				"properties", obj(
					"currentPassword", obj("type", "string"),
					"password", obj("type", "string", "minLength", 8),
				),
				"required", []string{"currentPassword", "password"},
			)),
			"responses", merge(
				plain("200", "OK"),
				refs("400", "BadRequest", "401", "Unauthorized"),
			),
		))),
		"/api/sample.xlsx", obj("get", merge(
			op("getSampleXlsx", "Admin", "Generated sample workbook (admin)"),
			obj(
				"description", "A three-sheet demo file (Products, Customers, Orders) to import when there is no spreadsheet at hand.",
			),
			obj("responses", merge(
				map[string]any{"200": obj(
					"description", "The workbook as .xlsx",
					"content", obj("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
						obj("schema", obj("type", "string", "format", "binary"))),
				)},
				refs("401", "Unauthorized", "403", "Forbidden"),
			)),
		)),
	)

	return obj(
		"openapi", "3.1.0",
		"info", obj(
			"title", "nori API",
			"version", "1.0.0",
			"description", "Private spreadsheet backend. Every value is a string, same as a spreadsheet cell. Each imported sheet (or created table) is a table; rows are flat objects keyed by column name plus an 'id'. Authentication is HTTP bearer: humans use a session token from POST /api/login, machines use an API key (nri_...). Roles: admin > editor > viewer. Column values are validated against the column type (text/number/date/datetime) and normalized to canonical form on write. datetime values are stored as RFC3339 UTC (naive input is treated as UTC) - convert to the user's zone for display. Errors are always {\"error\": \"message\"}.",
		),
		"servers", []any{obj("url", "/")},
		"tags", []any{
			obj("name", "System"),
			obj("name", "Auth"),
			obj("name", "Tables"),
			obj("name", "Rows"),
			obj("name", "Import/Export"),
			obj("name", "Users"),
			obj("name", "API keys"),
			obj("name", "Admin"),
		},
		"components", obj(
			"securitySchemes", obj("bearerAuth", obj(
				"type", "http",
				"scheme", "bearer",
				"description", "Session token (from POST /api/login) or API key (nri_...).",
			)),
			"schemas", schemas,
			"responses", responses,
		),
		"security", []any{obj("bearerAuth", []any{})},
		"paths", paths,
	)
}

// OpenAPISpec serves the OpenAPI 3.1 spec of this API, same auth rule as
// /llms.txt.
func (sv *Server) OpenAPISpec(w http.ResponseWriter, r *http.Request) {
	user, err := sv.validateAny(bearerToken(r))
	if err != nil || user.Role == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tables, err := sv.store.ListTables()
	if err != nil {
		serverError(w, err)
		return
	}
	body, err := specJSON(tables)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
