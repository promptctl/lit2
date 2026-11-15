package fold

import (
	"fmt"
	"slices"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

// Env is what every fold runs with, built once at the root by NewEnv: the
// registry, the suites signatures are judged by, and the goroutines that
// judge them.
type Env struct {
	registry Registry
	suites   events.Suites
	workers  int
}

// NewEnv is the fold's environment, refusing one that verifies on no
// goroutine `[LAW:parse-dont-validate]`, so a fold never meets one.
func NewEnv(r Registry, s events.Suites, workers int) (Env, error) {
	if workers < 1 {
		return Env{}, fmt.Errorf("a fold verifies on %d goroutines; it needs at least one", workers)
	}
	return Env{registry: r, suites: s, workers: workers}, nil
}

// Registry is the kinds the fold decodes and folds.
func (e Env) Registry() Registry { return e.registry }

// Suites is the suites the fold judges signatures by.
func (e Env) Suites() events.Suites { return e.suites }

// Workers is the goroutines that judge them.
func (e Env) Workers() int { return e.workers }

// Scanned is one range of a stream and what reading it found.
type Scanned struct {
	Range store.Range
	Scan  store.Scan
}

// Folded is a fold's state and the fold's facts for the command event.
type Folded struct {
	State State
	Facts FoldFacts
}

// Replay folds scans, every stream from 0, from the zero State: the one
// walk, which decodes every record, keeps one of each event, sorts by the
// total order, checks every signature, and steps each in order onto s.
func Replay(e Env, scans []Scanned) Folded {
	facts := newFacts()
	s := State{}
	kinds := e.registry.Kinds()
	var located []events.Event
	seen := map[events.EventID]int{}
	for _, sc := range scans {
		facts.Torn += len(sc.Scan.Torn)
		ref := sc.Range.Stream.Ref
		for _, fr := range sc.Scan.Frames {
			for _, rec := range fr.Batch.Records() {
				facts.Records++
				switch d := events.Decode(kinds, rec).(type) {
				case events.Event:
					facts.Decoded.count(d)
					i, dup := seen[d.ID()]
					switch {
					case !dup:
						seen[d.ID()] = len(located)
						located = append(located, d)
					case rank(d.Body()) > rank(located[i].Body()):
						facts.Duplicates++
						located[i] = d
					default:
						facts.Duplicates++
					}
				case events.Malformed:
					facts.Decoded.Malformed++
					s = s.malformed(Malformed{Ref: string(ref.Name()), At: fr.At, Digest: d.Digest, Why: d.Why})
				}
			}
		}
	}
	slices.SortFunc(located, func(a, b events.Event) int { return events.Compare(a.Order(), b.Order()) })
	checked := Check(e, located)
	facts.Checked = len(checked)
	for _, c := range checked {
		facts.Verdicts.count(c.verdict)
		var out outcome
		s, out = e.registry.step(s, c)
		facts.count(out)
	}
	facts.Counts = countsOf(s)
	return Folded{State: s, Facts: facts}
}

// rank orders the bodies the copies of one event can carry, by how much of
// the signed payload each keeps: the payload read; the payload committed
// but unread by this binary; a body that is not the one signed; none. The
// Replay keeps the highest, so which copy stands does not depend on which
// stream arrived first. Two copies of one rank stand alike: a committed
// payload is the one the header hashes, and the other two carry nothing
// the state records.
func rank(b events.Body) int {
	switch b.(type) {
	case events.Readable:
		return 3
	case events.Unread:
		return 2
	case events.Uncommitted:
		return 1
	case events.Erased:
		return 0
	}
	panic(fmt.Sprintf("fold: %T is not a Body", b))
}

// FoldFacts is what one fold did, for the command event: every count is
// present and zero when zero, so a fold of nothing reads records=0 and no
// fold leaves no facts `[LAW:nothing-unseen]`.
type FoldFacts struct {
	// Records is the records read; Torn the torn spans skipped; Duplicates
	// the records of an event read twice.
	Records, Torn, Duplicates int
	Decoded                   DecodeCounts
	// Checked is the signatures verified.
	Checked  int
	Verdicts VerdictCounts
	// Applied is the events whose rules applied; Carried and Unapplied
	// count the others by reason.
	Applied            int
	Carried, Unapplied map[string]int
	Counts             StateCounts
}

// DecodeCounts is the records by what decoding made of them; Unread is
// keyed by kind and reason.
type DecodeCounts struct {
	Readable                       int
	Unread                         map[string]int
	Erased, Uncommitted, Malformed int
}

// VerdictCounts is the signature verdicts of the events folded.
type VerdictCounts struct{ Valid, Invalid, UnknownSuite int }

// StateCounts is the folded state's size and its contradictions.
type StateCounts struct {
	Items, Tombstoned, Comments, Labels                          int
	ProseConflicts, Overruled, MixedForms, Collisions, Uncreated int
}

func newFacts() FoldFacts {
	f := FoldFacts{Decoded: DecodeCounts{Unread: map[string]int{}}, Carried: map[string]int{}, Unapplied: map[string]int{}}
	for _, r := range []Reason{BadSignature{}, UnknownSuite{}} {
		f.Carried[ReasonName(r)] = 0
	}
	for _, w := range []UnappliedWhy{Unread{}, Erased{}, Uncommitted{}, Misaddressed{}} {
		f.Unapplied[WhyName(w)] = 0
	}
	return f
}

func (d *DecodeCounts) count(e events.Event) {
	switch b := e.Body().(type) {
	case events.Readable:
		d.Readable++
	case events.Unread:
		d.Unread[string(e.Header().Kind())+":"+UnreadName(b.Why)]++
	case events.Erased:
		d.Erased++
	case events.Uncommitted:
		d.Uncommitted++
	}
}

func (v *VerdictCounts) count(verdict events.SignatureVerdict) {
	switch verdict {
	case events.Valid:
		v.Valid++
	case events.Invalid:
		v.Invalid++
	case events.UnknownSuite:
		v.UnknownSuite++
	}
}

func (f *FoldFacts) count(o outcome) {
	switch o := o.(type) {
	case applied:
		f.Applied++
	case carriedFor:
		f.Carried[ReasonName(o.why)]++
	case unappliedFor:
		f.Unapplied[WhyName(o.why)]++
	}
}

// ReasonName is a carried reason's wire name.
func ReasonName(r Reason) string {
	switch r.(type) {
	case BadSignature:
		return "bad_signature"
	case UnknownSuite:
		return "unknown_suite"
	}
	panic(fmt.Sprintf("fold: %T is not a Reason", r))
}

// WhyName is an unapplied reason's wire name.
func WhyName(w UnappliedWhy) string {
	switch w.(type) {
	case Unread:
		return "unread"
	case Erased:
		return "erased"
	case Uncommitted:
		return "uncommitted"
	case Misaddressed:
		return "misaddressed"
	}
	panic(fmt.Sprintf("fold: %T is not an UnappliedWhy", w))
}

// UnreadName is an unread payload's reason's wire name.
func UnreadName(w events.UnreadWhy) string {
	switch w.(type) {
	case events.UnknownKind:
		return "unknown_kind"
	case events.NewerSchema:
		return "newer_schema"
	case events.Undecodable:
		return "undecodable"
	}
	panic(fmt.Sprintf("fold: %T is not an UnreadWhy", w))
}

// countsOf is s's size and contradictions.
func countsOf(s State) StateCounts {
	var c StateCounts
	s.items.each(func(r *item) {
		if len(r.creates) > 0 {
			c.Items++
		}
		c.origin(r.origin())
		if _, ok := r.life.(Tombstoned); ok {
			c.Tombstoned++
		}
		c.Labels += len(r.facts.Members(labels))
		c.facts(r.facts)
		for _, el := range r.sortedElements() {
			o := el.origin(r.id)
			if _, uncreated := o.(Uncreated); !uncreated && el.creates[0].in == comments {
				c.Comments++
			}
			c.origin(o)
			c.facts(el.facts)
		}
	})
	return c
}

func (c *StateCounts) origin(o Origin) {
	switch o.(type) {
	case Created:
	case Collided:
		c.Collisions++
	case Uncreated:
		c.Uncreated++
	}
}

func (c *StateCounts) facts(f Facts) {
	for _, r := range f.registers() {
		prose := r.prose()
		if len(prose) > 1 {
			c.ProseConflicts++
		}
		switch p := r.settled().(type) {
		case Settled:
			c.Overruled += len(p.Overruled)
			if len(prose) > 0 {
				c.MixedForms++
			}
		case Unset:
		}
	}
}
