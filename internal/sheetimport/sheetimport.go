package sheetimport

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"nori/internal/store"
)

// Table is one Excel sheet ready to be stored.
type Table struct {
	Name    string
	Columns []string
	Rows    []map[string]string
}

// Decompression caps: an .xlsx is a zip of XML, so a small upload can
// expand into gigabytes in memory (a zip bomb). Package vars so tests
// can shrink them; the sizing rationale is in ARCHITECTURE.md.
var (
	unzipSizeLimit    int64 = 256 << 20 // all entries combined, after decompression
	unzipXMLSizeLimit int64 = 64 << 20  // worksheet and shared-strings XML
)

// Parse reads an .xlsx file and converts every sheet into a Table.
// The first row of each sheet supplies the column names.
func Parse(r io.Reader) ([]Table, error) {
	f, err := excelize.OpenReader(r, excelize.Options{
		UnzipSizeLimit:    unzipSizeLimit,
		UnzipXMLSizeLimit: unzipXMLSizeLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	tables := make([]Table, 0)
	for _, sheet := range f.GetSheetList() {
		grid, err := f.GetRows(sheet, excelize.Options{RawCellValue: false})
		if err != nil {
			return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		table, ok := sheetToTable(sheet, grid)
		if ok {
			tables = append(tables, table)
		}
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no sheets with data found")
	}
	return tables, nil
}

func sheetToTable(sheet string, grid [][]string) (Table, bool) {
	if len(grid) == 0 {
		return Table{}, false
	}
	columns := headerRow(grid[0])
	if len(columns) == 0 {
		return Table{}, false
	}

	rows := make([]map[string]string, 0)
	for _, line := range grid[1:] {
		row := make(map[string]string, len(columns))
		empty := true
		for i, col := range columns {
			value := ""
			if i < len(line) {
				value = strings.TrimSpace(line[i])
			}
			row[col] = value
			if value != "" {
				empty = false
			}
		}
		if !empty {
			rows = append(rows, row)
		}
	}
	return Table{Name: sanitize(sheet), Columns: columns, Rows: rows}, true
}

func headerRow(cells []string) []string {
	columns := make([]string, 0, len(cells))
	used := map[string]bool{}
	for i, cell := range cells {
		name := strings.TrimSpace(cell)
		if name == "" {
			name = fmt.Sprintf("Column%d", i+1)
		}
		if store.ReservedColName(name) {
			// storage-internal keys ("id", "seq", "_id") are kept under the
			// dedup-suffixed form instead
			name = name + "_"
		}
		for used[name] {
			name = name + "_"
		}
		used[name] = true
		columns = append(columns, name)
	}
	return columns
}

// sanitize makes a sheet name safe as a table name: reserved "_"-prefixed
// system names and empties are renamed, control chars and slashes removed.
func sanitize(name string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '[', ']':
			return '-'
		}
		if r < 32 {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	clean = strings.TrimLeft(clean, "_")
	if clean == "" {
		clean = "Sheet"
	}
	return clean
}

// Write renders one table as an .xlsx file.
func Write(w io.Writer, sheetName string, columns []string, rows [][]string) error {
	f := excelize.NewFile()
	defer f.Close()

	const defaultSheet = "Sheet1"
	sheet := sanitize(sheetName)
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	if err := f.DeleteSheet(defaultSheet); err != nil {
		return err
	}
	for i, col := range columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, col); err != nil {
			return err
		}
	}
	for r, row := range rows {
		for c, value := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			if err := f.SetCellValue(sheet, cell, value); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
