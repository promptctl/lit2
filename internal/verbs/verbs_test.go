package verbs_test

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	registry = fold.NewRegistry(fold.FirstFacts()...)
	env      = must(fold.NewEnv(registry, events.NewSuites(events.Ed25519()), 1))
	key      = must(events.NewKey(bytes.NewReader(bytes.Repeat([]byte{1}, 32))))
	running  = must(config.ReadReaderVersion("v0.4.0"))
	ref      = must(git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e"))

	// one and two are two checkouts writing one store
	one, two = store.CheckoutID{1}, store.CheckoutID{2}
)

// profile is a configuration of two levels, a story and a feature homed
// in unfiled, one label, and three columns, with more TOML after it.
func profile(more string) string {
	return `prefix = "lit"
claim_ttl = "6h"
labels = ["ready"]

[levels.1]
name = "story"

[levels.2]
name = "feature"
home = "unfiled"

[columns.backlog]
initial = true
next = ["plan"]

[columns.plan]
next = ["done"]

[columns.done]
terminal = true
` + more
}

// book is one store's state under one schema, which tests write to as
// either checkout, having read any state they kept.
type book struct {
	t       *testing.T
	schema  config.Schema
	st      fold.State
	entropy *rand.ChaCha8
	created events.ItemID // the last item a command created
}

func newBook(t *testing.T, toml string) *book {
	t.Helper()
	s, err := config.Parse([]byte(toml), "fixture", running)
	if err != nil {
		t.Fatal(err)
	}
	return &book{t: t, schema: s, entropy: rand.NewChaCha8([32]byte{'v'})}
}

func (b *book) world(st fold.State) verbs.World { return verbs.World{State: st, Schema: b.schema} }

// mint mints ds as w, having read seen, and folds them onto the book.
func (b *book) mint(w store.CheckoutID, seen fold.State, ds []events.Draft) []events.Event {
	b.t.Helper()
	m, err := events.Mint(events.Context{Writer: w, Agent: events.Unlabeled{}, ConfigRef: ref, Policy: 1,
		Frontier: seen.Frontier(), Known: seen.Known(), Now: time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)},
		events.NewDrafts(ds[0], ds[1:]...), key.Signer(), b.entropy)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, c := range fold.Check(env, m.Events) {
		b.st = fold.Step(registry, b.st, c)
	}
	for _, e := range m.Events {
		if n, ok := e.Header().About().(events.NewItem); ok {
			b.created = n.ID
		}
	}
	return m.Events
}

// as runs verb on ops as w, having read seen: it compiles against seen,
// mints onto the book and is the echo.
func (b *book) as(w store.CheckoutID, seen fold.State, verb verbs.Verb, ops ...verbs.Operand) string {
	b.t.Helper()
	c := verbs.Compile(b.world(seen), verb, ops, func() (string, error) { return "", nil })
	if no, refused := c.Refusal(); refused {
		b.t.Fatalf("%s %+v refused: %s: %v", verb, ops, no.Reason, no.Err)
	}
	if len(c.Drafts) == 0 {
		return ""
	}
	minted := b.mint(w, seen, c.Drafts)
	return verbs.Echo(b.world(b.st), c, minted)
}

// do runs verb on ops as one, having read everything.
func (b *book) do(verb verbs.Verb, ops ...verbs.Operand) string {
	b.t.Helper()
	return b.as(one, b.st, verb, ops...)
}

// refused is the reason verb on ops is refused, failing t when it is not.
func (b *book) refused(verb verbs.Verb, ops ...verbs.Operand) string {
	b.t.Helper()
	c := verbs.Compile(b.world(b.st), verb, ops, func() (string, error) { return "", nil })
	no, refused := c.Refusal()
	if !refused {
		b.t.Fatalf("%s %+v was not refused", verb, ops)
	}
	if len(c.Drafts) != 0 {
		b.t.Errorf("%s %+v was refused and drafted %d", verb, ops, len(c.Drafts))
	}
	return no.Reason
}

func (b *book) get(path string) string {
	b.t.Helper()
	g := verbs.Get(b.world(b.st), []string{path})
	if no, refused := g.Refusal(); refused {
		b.t.Fatalf("get %s refused: %s: %v", path, no.Reason, no.Err)
	}
	return g.Out
}

func op(path, value string) verbs.Operand { return verbs.Operand{Path: path, Value: value} }

// filed is the id an echo's first line creates.
func filed(t *testing.T, echo string) string {
	t.Helper()
	first, _, _ := strings.Cut(echo, "\n")
	id, ok := strings.CutPrefix(first, "+ ")
	if !ok {
		t.Fatalf("the echo %q creates nothing first", echo)
	}
	return id
}

