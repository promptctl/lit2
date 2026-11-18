package fold_test

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// The fixed world every fold test mints in: two principals, A and B, and
// three checkouts, one each and a second of A's; one clock reading; the
// first facts' registry; and names the tests read.
var (
	keyA, keyB = keyOf(1), keyOf(2)
	checkoutA  = store.CheckoutID{0xa}
	checkoutA2 = store.CheckoutID{0xa, 2}
	checkoutB  = store.CheckoutID{0xb}
	configRef  = must(git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e"))
	t0         = time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	env        = must(fold.NewEnv(fold.NewRegistry(fold.FirstFacts()...), events.NewSuites(events.Ed25519()), 2))

	title    = name("title")
	priority = name("priority")
	labels   = name("labels")
	comments = name("comments")
	bodyName = name("body")
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func name(s string) events.Name { return events.MustName(s) }

func keyOf(seed byte) events.Key {
	return must(events.NewKey(bytes.NewReader(bytes.Repeat([]byte{seed}, 32))))
}

func level(n int) events.Level { return must(events.ParseLevel(n)) }

// writer is one writing checkout and its principal's signer.
type writer struct {
	checkout store.CheckoutID
	signer   events.Signer
}

var (
	a  = writer{checkout: checkoutA, signer: keyA.Signer()}
	a2 = writer{checkout: checkoutA2, signer: keyA.Signer()}
	b  = writer{checkout: checkoutB, signer: keyB.Signer()}
)

// ref is the writer's stream.
func (w writer) ref() store.Ref { return store.WriterRef{Checkout: w.checkout} }

// world is the streams of one store as a fold reads them: each ref's
// batches, in append order, under one log. base is the state its founding
// leaves, which a test mints from as a writer that read only it.
type world struct {
	t       testing.TB
	entropy *rand.ChaCha8
	order   []store.Ref
	streams map[store.Ref]*stream
	base    fold.State
}

type stream struct {
	log     store.LogID
	batches []store.Batch
}

// The policy every world is founded under: the owner role, and the
// contributor role of the default profile.
var (
	owner       = name("owner")
	contributor = name("contributor")
	roles       = map[events.Name][]events.Capability{contributor: capabilities(
		"add */children", "add own/*", "set own/*", "set */status", "add */comments", "rm own/*/*", "rm own/* (level=1)")}
	// founder enrolls every world, under A's key, on a ref of its own, and
	// founderB approves B into it, so the founding sorts before any test's
	// event
	founder  = writer{checkout: store.CheckoutID{0xf}, signer: keyA.Signer()}
	founderB = writer{checkout: store.CheckoutID{0xf, 0xb}, signer: keyB.Signer()}
)

func capabilities(texts ...string) []events.Capability {
	var cs []events.Capability
	for _, t := range texts {
		cs = append(cs, events.MustCapability(t))
	}
	return cs
}

// newWorld is a store founded with A and B its owners: A enrolls, proposes
// B as an owner and approves, and B approves, so version 2 is in force.
func newWorld(t testing.TB) *world {
	wd := unfounded(t)
	wd.mint(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))
	both := events.Propose{Base: wd.state().Policy().(fold.Enrolled).Policy.Digest(), Members: map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner}, Threshold: events.Majority{}}
	wd.mint(founder, wd.state(), root(both), root(events.Approve{Proposal: both.Digest()}))
	wd.mint(founderB, wd.state(), root(events.Approve{Proposal: both.Digest()}))
	wd.base = wd.state()
	if p, ok := wd.base.Policy().(fold.Enrolled); !ok || p.Policy.Version() != 2 {
		t.Fatalf("the founding left %#v in force, want version 2", wd.base.Policy())
	}
	return wd
}

// unfounded is a store with no enrollment.
func unfounded(t testing.TB) *world {
	return &world{t: t, entropy: rand.NewChaCha8([32]byte{'f', 'o', 'l', 'd'}), streams: map[store.Ref]*stream{}}
}

func root(p events.Payload) events.Draft { return events.Act{About: events.Root{}, Payload: p} }

// mint mints ds as w, at the frontier and with the names of seen, the
// state w folded, citing its policy version, and appends them to w's
// stream as one command.
func (wd *world) mint(w writer, seen fold.State, d events.Draft, more ...events.Draft) []events.Event {
	wd.t.Helper()
	m, err := events.Mint(events.Context{Writer: w.checkout, Agent: events.Unlabeled{}, ConfigRef: configRef, Policy: seen.Cites(),
		Frontier: seen.Frontier(), Known: seen.Known(), Now: t0}, events.NewDrafts(d, more...), w.signer, wd.entropy)
	if err != nil {
		wd.t.Fatal(err)
	}
	wd.append(w.ref(), m.Batch)
	return m.Events
}

// append appends batch to ref's stream.
func (wd *world) append(ref store.Ref, batch store.Batch) {
	s, ok := wd.streams[ref]
	if !ok {
		s = &stream{log: store.LogID{byte(len(wd.order) + 1)}}
		wd.streams[ref] = s
		wd.order = append(wd.order, ref)
	}
	s.batches = append(s.batches, batch)
}

