package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/fold"
)

// flag is test.flag, a kind no package declares: a payload, its decoder,
// and one On line, compiled against the packages as they ship.
type flag struct{ Flag events.Name }

func (flag) Kind() events.Kind            { return "test.flag" }
func (flag) Schema() events.SchemaVersion { return 1 }
func (f flag) AppendTo(w *wire.Writer)    { w.Bytes(1, []byte(f.Flag.String())) }
func readFlag(r *wire.Reader) (events.Versioned, error) {
	var f flag
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		if tag == 1 {
			n, err := events.ParseName(string(r.Bytes()))
			if err != nil {
				return nil, err
			}
			f.Flag = n
		}
	}
	return f, r.Err()
}

func TestANewKindTouchesNothing(t *testing.T) {
	flags := name("flags")
	withFlags := must(fold.NewEnv(fold.NewRegistry(append(fold.FirstFacts(), fold.On(fold.Items, func(f fold.Fact[flag]) fold.Applies {
		return fold.Changes{List: []fold.Change{fold.Add{At: f.At.(fold.Node), Set: flags, Member: f.Payload.Flag}}}
	}, func(f fold.Fact[flag]) fold.Intent {
		return fold.PathWrite{Verb: events.Add, At: fold.OfItem{ID: f.At.(fold.OnItem).ID}, Field: []string{flags.String()}}
	}, readFlag))...), env.Suites(), 1))
	w, id := item(t)
	before := w.state()
	minted := w.mint(a, before, events.Act{About: events.Item{ID: id}, Payload: flag{Flag: name("blocked")}})
	s := fold.Replay(withFlags, w.founding, w.scans()).State
	got := found(t, s, id).Facts.Members(flags)
	if len(got) != 1 || got[0].Name != name("blocked") || len(s.Integrity().Unapplied) != 0 {
		t.Errorf("test.flag folds to %#v, unapplied %#v; want the flag blocked", got, s.Integrity().Unapplied)
	}
	// the first facts alone read it as a kind they do not know
	if un := w.state().Integrity().Unapplied; len(un) != 1 || un[0].Why != (fold.Unread{Why: events.UnknownKind{}}) {
		t.Errorf("without its row test.flag is %#v; want Unread{UnknownKind}", un)
	}
	// a caller's own minted event, readable to it, extends a registry that
	// has no row for its kind as a kind it does not know; the registry
	// that has the row applies it
	if out := fold.Extend(env, before, minted).Outcomes; len(out) != 1 || out[0] != (fold.LeftFor{Why: fold.Unread{Why: events.UnknownKind{}}}) {
		t.Errorf("extending the first facts with test.flag is %#v; want it left unread as an unknown kind", out)
	}
	if out := fold.Extend(withFlags, before, minted).Outcomes; len(out) != 1 {
		t.Fatalf("extending with test.flag's row is %#v", out)
	} else if _, ok := out[0].(fold.Applied); !ok {
		t.Errorf("extending with test.flag's row is %#v; want it applied", out[0])
	}
}
