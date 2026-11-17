package fold_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// apply is an apply of version over the applies over.
func apply(version string, over ...events.EventID) events.Draft {
	return events.Act{About: events.Root{}, Payload: events.NewWorkspaceApply([]byte(version), git.SHA1, over)}
}

// inForce is the state's version in force, failing the test when it has none.
func inForce(t *testing.T, s fold.State) fold.InForce {
	t.Helper()
	in, ok := s.Workspace().(fold.InForce)
	if !ok {
		t.Fatalf("the workspace is %#v; want a version in force", s.Workspace())
	}
	return in
}

func TestAStoreWithNoApplyHasNoVersion(t *testing.T) {
	w, _ := item(t)
	if v, ok := w.state().Workspace().(fold.NoVersion); !ok {
		t.Errorf("a store holding only an item is %#v; want NoVersion", v)
	}
}

func TestAnApplyOverTheActiveVersionReplacesIt(t *testing.T) {
	w := newWorld(t)
	one := w.mint(a, fold.State{}, apply("v1"))[0]
	two := w.mint(a, w.state(), apply("v2", one.ID()))[0]
	in := inForce(t, w.state())
	if !bytes.Equal(in.Active.Bytes, []byte("v2")) || in.Active.Blob != git.HashBlob(git.SHA1, []byte("v2")) || in.Active.Stamp.Event != two.ID() {
		t.Errorf("the active version is %+v; want v2, by its apply", in.Active)
	}
	if len(in.Contending) != 0 || !slices.Equal(in.Live, []events.EventID{two.ID()}) {
		t.Errorf("an apply over the only version leaves contending %v, live %v; want none and itself", in.Contending, in.Live)
	}
	if len(in.Applied) != 2 || in.Applied[0].Stamp.Event != one.ID() || in.Applied[1].Stamp.Event != two.ID() {
		t.Errorf("applied is %+v; want both applies, in total order", in.Applied)
	}
	if c := w.replay().Facts.Counts; c.Versions != 2 || c.Contending != 0 {
		t.Errorf("the counts are %d versions, %d contending; want 2 and 0", c.Versions, c.Contending)
	}
}

// Two applies naming one parent contend: every replica runs the last in
// total order, the other contends, and an apply naming both resolves it.
func TestTwoAppliesOverOneParentContendUntilAnApplyNamesBoth(t *testing.T) {
	w := newWorld(t)
	one := w.mint(a, fold.State{}, apply("v1"))[0]
	seen := w.state()
	fromA := w.mint(a, seen, apply("A's", one.ID()))[0]
	fromB := w.mint(b, seen, apply("B's", one.ID()))[0]
	in := inForce(t, w.state())
	last, other := later(fromA, fromB), fromA
	if last.ID() == fromA.ID() {
		other = fromB
	}
	if in.Active.Stamp.Event != last.ID() || len(in.Contending) != 1 || in.Contending[0].Stamp.Event != other.ID() {
		t.Fatalf("two applies over one parent are active %s, contending %+v; want the later active and the other contending", in.Active.Stamp.Event, in.Contending)
	}
	if !slices.Equal(in.Live, []events.EventID{other.ID(), last.ID()}) {
		t.Errorf("live is %v; want both, in total order", in.Live)
	}
	if c := w.replay().Facts.Counts; c.Contending != 1 {
		t.Errorf("the counts hold %d contending; want 1", c.Contending)
	}
	resolved := w.mint(b, w.state(), apply("merged", in.Live...))[0]
	in = inForce(t, w.state())
	if in.Active.Stamp.Event != resolved.ID() || len(in.Contending) != 0 || len(in.Live) != 1 {
		t.Errorf("an apply naming both leaves %+v; want it alone in force", in)
	}
}

// Two applies of one blob agree: two checkouts that each applied the
// default as version one are not in conflict.
func TestLiveAppliesOfOneBlobAgree(t *testing.T) {
	w := newWorld(t)
	w.mint(a, fold.State{}, apply("default"))
	w.mint(b, fold.State{}, apply("default"))
	in := inForce(t, w.state())
	if len(in.Contending) != 0 || len(in.Live) != 2 {
		t.Errorf("two applies of one blob are %+v; want both live and none contending", in)
	}
}

// A blob two live applies hold contends once, whichever blob is active.
func TestAContendingBlobAppliedTwiceContendsOnce(t *testing.T) {
	w := newWorld(t)
	one := w.mint(a, fold.State{}, apply("v1"))[0]
	seen := w.state()
	w.mint(a, seen, apply("B", one.ID()))
	w.mint(b, seen, apply("B", one.ID()))
	w.mint(b, seen, apply("C", one.ID()))
	if in := inForce(t, w.state()); len(in.Contending) != 1 || len(in.Live) != 3 {
		t.Errorf("B twice and C are %+v; want three live and one contending", in)
	}
}

// An apply's Over retires an apply that arrives after it, as a field
// write's does, so the order applies arrive in decides nothing.
func TestAnApplyRetiresWhatItNamesInAnyArrivalOrder(t *testing.T) {
	w := newWorld(t)
	one := w.mint(a, fold.State{}, apply("v1"))[0]
	w.mint(b, w.state(), apply("v2", one.ID()))
	stale := w.view(map[store.Ref]int{a.ref(): 0})
	if in := inForce(t, stale); !bytes.Equal(in.Active.Bytes, []byte("v2")) {
		t.Errorf("before v1 arrives, the active version is %q; want v2", in.Active.Bytes)
	}
	if in := inForce(t, w.state()); !bytes.Equal(in.Active.Bytes, []byte("v2")) || len(in.Live) != 1 {
		t.Errorf("after v1 arrives, the workspace is %+v; want v2 alone", in)
	}
}

func TestAnApplyAboutAnItemIsMisaddressed(t *testing.T) {
	w, id := item(t)
	w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.NewWorkspaceApply([]byte("v"), git.SHA1, nil)})
	s := w.state()
	if _, ok := s.Workspace().(fold.NoVersion); !ok {
		t.Errorf("an apply about an item put a version in force: %#v", s.Workspace())
	}
	un := s.Integrity().Unapplied
	if len(un) != 1 || un[0].Why != (fold.Misaddressed{Want: "the root"}) {
		t.Errorf("an apply about an item is %+v; want it unapplied, misaddressed to the root", un)
	}
}

func TestAFieldWriteAboutTheRootIsMisaddressed(t *testing.T) {
	w := newWorld(t)
	w.mint(a, fold.State{}, events.Act{About: events.Root{}, Payload: events.FieldSet{Field: title, Value: plain("x")}})
	if un := w.state().Integrity().Unapplied; len(un) != 1 || un[0].Why != (fold.Misaddressed{Want: "an item or an element of one"}) {
		t.Errorf("a field.set about the root is %+v; want it unapplied, misaddressed", un)
	}
}
