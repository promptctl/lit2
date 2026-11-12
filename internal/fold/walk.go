package fold

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

// Tag is the fold's version, which keys the snapshot: raised with any
// change to a rule, a shape or the snapshot's codec, so no snapshot an
// older fold wrote is resumed. Verification is a fold rule (§3.20), so a
// binary that adds a signature suite raises it too, and its genesis judges
// again every event an older binary carried as UnknownSuite: an exception
// is always checked again.
func Tag() store.FoldTag { return store.FoldTag("lit2 fold 1") }

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

// Folded is a fold's state, what the root saves, and the fold's facts for
// the command event.
type Folded struct {
	State State
	Save  Save
	Facts FoldFacts
}

// Save is what the root does with the snapshot after a fold: keep the one
// it resumed from, which is current, or write the next.
//
//sumtype:decl
type Save interface{ save() }

// Current is a snapshot extended by nothing: the store planned no range,
// so the snapshot resumed from is the snapshot, and rewriting it would
// write the same state again.
type Current struct{}

// Written is the next snapshot: the marks for the store to save, and the
// fold's payload.
type Written struct {
	Marks   []store.Mark
	Payload []byte
}

func (Current) save() {}
func (Written) save() {}

// Trust is the verdicts a snapshot carries: below each mark, under the
// same log, every event was folded and so checked, and Valid unless it is
// one of the Exceptions, whose verdict was not.
type Trust struct {
	Marks      []store.Mark
	Exceptions []events.EventID
}

// Snapshot is a snapshot's payload, read: the exceptions, the last event
// folded, and the state.
type Snapshot struct {
	Exceptions []events.EventID
	Through    Through
	State      State
}

// Through is the last event a snapshot folded, in total order.
//
//sumtype:decl
type Through interface{ through() }

// Nothing is a fold of no event.
type Nothing struct{}

// Upto is a fold whose last event in total order is Key.
type Upto struct{ Key events.OrderKey }

func (Nothing) through() {}
func (Upto) through()    {}

// Genesis folds scans, every stream from 0, from the zero State. It trusts
// the verdicts below held, the marks the store's plan judged still hold,
// and reads from file only its exceptions, under any fold version: a
// fold-version bump checks no signature that once verified, and a stream
// the store found rewritten is checked whole.
func Genesis(e Env, why Refold, held []store.Mark, file store.SnapshotFile, scans []Scanned) Folded {
	return walk(e, State{}, Nothing{}, nil, nil, trustOf(held, file), Refolded{Why: why}, scans)
}

// Extend folds scans, the streams past marks, onto a snapshot. Every shape
// merges in any order, so a delta that interleaves the snapshot's events
// extends it as a genesis of the union would fold it.
func Extend(e Env, s Snapshot, marks []store.Mark, scans []Scanned) Folded {
	return walk(e, s.State, s.Through, marks, s.Exceptions, Trust{Marks: marks, Exceptions: s.Exceptions}, Extended{}, scans)
}

// trustOf is the verdicts held carries with file's exceptions: none from no
// file, a torn one, or a payload whose exceptions do not read, since a mark
// trusted without them would trust what failed.
func trustOf(held []store.Mark, file store.SnapshotFile) Trust {
	switch f := file.(type) {
	case store.Saved:
		ex, err := readExceptions(f.Payload)
		if err != nil {
			return Trust{}
		}
		return Trust{Marks: held, Exceptions: ex}
	case store.NoSnapshot, store.TornSnapshot:
		return Trust{}
	}
	panic(fmt.Sprintf("fold: %T is not a SnapshotFile", file))
}

