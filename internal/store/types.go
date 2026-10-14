package store

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Column types are hints over the string storage: text values are stored
// verbatim, numbers and dates are normalized to canonical strings so that
// sorting and validation behave. datetime values are canonical UTC
// (RFC3339 with the Z suffix) — the backend never touches any other zone;
// clients convert to local for display.
const (
	TypeText     = "text"
	TypeNumber   = "number"
	TypeDate     = "date"
	TypeDatetime = "datetime"
)

// Date orders for ambiguous slash dates (3/4/2026): which side is the day.
const (
	OrderDMY = "dmy"
	OrderMDY = "mdy"
)

type Column struct {
	// clover tags matter most: clover converts struct fields to its own map
	// keys before msgpack encoding. json tags keep the API shape stable.
	Name string `json:"name" clover:"name"`
	Type string `json:"type" clover:"type"`
}

// Unambiguous layouts — day and month are never both small.
var fixedDateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006-01-02 15:04:05",
}

var datetimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
}

// Slash layouts depend on the date order. The padded variant comes first;
// the unpadded variant accepts one- or two-digit input. Both sides of the
// pair interpret the same order, so "3/4/2026" and "03/04/2026" agree.
var slashDateLayouts = map[string][]string{
	OrderDMY: {"02/01/2006", "2/1/2006"},
	OrderMDY: {"01/02/2006", "1/2/2006"},
}

var slashDatetimeLayouts = map[string][]string{
	OrderDMY: {"02/01/2006 15:04", "2/1/2006 15:04"},
	OrderMDY: {"01/02/2006 15:04", "1/2/2006 15:04"},
}

// parseInOrder parses raw as a calendar date under the given date order.
func parseInOrder(raw, order string) (time.Time, bool) {
	for _, layout := range fixedDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	for _, layout := range slashDateLayouts[order] {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// maybeBuddhist folds a parsed Buddhist Era year back to CE when the
// setting is on. BE years as written today (2560s) far exceed any
// realistic CE year, so the 2400 threshold is unambiguous in practice.
func (s *Store) maybeBuddhist(t time.Time) time.Time {
	if s.BuddhistYears() && t.Year() >= 2400 {
		return t.AddDate(-543, 0, 0)
	}
	return t
}

// NormalizeColumn converts raw input into the canonical string form for
// the column type. Empty stays empty. Numbers become their shortest float
// representation ("1,234.50" -> "1234.5"); dates become YYYY-MM-DD;
// datetimes become RFC3339 UTC (offsets convert, naive input is UTC).
func (s *Store) NormalizeColumn(colType, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	switch colType {
	case TypeNumber:
		cleaned := strings.ReplaceAll(v, ",", "")
		// values with a leading zero ("0812...", "007") are codes and
		// phone numbers, not numbers — keep them as text
		if s := strings.TrimLeft(cleaned, "+-"); len(s) > 1 && s[0] == '0' && s[1] >= '0' && s[1] <= '9' {
			return "", fmt.Errorf("%q looks like a code (leading zero), not a number", raw)
		}
		f, err := strconv.ParseFloat(cleaned, 64)
		if err != nil {
			return "", fmt.Errorf("%q is not a number", raw)
		}
		// strconv accepts "NaN" and "Inf" — those are not cell values
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("%q is not a finite number", raw)
		}
		// whole numbers at 16+ digits no longer round-trip a float64
		// exactly — keep them as text instead of corrupting the value
		if f == math.Trunc(f) && math.Abs(f) >= 1e15 {
			return "", fmt.Errorf("%q has too many digits to be a number", raw)
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	case TypeDate:
		if t, ok := parseInOrder(v, s.DateOrder()); ok {
			return s.maybeBuddhist(t).Format("2006-01-02"), nil
		}
		return "", fmt.Errorf("%q is not a date (use YYYY-MM-DD)", raw)
	case TypeDatetime:
		for _, layout := range datetimeLayouts {
			if t, err := time.Parse(layout, v); err == nil {
				return s.maybeBuddhist(t).UTC().Format(time.RFC3339), nil
			}
		}
		for _, layout := range slashDatetimeLayouts[s.DateOrder()] {
			if t, err := time.Parse(layout, v); err == nil {
				return s.maybeBuddhist(t).UTC().Format(time.RFC3339), nil
			}
		}
		return "", fmt.Errorf("%q is not a datetime (use YYYY-MM-DD HH:MM, stored as UTC)", raw)
	default:
		return raw, nil
	}
}

// DetectColumns guesses a type for every column from its values: number
// when all non-empty values parse as numbers, datetime when all carry a
// time component, date when all match a known date layout, text otherwise.
func (s *Store) DetectColumns(names []string, rows []map[string]string) []Column {
	cols := make([]Column, len(names))
	guess := func(col string) string {
		isNum, isDatetime, isDate, seen := true, true, true, false
		for _, row := range rows {
			v := strings.TrimSpace(row[col])
			if v == "" {
				continue
			}
			seen = true
			if _, err := s.NormalizeColumn(TypeNumber, v); err != nil {
				isNum = false
			}
			if _, err := s.NormalizeColumn(TypeDatetime, v); err != nil {
				isDatetime = false
			}
			if _, err := s.NormalizeColumn(TypeDate, v); err != nil {
				isDate = false
			}
			if !isNum && !isDatetime && !isDate {
				return TypeText
			}
		}
		if !seen {
			return TypeText
		}
		switch {
		case isNum:
			return TypeNumber
		case isDatetime:
			return TypeDatetime
		case isDate:
			return TypeDate
		}
		return TypeText
	}
	for i, name := range names {
		cols[i] = Column{Name: name, Type: guess(name)}
	}
	return cols
}

func validType(t string) bool {
	return t == TypeText || t == TypeNumber || t == TypeDate || t == TypeDatetime
}

// Limits keep a table inside the zone where reads stay cached and fast:
// past MaxTableRows the decoded-row cache is skipped and every read pays a
// full-table decode. Imports are capped separately at the HTTP layer.
var (
	MaxTableRows = 200_000
	MaxTableCols = 256

	ErrTableFull   = errors.New("table is full")
	ErrTooManyCols = errors.New("too many columns")
)
