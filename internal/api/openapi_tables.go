package api

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"nori/internal/store"
)

// Per-table part of /openapi.json: for every table, typed paths for its
// rows next to the generic /api/tables/{t}/... ones, with a schema per
// column type, one filter parameter per column and a sort enum.

// reservedListParams are list query params that are never column filters.
var reservedListParams = map[string]bool{"limit": true, "offset": true, "q": true, "sort": true, "order": true}

// addTablePaths adds the typed paths and schemas of each table to doc.
func addTablePaths(doc map[string]any, tables []store.TableInfo) {
	paths := doc["paths"].(map[string]any)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	tags := doc["tags"].([]any)
	used := map[string]bool{}
	for _, t := range tables {
		ident := tableIdent(t.Name, used)
		tag := "Table " + t.Name
		tags = append(tags, obj("name", tag, "description", "rows of the "+t.Name+" table, typed by its columns"))

		schemas["Row_"+ident] = rowSchema(t)
		schemas["RowCreate_"+ident] = rowWriteSchema(t, false)
		schemas["RowPatch_"+ident] = rowWriteSchema(t, true)
		schemas["RowsPage_"+ident] = obj("type", "object", "properties", obj(
			"columns", obj("type", "array", "items", obj("type", "string")),
			"types", obj("type", "array", "items", obj("type", "string")),
			"total", obj("type", "integer", "description", "rows matching the query, before pagination"),
			"limit", obj("type", "integer"),
			"offset", obj("type", "integer"),
			"rows", obj("type", "array", "items", sref("Row_"+ident)),
		))

		base := "/api/tables/" + url.PathEscape(t.Name) + "/rows"
		paths[base] = obj(
			"get", merge(op("list"+ident+"Rows", tag, "List "+t.Name+" rows (paginated, searchable, sortable)"), obj(
				"parameters", listParams(t),
				"responses", merge(
					response("200", "OK", sref("RowsPage_"+ident)),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
			"post", merge(op("create"+ident+"Row", tag, "Create a "+t.Name+" row (editor+)"), obj(
				"requestBody", requestBody(sref("RowCreate_"+ident)),
				"responses", merge(
					response("201", "Created row (with id)", sref("Row_"+ident)),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
		)
		paths[base+"/{id}"] = obj(
			"parameters", pathParams("id"),
			"get", merge(op("get"+ident+"Row", tag, "Get one "+t.Name+" row"), obj(
				"responses", merge(
					response("200", "OK", sref("Row_"+ident)),
					refs("401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
			"patch", merge(op("update"+ident+"Row", tag, "Partial update of a "+t.Name+" row (editor+)"), obj(
				"requestBody", requestBody(sref("RowPatch_"+ident)),
				"responses", merge(
					response("200", "Updated row", sref("Row_"+ident)),
					refs("400", "BadRequest", "401", "Unauthorized", "403", "Forbidden", "404", "NotFound"),
				),
			)),
			"delete", merge(op("delete"+ident+"Row", tag, "Delete a "+t.Name+" row (editor+)"), obj(
				"responses", merge(plain("200", "OK"),
					refs("401", "Unauthorized", "403", "Forbidden", "404", "NotFound")),
			)),
		)
	}
	doc["tags"] = tags
}

// tableIdent turns a table name into a unique ASCII identifier for schema
// names and operationIds: "order items" -> "OrderItems". Names with no
// ASCII letters or digits become Table, Table2, ...
func tableIdent(name string, used map[string]bool) string {
	var b strings.Builder
	upper := true
	for _, r := range name {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if upper {
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
			upper = false
		} else {
			upper = true
		}
	}
	id := b.String()
	if id == "" || unicode.IsDigit(rune(id[0])) {
		id = "Table" + id
	}
	unique := id
	for n := 2; used[unique]; n++ {
		unique = fmt.Sprintf("%s%d", id, n)
	}
	used[unique] = true
	return unique
}

// cellSchema describes one column's values as served: always a string,
// in the canonical form of its type.
func cellSchema(colType string) map[string]any {
	switch colType {
	case store.TypeNumber:
		return obj("type", "string", "x-nori-type", colType,
			"description", "number, as a canonical decimal string (\"1234.5\"); \"\" when empty")
	case store.TypeDate:
		return obj("type", "string", "x-nori-type", colType, "format", "date",
			"description", "YYYY-MM-DD; \"\" when empty")
	case store.TypeDatetime:
		return obj("type", "string", "x-nori-type", colType, "format", "date-time",
			"description", "RFC3339 UTC; \"\" when empty")
	default:
		return obj("type", "string", "x-nori-type", store.TypeText)
	}
}

// inputSchema describes what a write accepts for one column: strings,
// and plain JSON numbers for number columns. Values are normalized and
// validated against the column type on write.
func inputSchema(colType string) map[string]any {
	switch colType {
	case store.TypeNumber:
		return obj("type", []string{"string", "number"}, "x-nori-type", colType,
			"description", "number; \"1,234.50\" is accepted and stored as \"1234.5\"; \"\" clears")
	case store.TypeDate:
		return obj("type", "string", "x-nori-type", colType,
			"description", "date, e.g. 2026-09-21 (slash dates follow Settings > Dates); \"\" clears")
	case store.TypeDatetime:
		return obj("type", "string", "x-nori-type", colType,
			"description", "date and time, e.g. 2026-09-21 14:30; stored as RFC3339 UTC; \"\" clears")
	default:
		return obj("type", "string", "x-nori-type", store.TypeText)
	}
}

func rowSchema(t store.TableInfo) map[string]any {
	props := obj("id", obj("type", "string", "description", "server-generated row id"))
	required := []string{"id"}
	for i, c := range t.Columns {
		props[c] = cellSchema(t.Types[i])
		required = append(required, c)
	}
	return obj("type", "object", "properties", props, "required", required)
}

// rowWriteSchema is the create body, or with patch the PATCH body, where a
// number column may also take {"inc": n}.
func rowWriteSchema(t store.TableInfo, patch bool) map[string]any {
	props := obj()
	for i, c := range t.Columns {
		s := inputSchema(t.Types[i])
		if patch && t.Types[i] == store.TypeNumber {
			s = obj("oneOf", []any{s, obj("type", "object",
				"properties", obj("inc", obj("type", []string{"number", "string"},
					"description", "amount to add atomically; negative to subtract; an empty cell counts as 0")),
				"required", []string{"inc"},
				"additionalProperties", false)})
		}
		props[c] = s
	}
	description := "column values; columns left out stay empty"
	if patch {
		description = "only the columns to change"
	}
	return obj("type", "object", "description", description, "properties", props, "additionalProperties", false)
}

func listParams(t store.TableInfo) []any {
	params := []any{
		obj("name", "limit", "in", "query", "schema", obj("type", "integer", "default", 50, "maximum", 1000), "description", "page size"),
		obj("name", "offset", "in", "query", "schema", obj("type", "integer", "default", 0)),
		queryParam("q", "case-insensitive contains-match across all columns"),
		obj("name", "sort", "in", "query", "schema", sortSchema(t.Columns), "description", "column to sort by"),
		obj("name", "order", "in", "query", "schema", obj("type", "string", "enum", []string{"asc", "desc"}, "default", "asc")),
	}
	for i, c := range t.Columns {
		if reservedListParams[c] {
			continue
		}
		params = append(params, obj(
			"name", c, "in", "query", "style", "form", "explode", true,
			"schema", obj("type", "array", "items", obj("type", "string")),
			"description", filterDescription(t.Types[i]),
		))
	}
	return params
}

func sortSchema(columns []string) map[string]any {
	if len(columns) == 0 {
		return obj("type", "string")
	}
	return obj("type", "string", "enum", columns)
}

func filterDescription(colType string) string {
	const shared = " Repeat the parameter and every value must match. col:<column> as the operand compares with another column of the same row and type."
	switch colType {
	case store.TypeNumber, store.TypeDate, store.TypeDatetime:
		return "filter (" + colType + "): a plain value is a case-insensitive contains-match; eq:/ne: exact; lt:/lte:/gt:/gte: compare as " + colType + " (e.g. lt:5); contains: spelled out." + shared
	default:
		return "filter (text): a plain value is a case-insensitive contains-match; eq:/ne: exact, case-insensitive; contains: spelled out." + shared
	}
}
