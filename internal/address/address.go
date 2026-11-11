// Package address is what can be named: the path grammar, selectors, globs
// and the field table.
//
// Its layer in architecture.md §6: path grammar; selectors; globs; the field
// table (§3.1, §5 step 4). A path parses once into a typed address and every
// verb takes the address `[LAW:parse-dont-validate]`. Here it holds the field
// table as --help reads it: if a field is not in the table it does not
// exist.
package address

import (
	"fmt"
	"slices"
)

// FieldTable is the schema of the surface, one row per field, in the order
// --help prints it.
type FieldTable struct{ rows []FieldRow }

// FieldRow is one field as --help prints it.
type FieldRow struct {
	Name string
	Help string
}

// NewFieldTable makes a table of rows, refusing a name held twice, since a
// field is its name.
func NewFieldTable(rows ...FieldRow) (FieldTable, error) {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if seen[r.Name] {
			return FieldTable{}, fmt.Errorf("field %q is in the table twice", r.Name)
		}
		seen[r.Name] = true
	}
	return FieldTable{rows: slices.Clone(rows)}, nil
}

// Rows is the table's rows in order.
func (t FieldTable) Rows() []FieldRow { return slices.Clone(t.rows) }
