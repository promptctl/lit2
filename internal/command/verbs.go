package command

// VerbsFacts is what a verb's body did, which the root alone records
// `[LAW:nothing-unseen]`: each operand in order, the drafts by kind, how
// many deleted a node, whether it was a dry run, the events it minted,
// the field table it read by origin, and the requirements its arrivals
// were judged by. A value's text is never in it.
type VerbsFacts struct {
	Operands     []OperandFacts
	Drafts       int
	Kinds        map[string]int
	Deletes      int
	DryRun       bool
	Minted       []string
	Rows         RowCounts
	Requirements RequirementCounts
}

// RequirementCounts is the requirements the transition gate evaluated,
// and how many were unmet, by severity: a write that lands with unmet
// warnings passed over an advisory.
type RequirementCounts struct{ Evaluated, Warnings, Errors int }

// OperandFacts is one operand: its path as typed, and either its
// canonical address and how many values it gave, or why it was refused,
// with the detail of an address refusal, which names only paths.
type OperandFacts struct {
	Text   string
	Canon  string
	Values int
	Reason string
	Detail string
}

// RowCounts is the field table's rows by origin.
type RowCounts struct{ Builtin, Declared, Checks int }

type (
	verbsLine struct {
		Operands []operandLine  `json:"operands"`
		Drafts   int            `json:"drafts"`
		Kinds    map[string]int `json:"kinds"`
		Deletes  int            `json:"deletes"`
		DryRun   bool           `json:"dry_run"`
		Minted   []string       `json:"minted"`
		Rows     rowsLine       `json:"rows"`
		Reqs     reqsLine       `json:"requirements"`
	}
	reqsLine struct {
		Evaluated int `json:"evaluated"`
		Warnings  int `json:"unmet_warning"`
		Errors    int `json:"unmet_error"`
	}
	operandLine struct {
		Text   string `json:"text"`
		Canon  string `json:"canonical,omitempty"`
		Values int    `json:"values"`
		Reason string `json:"reason,omitempty"`
		Detail string `json:"detail,omitempty"`
	}
	rowsLine struct {
		Builtin  int `json:"builtin"`
		Declared int `json:"declared"`
		Checks   int `json:"checks"`
	}
)

// verbsBlock is the facts as the line's verbs block, absent when no verb
// body ran. Every count is written, zeros included.
func verbsBlock(v *VerbsFacts) *verbsLine {
	if v == nil {
		return nil
	}
	l := &verbsLine{Operands: []operandLine{}, Drafts: v.Drafts, Kinds: map[string]int{}, Deletes: v.Deletes, DryRun: v.DryRun, Minted: append([]string{}, v.Minted...),
		Rows: rowsLine{Builtin: v.Rows.Builtin, Declared: v.Rows.Declared, Checks: v.Rows.Checks}, Reqs: reqsLine(v.Requirements)}
	for k, n := range v.Kinds {
		l.Kinds[k] = n
	}
	for _, o := range v.Operands {
		l.Operands = append(l.Operands, operandLine(o))
	}
	return l
}
