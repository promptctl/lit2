package command

import (
	"github.com/promptctl/lit2/internal/fold"
)

// The fold block as the line spells it, one object per fold that ran:
// what it read, checked and applied, and the
// state it left, every count present and zero when zero.
type (
	foldLine struct {
		Records    int            `json:"records"`
		Torn       int            `json:"torn"`
		Duplicates int            `json:"duplicates"`
		Decoded    decodedLine    `json:"decoded"`
		Checked    int            `json:"checked"`
		Verdicts   verdictsLine   `json:"verdicts"`
		Applied    int            `json:"applied"`
		Carried    map[string]int `json:"carried"`
		Unapplied  map[string]int `json:"unapplied"`
		Counts     stateLine      `json:"counts"`
	}
	decodedLine struct {
		Readable    int            `json:"readable"`
		Unread      map[string]int `json:"unread"`
		Erased      int            `json:"erased"`
		Uncommitted int            `json:"uncommitted"`
		Malformed   int            `json:"malformed"`
	}
	verdictsLine struct {
		Valid        int `json:"valid"`
		Invalid      int `json:"invalid"`
		UnknownSuite int `json:"unknown_suite"`
	}
	stateLine struct {
		Items          int `json:"items"`
		Comments       int `json:"comments"`
		Members        int `json:"members"`
		ProseConflicts int `json:"prose_conflicts"`
		Overruled      int `json:"overruled"`
		MixedForms     int `json:"mixed_forms"`
		Collisions     int `json:"collisions"`
		Uncreated      int `json:"uncreated"`
		Versions       int `json:"versions"`
		Contending     int `json:"contending"`
	}
)

func foldBlock(fs []fold.FoldFacts) []foldLine {
	var out []foldLine
	for _, f := range fs {
		l := foldLine{
			Records: f.Records, Torn: f.Torn, Duplicates: f.Duplicates,
			Decoded:  decodedLine{Readable: f.Decoded.Readable, Unread: f.Decoded.Unread, Erased: f.Decoded.Erased, Uncommitted: f.Decoded.Uncommitted, Malformed: f.Decoded.Malformed},
			Checked:  f.Checked,
			Verdicts: verdictsLine{Valid: f.Verdicts.Valid, Invalid: f.Verdicts.Invalid, UnknownSuite: f.Verdicts.UnknownSuite},
			Applied:  f.Applied, Carried: f.Carried, Unapplied: f.Unapplied,
			Counts: stateLine{
				Items: f.Counts.Items, Comments: f.Counts.Comments, Members: f.Counts.Members,
				ProseConflicts: f.Counts.ProseConflicts, Overruled: f.Counts.Overruled, MixedForms: f.Counts.MixedForms,
				Collisions: f.Counts.Collisions, Uncreated: f.Counts.Uncreated,
				Versions: f.Counts.Versions, Contending: f.Counts.Contending,
			},
		}
		out = append(out, l)
	}
	return out
}