// TestOverReplacesOnlyWhatWasRead: two concurrent titles conflict; a
// write that read one of them replaces that one and still conflicts with
// the other; a write that read both settles it.
func TestOverReplacesOnlyWhatWasRead(t *testing.T) {
	b := newBook(t, profile(""))
	id := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: a}")))
	s0 := b.st
	b.as(one, s0, verbs.Set, op(id+"/title", "b"))
	s1 := b.st
	b.as(two, s0, verbs.Set, op(id+"/title", "c"))
	both := func(x, y string) bool {
		got := b.get(id + "/title")
		return got == id+"/title\t"+x+"\n"+id+"/title\t"+y+"\n" || got == id+"/title\t"+y+"\n"+id+"/title\t"+x+"\n"
	}
	if !both("b", "c") {
		t.Fatalf("two concurrent titles read %q; want both", b.get(id+"/title"))
	}
	echo := b.as(one, s1, verbs.Set, op(id+"/title", "d"))
	if echo != id+"/title \"b\" → \"d\"\n" {
		t.Errorf("a write over b alone echoes %q", echo)
	}
	if !both("c", "d") {
		t.Errorf("a write that read only b reads %q; want c still beside d", b.get(id+"/title"))
	}
	if echo := b.do(verbs.Set, op(id+"/title", "e")); !strings.Contains(echo, " | ") {
		t.Errorf("a write over a conflict echoes %q; want both values before it", echo)
	}
	if got := b.get(id + "/title"); got != "e\n" {
		t.Errorf("a write that read both reads %q; want e alone", got)
	}
}

// TestTwoEqualLiveWritesAreBothRetired: the same title written
// concurrently is two live writes, so writing it again is not a no-op,
// and after it one write is live.
func TestTwoEqualLiveWritesAreBothRetired(t *testing.T) {
	b := newBook(t, profile(""))
	id := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: a}")))
	s0 := b.st
	b.as(one, s0, verbs.Set, op(id+"/title", "x"))
	b.as(two, s0, verbs.Set, op(id+"/title", "x"))
	if b.do(verbs.Set, op(id+"/title", "x")) == "" {
		t.Fatal("rewriting a value two writes hold drafted nothing")
	}
	if b.do(verbs.Set, op(id+"/title", "x")) != "" {
		t.Error("after one write over both, the same value drafts again")
	}
}

// TestAStatusUnderAnAliasReadsByItsColumn: a status recorded under a name
// the column now carries as an alias reads by the column's name, and a
// write may name the alias.
func TestAStatusUnderAnAliasReadsByItsColumn(t *testing.T) {
	b := newBook(t, strings.Replace(profile(""), "[columns.plan]\nnext = [\"done\"]", "[columns.plan]\nnext = [\"done\"]\naliases = [\"doing\"]", 1))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	story := filed(t, b.do(verbs.Add, op(feature+"/children", "{title: s}")))
	b.mint(one, b.st, []events.Draft{events.Act{About: events.Item{ID: b.created},
		Payload: events.Transition{From: events.MustName("backlog"), To: events.MustName("doing")}}})
	if got := b.get(story + "/status"); got != "plan\n" {
		t.Errorf("a status recorded as doing reads %q; want plan", got)
	}
	if echo := b.do(verbs.Set, op(story+"/status", "done")); echo != story+"/status plan → done\n" {
		t.Errorf("set status echoes %q", echo)
	}
	if echo := b.do(verbs.Set, op(story+"/status", "doing")); echo != story+"/status done → plan\n" {
		t.Errorf("set status by its alias echoes %q", echo)
	}
}

// TestATemplateTheCompileRefusesIsTheTemplates: a template's column that
// the graph does not hold refuses the create, as the template's; a key
// given in the value is written in its place.
func TestATemplateTheCompileRefusesIsTheTemplates(t *testing.T) {
	b := newBook(t, profile("\n[levels.1.template]\nstatus = \"nope\"\n"))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	if r := b.refused(verbs.Add, op(feature+"/children", "{title: s}")); r != "template" {
		t.Errorf("a template status no column holds is refused as %s; want template", r)
	}
	if echo := b.do(verbs.Add, op(feature+"/children", "{title: s, status: plan}")); !strings.Contains(echo, "/status = plan\n") || strings.Contains(echo, "(template)") {
		t.Errorf("a key given over the template echoes %q", echo)
	}
}

// TestAGraphTheAncestryBreaksIsTheConfigurations: a section that adds a
// second initial column refuses a status write under it as configuration.
func TestAGraphTheAncestryBreaksIsTheConfigurations(t *testing.T) {
	b := newBook(t, profile("\n[nodes.unfiled.columns.intake]\ninitial = true\nnext = [\"done\"]\n"))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	story := filed(t, b.do(verbs.Add, op(feature+"/children", "{title: s}")))
	if r := b.refused(verbs.Set, op(story+"/status", "plan")); r != "configuration" {
		t.Errorf("a status under a broken graph is refused as %s; want configuration", r)
	}
}

// TestAHeldUndeclaredMemberIsRemovable: a label held but no longer
// declared is removed by name; a name neither held nor declared is
// refused as a value; a declared one not held is a no-op.
func TestAHeldUndeclaredMemberIsRemovable(t *testing.T) {
	b := newBook(t, profile(""))
	id := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	b.mint(one, b.st, []events.Draft{events.Act{About: events.Item{ID: b.created},
		Payload: events.MemberAdd{Field: events.MustName("labels"), Member: events.MustName("legacy")}}})
	if got := b.get(id + "/labels"); got != "legacy\n" {
		t.Fatalf("the held label reads %q", got)
	}
	if echo := b.do(verbs.Rm, op(id+"/labels/legacy", "")); echo != "- "+id+"/labels/legacy\n" {
		t.Errorf("rm of a held undeclared label echoes %q; want %q", echo, "- "+id+"/labels/legacy\n")
	}
	if r := b.refused(verbs.Rm, op(id+"/labels/legasy", "")); r != "value" {
		t.Errorf("rm of a typo is refused as %s; want value", r)
	}
	if b.do(verbs.Rm, op(id+"/labels/ready", "")) != "" {
		t.Error("rm of a declared label not held drafted")
	}
}
