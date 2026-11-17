package config

import (
	"fmt"
	"strings"
)

// KeyPath is where in a workspace file a fault is: `levels.2.home`,
// `columns.impl.entry[0].test`, `nodes.unfiled.columns.audit.next`.
type KeyPath string

// Reason is one kind of fault. Its token is what a refusal prints and what
// the command event records, so a token is never reworded.
type Reason string

const (
	// The file itself.
	ReasonSyntax     Reason = "syntax"
	ReasonUnknownKey Reason = "unknown-key"
	ReasonMissingKey Reason = "missing-key"
	ReasonWrongType  Reason = "wrong-type"

	// The version's gate and the prefix.
	ReasonReaderTooOld Reason = "reader-too-old"
	ReasonBadVersion   Reason = "bad-version"
	ReasonBadPrefix    Reason = "bad-prefix"

	// Levels.
	ReasonNoLevels            Reason = "no-levels"
	ReasonLevelOutOfRange     Reason = "level-out-of-range"
	ReasonLevelsNotContiguous Reason = "levels-not-contiguous"
	ReasonEmptyLevelName      Reason = "empty-level-name"
	ReasonDuplicateLevelName  Reason = "duplicate-level-name"
	ReasonHomeOnLeaf          Reason = "home-on-leaf"
	ReasonMissingHome         Reason = "missing-home"
	ReasonBadHome             Reason = "bad-home"
	ReasonDuplicateHome       Reason = "duplicate-home"
	ReasonHomeCollides        Reason = "home-collides"

	// Fields.
	ReasonBadFieldName      Reason = "bad-field-name"
	ReasonUnknownKind       Reason = "unknown-kind"
	ReasonKindParameter     Reason = "kind-parameter"
	ReasonBuiltinRedeclared Reason = "builtin-redeclared"

	// Labels.
	ReasonBadLabel       Reason = "bad-label"
	ReasonDuplicateLabel Reason = "duplicate-label"

	// Column aliases.
	ReasonAliasCollides  Reason = "alias-collides"
	ReasonDuplicateAlias Reason = "duplicate-alias"

	// Templates.
	ReasonTemplateFieldUndeclared Reason = "template-field-undeclared"
	ReasonTemplateFieldDerived    Reason = "template-field-derived"
	ReasonTemplateValue           Reason = "template-value"

	// Columns.
	ReasonBadColumnName  Reason = "bad-column-name"
	ReasonInitialAndTerm Reason = "initial-and-terminal"
	ReasonExitOnTerminal Reason = "exit-on-terminal"
	ReasonNextOnTerminal Reason = "next-on-terminal"
	ReasonNextUndeclared Reason = "next-target-undeclared"
	ReasonNoInitial      Reason = "no-initial"
	ReasonManyInitial    Reason = "many-initial"
	ReasonNoTerminal     Reason = "no-terminal"

	// Requirements and checks.
	ReasonBadAtom         Reason = "bad-atom"
	ReasonPathUndeclared  Reason = "path-undeclared"
	ReasonPathIsCheck     Reason = "path-is-check"
	ReasonAtomValue       Reason = "atom-value"
	ReasonBadScope        Reason = "bad-scope"
	ReasonBadSeverity     Reason = "bad-severity"
	ReasonTestOrCheck     Reason = "test-or-check"
	ReasonBadCheckName    Reason = "bad-check-name"
	ReasonCheckUndeclared Reason = "check-undeclared"
	ReasonCheckCycle      Reason = "check-cycle"
	ReasonCheckCollides   Reason = "check-collides"

	// Links.
	ReasonBadLinkName         Reason = "bad-link-name"
	ReasonBadLinkKind         Reason = "bad-link-kind"
	ReasonEmptyBetween        Reason = "empty-between"
	ReasonBadPair             Reason = "bad-pair"
	ReasonPairLevelUndeclared Reason = "pair-level-undeclared"

	// Durations and sections.
	ReasonBadDuration Reason = "bad-duration"
	ReasonBadNodeKey  Reason = "bad-node-key"
)

// Refusal is one fault: where it is, why, and what a person reads to fix it.
type Refusal struct {
	Key     KeyPath
	Reason  Reason
	Message string
}

func (r Refusal) String() string {
	if r.Key == "" {
		return fmt.Sprintf("%s (%s)", r.Message, r.Reason)
	}
	return fmt.Sprintf("%s: %s (%s)", r.Key, r.Message, r.Reason)
}

// Refused is one version refused. It names every fault in it, never only the
// first, so one run of `config commit` tells a person everything to fix.
type Refused struct {
	Source   string
	Refusals []Refusal
}

func (e *Refused) Error() string {
	lines := make([]string, 0, len(e.Refusals))
	for _, r := range e.Refusals {
		lines = append(lines, "  "+r.String())
	}
	return fmt.Sprintf("%s refused, %d fault(s):\n%s", e.Source, len(e.Refusals), strings.Join(lines, "\n"))
}

// keyed is the key path of a child: a segment of a table, or an index into a
// list.
func keyed(base KeyPath, seg string) KeyPath {
	if base == "" {
		return KeyPath(seg)
	}
	return base + "." + KeyPath(seg)
}

func indexed(base KeyPath, i int) KeyPath {
	return base + KeyPath(fmt.Sprintf("[%d]", i))
}