// walk is the one walk: decode every record, keep one of each event, sort
// by the total order, check, and step each in order onto s.
func walk(e Env, s State, through Through, marks []store.Mark, exceptions []events.EventID, t Trust, base BaseFact, scans []Scanned) Folded {
	facts := newFacts(base)
	kinds := e.registry.Kinds()
	var located []Located
	seen := map[events.EventID]int{}
	for _, sc := range scans {
		facts.Torn += len(sc.Scan.Torn)
		ref := sc.Range.Stream.Ref
		for _, fr := range sc.Scan.Frames {
			at := store.Position{Log: sc.Range.Stream.Until.Log, At: fr.At}
			for _, rec := range fr.Batch.Records() {
				facts.Records++
				switch d := events.Decode(kinds, rec).(type) {
				case events.Event:
					facts.Decoded.count(d)
					l := Located{Event: d, Ref: ref, At: at}
					i, dup := seen[d.ID()]
					switch {
					case !dup:
						seen[d.ID()] = len(located)
						located = append(located, l)
					case rank(d.Body()) > rank(located[i].Event.Body()):
						facts.Duplicates++
						located[i] = l
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
	slices.SortFunc(located, func(a, b Located) int { return events.Compare(a.Event.Order(), b.Event.Order()) })
	checked, trusted := check(e, located, t)
	facts.Checked, facts.Trusted = len(checked)-trusted, trusted
	// Through and Late move over verified events only, as the frontier
	// does: a carried event's position is the forger's to choose
	was := through
	var excepted []events.EventID
	for _, c := range checked {
		facts.Verdicts.count(c.verdict)
		switch c.verdict {
		case events.Valid:
			if before(c.event.Order(), was) {
				facts.Late++
			}
			through = later(through, c.event.Order())
		case events.Invalid, events.UnknownSuite:
			excepted = append(excepted, c.event.ID())
		}
		var out outcome
		s, out = e.registry.step(s, c)
		facts.count(out)
	}
	exceptions = withIDs(exceptions, excepted)
	snap := Snapshot{Exceptions: exceptions, Through: through, State: s}
	facts.Exceptions, facts.Counts = len(exceptions), countsOf(s)
	return Folded{State: s, Save: saved(base, marks, scans, snap), Facts: facts}
}

// saved is the save after a walk from base over scans: an extension handed
// nothing to read leaves the snapshot current, and every other walk writes
// one.
func saved(base BaseFact, marks []store.Mark, scans []Scanned, snap Snapshot) Save {
	switch base.(type) {
	case Extended:
		if len(scans) == 0 {
			return Current{}
		}
	case Refolded:
	}
	return Written{Marks: advanced(marks, scans), Payload: snap.encode()}
}

// rank orders the bodies the copies of one event can carry, by how much of
// the signed payload each keeps: the payload read; the payload committed
// but unread by this binary; a body that is not the one signed; none. The
// walk keeps the highest, so which copy stands does not depend on which
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

// before is whether k sorts before where t stopped.
func before(k events.OrderKey, t Through) bool {
	switch t := t.(type) {
	case Nothing:
		return false
	case Upto:
		return events.Compare(k, t.Key) < 0
	}
	panic(fmt.Sprintf("fold: %T is not a Through", t))
}

func later(t Through, k events.OrderKey) Through {
	switch t := t.(type) {
	case Nothing:
		return Upto{Key: k}
	case Upto:
		if events.Compare(k, t.Key) > 0 {
			return Upto{Key: k}
		}
		return t
	}
	panic(fmt.Sprintf("fold: %T is not a Through", t))
}

// advanced is marks with each scanned stream's mark at its last whole
// frame, by ref name: the incomplete tail is read again next time.
func advanced(marks []store.Mark, scans []Scanned) []store.Mark {
	by := map[store.Ref]store.Mark{}
	for _, m := range marks {
		by[m.Ref] = m
	}
	for _, sc := range scans {
		ref := sc.Range.Stream.Ref
		by[ref] = store.Mark{Ref: ref, Upto: store.Position{Log: sc.Range.Stream.Until.Log, At: sc.Scan.End}}
	}
	out := make([]store.Mark, 0, len(by))
	for _, m := range by {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b store.Mark) int { return cmp.Compare(a.Ref.Name(), b.Ref.Name()) })
	return out
}

// BaseFact is where a fold started.
//
//sumtype:decl
type BaseFact interface{ baseFact() }

// Extended is a fold resumed from the snapshot.
type Extended struct{}

// Refolded is a fold from genesis, and why.
type Refolded struct{ Why Refold }

func (Extended) baseFact() {}
func (Refolded) baseFact() {}

// Refold is why a fold started from genesis.
//
//sumtype:decl
type Refold interface{ refold() }

// Missed is a snapshot the store did not resume.
type Missed struct{ Miss store.Miss }

// PayloadUnreadable is a snapshot resumed whose payload does not read.
type PayloadUnreadable struct{ Err error }

func (Missed) refold()            {}
func (PayloadUnreadable) refold() {}

// FoldFacts is what one fold did, for the command event: every count is
// present and zero when zero, so a fold of nothing reads records=0 and no
// fold leaves no facts `[LAW:nothing-unseen]`.
type FoldFacts struct {
	Base BaseFact
	// Records is the records read; Torn the torn spans skipped; Duplicates
	// the records of an event read twice; Late the events that sort before
	// the snapshot's last.
	Records, Torn, Duplicates, Late int
	Decoded                         DecodeCounts
	// Checked is the signatures verified; Trusted the verdicts a snapshot
	// carried; Exceptions the events saved as not Valid.
	Checked, Trusted, Exceptions int
	Verdicts                     VerdictCounts
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

func newFacts(base BaseFact) FoldFacts {
	f := FoldFacts{Base: base, Decoded: DecodeCounts{Unread: map[string]int{}}, Carried: map[string]int{}, Unapplied: map[string]int{}}
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
		for _, el := range r.elements {
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
	for _, r := range f.fields {
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
