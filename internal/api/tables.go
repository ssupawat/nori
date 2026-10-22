package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"nori/internal/sheetimport"
	"nori/internal/store"
)

func (sv *Server) createTable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string         `json:"name"`
		Columns []store.Column `json:"columns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, `body must be {"name": "...", "columns": [{"name": "...", "type": "text|number|date|datetime"}]}`)
		return
	}
	if len(body.Columns) == 0 {
		writeError(w, http.StatusBadRequest, "at least one column is required")
		return
	}
	if err := sv.store.CreateTable(body.Name, body.Columns); mapStoreErr(w, err) {
		return
	}
	colDesc := make([]string, len(body.Columns))
	for i, c := range body.Columns {
		colDesc[i] = c.Name + ":" + c.Type
	}
	sv.auditEvent(r, "create_table", body.Name, "", map[string]string{"columns": strings.Join(colDesc, ",")})
	cols, err := sv.store.GetColumnsMeta(body.Name)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name, "columns": cols})
}

func (sv *Server) listTables(w http.ResponseWriter, r *http.Request) {
	tables, err := sv.store.ListTables()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tables)
}

func (sv *Server) checkTable(w http.ResponseWriter, name string) bool {
	exists, err := sv.store.TableExists(name)
	if err != nil {
		serverError(w, err)
		return false
	}
	if !exists {
		writeError(w, http.StatusNotFound, fmt.Sprintf("table %q not found", name))
		return false
	}
	return true
}
func (sv *Server) addColumn(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	var body struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, `body must be {"name": "column name", "type": "text|number|date|datetime"}`)
		return
	}
	if err := sv.store.AddColumn(table, body.Name, body.Type); mapStoreErr(w, err) {
		return
	}
	colType := body.Type
	if colType == "" {
		colType = store.TypeText
	}
	sv.auditEvent(r, "add_column", table, "", map[string]string{"column": strings.TrimSpace(body.Name), "type": colType})
	cols, err := sv.store.GetColumnsMeta(table)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"columns": cols})
}

func (sv *Server) deleteTable(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	err := sv.store.DeleteTable(table)
	if mapStoreErr(w, err) {
		return
	}
	sv.auditEvent(r, "delete_table", table, "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// renameBody is the shared shape for rename requests.
func renameBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, `body must be {"name": "new name"}`)
		return "", false
	}
	return strings.TrimSpace(body.Name), true
}

func (sv *Server) renameTable(w http.ResponseWriter, r *http.Request) {
	oldName := chi.URLParam(r, "t")
	newName, ok := renameBody(w, r)
	if !ok {
		return
	}
	if err := sv.store.RenameTable(oldName, newName); mapStoreErr(w, err) {
		return
	}
	sv.auditEvent(r, "rename_table", newName, "", map[string]string{"from": oldName})
	cols, err := sv.store.GetColumnsMeta(newName)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": newName, "columns": cols})
}

func (sv *Server) renameColumn(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	oldCol := chi.URLParam(r, "col")
	var body struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Convert bool   `json:"convert"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"name": "...", "type": "text|number|date|datetime"}`)
		return
	}
	newCol := strings.TrimSpace(body.Name)
	if newCol != "" && newCol != oldCol {
		if err := sv.store.RenameColumn(table, oldCol, newCol); mapStoreErr(w, err) {
			return
		}
		sv.auditEvent(r, "rename_column", table, "", map[string]string{"from": oldCol, "to": newCol})
		oldCol = newCol
	}
	if body.Type != "" {
		if err := sv.store.SetColumnType(table, oldCol, body.Type, body.Convert); mapStoreErr(w, err) {
			return
		}
		sv.auditEvent(r, "set_column_type", table, "", map[string]string{"column": oldCol, "type": body.Type, "convert": strconv.FormatBool(body.Convert)})
	}
	cols, err := sv.store.GetColumnsMeta(table)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"columns": cols})
}

func (sv *Server) deleteColumn(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	col := chi.URLParam(r, "col")
	if err := sv.store.DeleteColumn(table, col); mapStoreErr(w, err) {
		return
	}
	sv.auditEvent(r, "delete_column", table, "", map[string]string{"column": col})
	cols, err := sv.store.GetColumnsMeta(table)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"columns": cols})
}

func (sv *Server) exportTable(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "t")
	if !sv.checkTable(w, table) {
		return
	}
	columns, err := sv.store.GetColumns(table)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.xlsx"`, table))
	pages := func(yield func([][]string) error) error {
		// 1000 == store's max page size; paged so a full-table export
		// never holds the whole table in memory.
		const pageSize = 1000
		for offset := 0; ; offset += pageSize {
			rows, _, err := sv.store.GetRows(table, store.RowQuery{Limit: pageSize, Offset: offset})
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				return nil
			}
			page := make([][]string, len(rows))
			for i, row := range rows {
				cells := make([]string, len(columns))
				for j, c := range columns {
					cells[j] = row[c]
				}
				page[i] = cells
			}
			if err := yield(page); err != nil {
				return err
			}
			if len(rows) < pageSize {
				return nil
			}
		}
	}
	if err := sheetimport.WriteStreaming(w, table, columns, pages); err != nil {
		serverError(w, err)
		return
	}
}
