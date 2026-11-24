package root

import (
	"fmt"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

// foldEnv is the fold every invocation runs: the first facts' kinds, the
// suites this binary verifies, and the cores that verify.
func foldEnv(workers int) fold.Env {
	env, err := fold.NewEnv(fold.NewRegistry(fold.FirstFacts()...), events.NewSuites(events.Ed25519()), workers)
	if err != nil {
		// [LAW:no-silent-failure] GOMAXPROCS and the twin's count are at
		// least one; another is a build fault, refused before any step runs
		panic(fmt.Sprintf("root: %v", err))
	}
	return env
}

// opened is a run whose store opened and folded, §5 steps 1 and 3: the
// store, the checkout it is minted for, and the state.
type opened struct {
	body
	store    command.Opened
	founding fold.Founding
	checkout git.Checkout
	id       command.Minted
	folded   fold.Folded
}

// open opens the run's store and folds it, or fails as an environment that
// has none, or whose store no enrollment founded: only admin init, which
// founds it, reads such a store.
func (r body) open() (opened, error) {
	switch o := r.p.Store.(type) {
	case command.Opened:
		in := r.p.Place.(command.InCheckout) // a store opens only for a minted checkout
		op, err := r.openAs(o, in.Checkout, in.Identity.(command.Minted))
		if err != nil {
			return opened{}, err
		}
		if err := unjudged(op.folded.State.Policy(), in.Checkout); err != nil {
			return opened{}, err
		}
		return op, nil
	case command.Unopened:
		return opened{}, storeFailure{err: o.Why}
	}
	panic(fmt.Sprintf("root: %T is not an Opening", r.p.Store))
}

// openAs folds o, the store of c minted as id, seeded with the founding
// its clone pinned.
func (r body) openAs(o command.Opened, c git.Checkout, id command.Minted) (opened, error) {
	shared, _ := r.p.IdentityFiles(c)
	founding, err := pinned(shared)
	if err != nil {
		return opened{}, err
	}
	folded, err := replay(r.p.Fold, founding, o.Store, r.rec.Stored)
	if err != nil {
		return opened{}, storeFailure{err: err}
	}
	r.rec.Policy.Founding, r.rec.Policy.Under = founding, folded.State.Policy()
	return opened{body: r, store: o, founding: founding, checkout: c, id: id, folded: folded}, nil
}

// stored runs do on the run's store, opened and folded.
func (r body) stored(do func(opened) cli.Outcome) cli.Outcome {
	o, err := r.open()
	if err != nil {
		return failed(err)
	}
	return do(o)
}

// replay reads every head and every stream from 0, and folds what it read;
// each step's result lands on stored as it returns, so a failing step
// leaves the steps before it on the event.
func replay(env fold.Env, founding fold.Founding, s *store.Store, stored *command.StoreFacts) (fold.Folded, error) {
	heads, read, err := s.ReadAll()
	if err != nil {
		return fold.Folded{}, err
	}
	stored.Heads = append(stored.Heads, heads)
	stored.Reads = append(stored.Reads, read...)
	folded := fold.Replay(env, founding, read)
	stored.Folds = append(stored.Folds, folded.Facts)
	return folded, nil
}

// mint signs drafts as one frame of events after the folded state,
// written under the version ref names.
func (o opened) mint(drafts []events.Draft, ref git.ObjectID) (events.Minted, error) {
	ctx := events.Context{Writer: o.id.Checkout, Agent: o.p.Agent, ConfigRef: ref, Policy: o.folded.State.Cites(),
		Frontier: o.folded.State.Frontier(), Known: o.folded.State.Known(), Now: o.now}
	m, err := events.Mint(ctx, events.NewDrafts(drafts[0], drafts[1:]...), o.id.Key.Signer(), o.p.Entropy)
	if err != nil {
		return events.Minted{}, command.Internal{Err: fmt.Errorf("minting: %w", err)}
	}
	return m, nil
}

// append writes m's frame to the store, recording what it wrote.
func (o opened) append(m events.Minted) error {
	appended, err := o.store.Store.Append(m.Batch)
	if err != nil {
		return storeFailure{err: err}
	}
	o.rec.Stored.Appends = append(o.rec.Stored.Appends, appended)
	return nil
}

// storeFailure is a verb that has no store to read, or whose store failed
// a step: the environment's, never lit2's.
type storeFailure struct{ err error }

func (f storeFailure) Error() string     { return "the store: " + f.err.Error() }
func (storeFailure) Class() reason.Class { return reason.ClassEnvironment }
func (storeFailure) Reason() string      { return "store" }

// unenrolled is a store whose clone names no founding: no verb reads it
// until admin init founds or adopts one, the environment's step to take.
type unenrolled struct{}

func (unenrolled) Error() string {
	return "this clone names no founding enrollment, so no policy says who may write: lit2 admin init founds the store, or adopts the one " + string(FoundingFile) + " names"
}
func (unenrolled) Class() reason.Class { return reason.ClassEnvironment }
func (unenrolled) Reason() string      { return "policy" }
