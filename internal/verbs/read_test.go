package verbs

import (
	"testing"

	"github.com/promptctl/lit2/internal/field"
)

// TestEveryDerivedFieldHasADerivation: Read panics on a derived row with
// no reading, so the table and the derivations agree before any run.
func TestEveryDerivedFieldHasADerivation(t *testing.T) {
	for path, d := range field.Builtins(field.Enum{}) {
		if _, ok := derivations[path]; (d.Writability == field.Derived) != ok {
			t.Errorf("%s: derived %v, derivation %v", path, d.Writability == field.Derived, ok)
		}
	}
}
