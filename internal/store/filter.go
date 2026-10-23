package store

import (
	"slices"
	"strconv"
	"strings"
)

// Filter operators. A column filter value such as "lt:5" is an operator
// filter; anything else is the plain case-insensitive contains-match.
const (
	OpEq       = "eq"
	OpNe       = "ne"
	OpLt       = "lt"
	OpLte      = "lte"
	OpGt       = "gt"
	OpGte      = "gte"
	OpContains = "contains"
)

var filterOps = []string{OpEq, OpNe, OpLt, OpLte, OpGt, OpGte, OpContains}

// ColRefPrefix marks an operand that names another column of the same
// row, so qty=lt:col:reorder_level compares the two cells per row.
const ColRefPrefix = "col:"

// Cond is one operator filter on a column.
type Cond struct {
	Col string
	Op  string
	Val string
}

// ParseFilterOp splits "op:value" into its operator and value. ok is false
// when raw does not start with a known operator.
func ParseFilterOp(raw string) (op, val string, ok bool) {
	name, rest, found := strings.Cut(raw, ":")
	if !found || !slices.Contains(filterOps, name) {
		return "", "", false
	}
	return name, rest, true
}

type compiledCond struct {
	col  string
	op   string
	want string  // lowered operand (eq, ne, contains) or canonical operand (date, datetime)
	num  float64 // parsed operand for number columns
	kind string  // column type the comparison runs in; empty = string match
	ref  string  // column whose cell is the operand, instead of want/num
}

// compileConds validates every condition against the table's columns and
// types.
func (s *Store) compileConds(conds []Cond, columns []string, types map[string]string) ([]compiledCond, error) {
	out := make([]compiledCond, 0, len(conds))
	for _, c := range conds {
		if !slices.Contains(columns, c.Col) {
			return nil, invalidf("unknown filter column %q", c.Col)
		}
		cc := compiledCond{col: c.Col, op: c.Op, want: strings.ToLower(c.Val)}
		ordered := c.Op == OpLt || c.Op == OpLte || c.Op == OpGt || c.Op == OpGte
		colType := types[c.Col]
		if ref, isRef := strings.CutPrefix(c.Val, ColRefPrefix); isRef && c.Op != OpContains {
			if err := checkColRef(c, ref, ordered, columns, types); err != nil {
				return nil, err
			}
			cc.ref = ref
			if colType == TypeNumber || colType == TypeDate || colType == TypeDatetime {
				cc.kind = colType
			}
			out = append(out, cc)
			continue
		}
		switch {
		case c.Op == OpContains:
		case c.Val == "" && !ordered:
			// eq: / ne: with nothing matches empty / non-empty cells
		case colType == TypeNumber || colType == TypeDate || colType == TypeDatetime:
			norm, err := s.NormalizeColumn(colType, c.Val)
			if err != nil {
				return nil, invalidf("filter %q on column %q: %v", c.Op+":"+c.Val, c.Col, err)
			}
			if norm == "" {
				return nil, invalidf("filter %q on column %q needs a value", c.Op+":", c.Col)
			}
			cc.kind = colType
			cc.want = norm
			if colType == TypeNumber {
				cc.num, _ = strconv.ParseFloat(norm, 64)
			}
		case ordered:
			return nil, invalidf("operator %q needs a number, date or datetime column; %q is %s", c.Op, c.Col, colType)
		}
		out = append(out, cc)
	}
	return out, nil
}

// checkColRef validates a col: operand: the column exists and holds the
// same type.
func checkColRef(c Cond, ref string, ordered bool, columns []string, types map[string]string) error {
	if !slices.Contains(columns, ref) {
		return invalidf("filter %q on column %q: unknown column %q", c.Op+":"+c.Val, c.Col, ref)
	}
	left, right := orText(types[c.Col]), orText(types[ref])
	if left != right {
		return invalidf("filter %q: cannot compare %q (%s) with %q (%s)", c.Op+":"+c.Val, c.Col, left, ref, right)
	}
	if ordered && left == TypeText {
		return invalidf("operator %q needs a number, date or datetime column; %q is %s", c.Op, c.Col, left)
	}
	return nil
}

func orText(t string) string {
	if t == "" {
		return TypeText
	}
	return t
}

func matchConds(e rowDoc, conds []compiledCond) bool {
	for _, c := range conds {
		if !matchCond(e, c) {
			return false
		}
	}
	return true
}

// matchCond compares a stored cell with the operand. Cells that do not
// parse in the column's type (empty, or text left over from before a type
// change) fail every comparison except ne; with a col: operand that holds
// for the cell on either side.
func matchCond(e rowDoc, c compiledCond) bool {
	if c.op == OpContains {
		return strings.Contains(e.lrow[c.col], c.want)
	}
	cmp, ok := compareCell(e, c)
	if !ok {
		return c.op == OpNe
	}
	switch c.op {
	case OpEq:
		return cmp == 0
	case OpNe:
		return cmp != 0
	case OpLt:
		return cmp < 0
	case OpLte:
		return cmp <= 0
	case OpGt:
		return cmp > 0
	case OpGte:
		return cmp >= 0
	}
	return false
}

// compareCell orders the row's cell against the operand: a literal, or
// with c.ref the same row's cell in that column.
func compareCell(e rowDoc, c compiledCond) (int, bool) {
	switch c.kind {
	case TypeNumber:
		f, err := strconv.ParseFloat(e.row[c.col], 64)
		if err != nil {
			return 0, false
		}
		want := c.num
		if c.ref != "" {
			if want, err = strconv.ParseFloat(e.row[c.ref], 64); err != nil {
				return 0, false
			}
		}
		switch {
		case f < want:
			return -1, true
		case f > want:
			return 1, true
		}
		return 0, true
	case TypeDate, TypeDatetime:
		v, want := e.row[c.col], c.want
		if c.ref != "" {
			want = e.row[c.ref]
		}
		if v == "" || want == "" {
			return 0, false
		}
		return strings.Compare(v, want), true
	}
	want := c.want
	if c.ref != "" {
		want = e.lrow[c.ref]
	}
	if e.lrow[c.col] != want {
		return 1, true
	}
	return 0, true
}