// span is ref's batches from..to as a read of a stream whose head is the
// end of batch to finds them: its range from the offset of batch from, its
// until the end of batch to.
func (wd *world) span(ref store.Ref, from, to int) fold.Scanned {
	s := wd.streams[ref]
	var all []byte
	var at store.Offset
	for i, bt := range s.batches[:to] {
		if i == from {
			at = store.Offset(len(all))
		}
		all = append(all, store.Encode(bt)...)
	}
	if from == to {
		at = store.Offset(len(all))
	}
	r := store.Range{Stream: store.Stream{Ref: ref, Until: store.Position{Log: s.log, At: store.Offset(len(all))}}, From: at}
	return fold.Scanned{Range: r, Scan: store.ScanFrames(at, all[at:])}
}

// head is ref's batch count.
func (wd *world) head(ref store.Ref) int { return len(wd.streams[ref].batches) }

// scans is every stream from 0, in the order the streams were made.
func (wd *world) scans() []fold.Scanned {
	var out []fold.Scanned
	for _, ref := range wd.order {
		out = append(out, wd.span(ref, 0, wd.head(ref)))
	}
	return out
}

// replay folds every stream from 0.
func (wd *world) replay() fold.Folded {
	return fold.Replay(env, wd.scans())
}

// view is the fold of each stream's first upto[ref] batches, and every
// batch of a stream upto does not name: what a writer that has read only
// those folds.
func (wd *world) view(upto map[store.Ref]int) fold.State {
	var scans []fold.Scanned
	for _, ref := range wd.order {
		n, ok := upto[ref]
		if !ok {
			n = wd.head(ref)
		}
		scans = append(scans, wd.span(ref, 0, n))
	}
	return fold.Replay(env, scans).State
}

// state is the replay's state.
func (wd *world) state() fold.State { return wd.replay().State }

func create(at events.Placement) events.Draft {
	return events.CreateItem{Payload: events.ItemCreate{Place: at}}
}

func createUnder(parent events.ItemID) events.Draft {
	return events.CreateItem{Payload: events.ItemCreate{Place: events.BelowParent{Parent: parent}}}
}

func idOf(e events.Event) events.ItemID { return e.Header().About().(events.NewItem).ID }

// ghost is an item id no test mints: c, 25 times, then 0.
func ghost(c string) events.ItemID { return must(events.ParseItemID(strings.Repeat(c, 25) + "0")) }

func set(id events.ItemID, field events.Name, v events.Value, over ...events.Event) events.Draft {
	return events.Act{About: events.Item{ID: id}, Payload: events.FieldSet{Field: field, Value: v, Over: idsOf(over)}}
}

func prose(s string) events.Value { return events.Prose{Body: events.SealPlain(s)} }

func plain(s string) events.Value { return events.Plain{Canonical: s} }

// isPlain reports whether v is the plain value canonical. A Value is not
// compared with == : a plain value carries its unknown fields, so equality
// is its canonical text, as valueEqual states.
func isPlain(v events.Value, canonical string) bool {
	p, ok := v.(events.Plain)
	return ok && p.Canonical == canonical
}

func idsOf(es []events.Event) []events.EventID {
	var ids []events.EventID
	for _, e := range es {
		ids = append(ids, e.ID())
	}
	return ids
}

// found is id's item, failing the test when the state has none.
func found(t testing.TB, s fold.State, id events.ItemID) fold.Item {
	t.Helper()
	f, ok := s.Item(id).(fold.Found)
	if !ok {
		t.Fatalf("%s is %#v; want it found", id, s.Item(id))
	}
	return f.Item
}

// stamped is whether stamps are exactly the events es, in order.
func stamped(stamps []fold.Stamp, es ...events.Event) bool {
	if len(stamps) != len(es) {
		return false
	}
	for i, s := range stamps {
		if s.Event != es[i].ID() {
			return false
		}
	}
	return true
}

// commitment is the commitment of the text s.
func commitment(s string) events.Commitment { return events.SealPlain(s).Commitment() }

// advised is whether the state names concern at at.
func advised(s fold.State, at fold.Target, concern fold.Concern) bool {
	for _, adv := range s.Advisories() {
		if adv.At == at && sameConcern(adv.Is, concern) {
			return true
		}
	}
	return false
}

func sameConcern(a, b fold.Concern) bool {
	switch a := a.(type) {
	case fold.Conflict, fold.MixedForms, fold.Orphaned, fold.UnderLeaf, fold.Uncreated:
		return a == b
	case fold.Collided:
		c, ok := b.(fold.Collided)
		return ok && a.First.Stamp.Event == c.First.Stamp.Event && len(a.Others) == len(c.Others)
	}
	return false
}

// later is the one of two events the total order puts last.
func later(x, y events.Event) events.Event {
	if events.Compare(x.Order(), y.Order()) > 0 {
		return x
	}
	return y
}
