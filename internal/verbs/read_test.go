package verbs

import (
	"testing"

	"github.com/promptctl/lit2/internal/field"
)

// TestEveryDerivedFieldHasADerivation: Read panics on a derived or a
// governed row with no reading, so the table and the readings agree
// before any run.
func TestEveryDerivedFieldHasADerivation(t *testing.T) {
	for path, d := range field.Builtins(field.Enum{}) {
		if _, ok := derivations[path]; (d.Writability == field.Derived) != ok {
			t.Errorf("%s: derived %v, derivation %v", path, d.Writability == field.Derived, ok)
		}
		if _, ok := governedReads[path]; (d.Writability == field.Governed) != ok {
			t.Errorf("%s: governed %v, reading %v", path, d.Writability == field.Governed, ok)
		}
	}
}
