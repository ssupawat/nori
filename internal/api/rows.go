package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"nori/internal/store"
)

func (sv *Server) listRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}

	rq := store.RowQuery{
		Q:     r.URL.Query().Get("q"),
		Sort:  r.URL.Query().Get("sort"),
		Order: r.URL.Query().Get("order"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		rq.Limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		rq.Offset = n
	}

	cols, err := sv.store.GetColumnsMeta(table)
	if err != nil {
		serverError(w, err)
		return
	}
	columns := make([]string, len(cols))
	types := make([]string, len(cols))
	for i, c := range cols {
		columns[i] = c.Name
		types[i] = c.Type
	}
	filters := map[string]string{}
	for col, values := range r.URL.Query() {
		if col == "limit" || col == "offset" || col == "q" || col == "sort" || col == "order" {
			continue
		}
		if !slices.Contains(columns, col) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown filter column %q", col))
			return
		}
		for _, v := range values {
			if op, val, ok := store.ParseFilterOp(v); ok {
				rq.Conds = append(rq.Conds, store.Cond{Col: col, Op: op, Val: val})
			} else if _, seen := filters[col]; !seen {
				filters[col] = v
			} else {
				// a repeated plain value must match too
				rq.Conds = append(rq.Conds, store.Cond{Col: col, Op: store.OpContains, Val: v})
			}
		}
	}
	rq.Filters = filters

	rows, total, err := sv.store.GetRows(table, rq)
	if mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"columns": columns,
		"types":   types,
		"rows":    rows,
		"total":   total,
		"limit":   limitOrDefault(rq.Limit),
		"offset":  rq.Offset,
	})
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func (sv *Server) getRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	row, err := sv.store.GetRow(table, chi.URLParam(r, "id"))
	if mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// rowValues converts the JSON body into string values, rejecting unknown
// columns.
func (sv *Server) rowValues(w http.ResponseWriter, r *http.Request, table string) (map[string]string, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, false
	}
	columns, err := sv.store.GetColumns(table)
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	values := make(map[string]string, len(body))
	for k, v := range body {
		if k == "id" {
			continue
		}
		if !slices.Contains(columns, k) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown column %q; valid columns: %s", k, strings.Join(columns, ", ")))
			return nil, false
		}
		values[k] = stringify(v)
	}
	return values, true
}

// rowUpdate is rowValues for PATCH: a column may also be an {"inc": n}
// object, which adds n to the stored number instead of replacing it.
func (sv *Server) rowUpdate(w http.ResponseWriter, r *http.Request, table string) (values, incs map[string]string, ok bool) {
	var body map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, nil, false
	}
	columns, err := sv.store.GetColumns(table)
	if err != nil {
		serverError(w, err)
		return nil, nil, false
	}
	values = make(map[string]string, len(body))
	incs = map[string]string{}
	for k, v := range body {
		if k == "id" {
			continue
		}
		if !slices.Contains(columns, k) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown column %q; valid columns: %s", k, strings.Join(columns, ", ")))
			return nil, nil, false
		}
		if obj, isObj := v.(map[string]any); isObj {
			n, valid := incOperand(obj)
			if !valid {
				writeError(w, http.StatusBadRequest, fmt.Sprintf(`column %q: the only supported object is {"inc": <number>}`, k))
				return nil, nil, false
			}
			incs[k] = n
			continue
		}
		if n, isNum := v.(json.Number); isNum {
			f, _ := n.Float64() // plain values render as float64
			v = f
		}
		values[k] = stringify(v)
	}
	return values, incs, true
}

// incOperand reads the number from {"inc": n}; n may be a JSON number or a
// numeric string.
func incOperand(obj map[string]any) (string, bool) {
	if len(obj) != 1 {
		return "", false
	}
	switch n := obj["inc"].(type) {
	case json.Number:
		return n.String(), true
	case string:
		return n, n != ""
	}
	return "", false
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func (sv *Server) createRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	values, ok := sv.rowValues(w, r, table)
	if !ok {
		return
	}
	row, err := sv.store.InsertRow(table, values)
	if mapStoreErr(w, err) {
		return
	}
	sv.auditEvent(r, "create_row", table, row["id"], rowValuesOnly(row))
	writeJSON(w, http.StatusCreated, row)
}

func (sv *Server) updateRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	values, incs, ok := sv.rowUpdate(w, r, table)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	row, old, err := sv.store.UpdateRowOps(table, id, values, incs)
	if mapStoreErr(w, err) {
		return
	}
	changes := map[string]string{}
	for k, v := range values {
		if old == nil || old[k] != v {
			changes[k] = v
		}
	}
	for k := range incs {
		if old == nil || old[k] != row[k] {
			changes[k] = row[k] // the resulting value
		}
	}
	sv.auditEvent(r, "update_row", table, id, changes)
	writeJSON(w, http.StatusOK, row)
}

func (sv *Server) deleteRow(w http.ResponseWriter, r *http.Request) {
	table, id := chi.URLParam(r, "t"), chi.URLParam(r, "id")
	old, _ := sv.store.GetRow(table, id) // captured for the audit trail before it is gone
	err := sv.store.DeleteRow(table, id)
	if mapStoreErr(w, err) {
		return
	}
	if old != nil {
		sv.auditEvent(r, "delete_row", table, id, rowValuesOnly(old))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
