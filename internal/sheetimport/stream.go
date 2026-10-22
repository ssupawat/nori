package sheetimport

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// streamPageSize is how many rows one streamed page carries.
const streamPageSize = 1000

// TableStream is one sheet ready to be stored. Sample is the first page
// (for column type detection); Rows yields every data row, sample included.
type TableStream struct {
	Name    string
	Columns []string
	Sample  []map[string]string
	Rows    func(yield func([]map[string]string) error) error
}

// ParseStream reads every sheet from r and hands each table to handle,
// one at a time, without holding a whole sheet in memory. Sheets Parse
// skips (empty grid or header) are skipped here too.
func ParseStream(r io.Reader, handle func(TableStream) error) error {
	f, err := excelize.OpenReader(r, excelize.Options{
		UnzipSizeLimit:    unzipSizeLimit,
		UnzipXMLSizeLimit: unzipXMLSizeLimit,
	})
	if err != nil {
		return fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	for _, sheet := range f.GetSheetList() {
		columns, sample, err := sheetHead(f, sheet)
		if err != nil {
			return fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		if columns == nil {
			continue
		}
		ts := TableStream{Name: sanitize(sheet), Columns: columns, Sample: sample}
		ts.Rows = func(yield func([]map[string]string) error) error {
			return streamRows(f, sheet, columns, yield)
		}
		if err := handle(ts); err != nil {
			return err
		}
	}
	return nil
}

// sheetHead reads the header and the first page of data rows. columns ==
// nil means the sheet is skipped by Parse semantics (empty sheet or grid).
func sheetHead(f *excelize.File, sheet string) ([]string, []map[string]string, error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil, rows.Error()
	}
	cells, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	columns := headerRow(cells)
	if len(columns) == 0 {
		return nil, nil, nil
	}
	sample := make([]map[string]string, 0, streamPageSize)
	for len(sample) < streamPageSize && rows.Next() {
		cells, err := rows.Columns()
		if err != nil {
			return nil, nil, err
		}
		if row, keep := streamRowMap(columns, cells); keep {
			sample = append(sample, row)
		}
	}
	return columns, sample, rows.Error()
}

func streamRows(f *excelize.File, sheet string, columns []string, yield func([]map[string]string) error) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return err
	}
	defer rows.Close()
	first := true
	page := make([]map[string]string, 0, streamPageSize)
	flush := func() error {
		if len(page) == 0 {
			return nil
		}
		if err := yield(page); err != nil {
			return err
		}
		page = make([]map[string]string, 0, streamPageSize)
		return nil
	}
	for rows.Next() {
		cells, err := rows.Columns()
		if err != nil {
			return err
		}
		if first {
			first = false
			continue // header row: already consumed by sheetHead
		}
		if row, keep := streamRowMap(columns, cells); keep {
			page = append(page, row)
			if len(page) >= streamPageSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := rows.Error(); err != nil {
		return err
	}
	return flush()
}

// streamRowMap mirrors sheetToTable's per-row semantics: values are
// trimmed, missing cells become "", and fully-empty rows are dropped.
func streamRowMap(columns []string, cells []string) (map[string]string, bool) {
	row := make(map[string]string, len(columns))
	empty := true
	for i, col := range columns {
		value := ""
		if i < len(cells) {
			value = strings.TrimSpace(cells[i])
		}
		row[col] = value
		if value != "" {
			empty = false
		}
	}
	return row, !empty
}
