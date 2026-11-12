package root

import (
	"fmt"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
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

// verb runs steps 1 and 3 of §5 before a verb's body: the store opened and
// folded, or the store failing as an environment that has none.
func verb(p Process, stored *command.StoreFacts, body func(fold.Folded) cli.Outcome) cli.Outcome {
	switch o := p.Store.(type) {
	case command.Opened:
		folded, err := open(p.Fold, o.Store, stored)
		if err != nil {
			return cli.Failed{Failure: storeFailure{err: err}}
		}
		return body(folded)
	case command.Unopened:
		return cli.Failed{Failure: storeFailure{err: o.Why}}
	}
	panic(fmt.Sprintf("root: %T is not an Opening", p.Store))
}

// open reads every head and the snapshot, folds what is past it, and saves
// the snapshot by rename, no lock; each step's result lands on stored as it
// returns, so a failing step leaves the steps before it on the event. The
// snapshot is a disposable cache, so what became of it is a fact on the
// event and never a failure of the verb: the next command refolds what a
// failed swap missed.
func open(env fold.Env, s *store.Store, stored *command.StoreFacts) (fold.Folded, error) {
	heads, err := s.Heads()
	if err != nil {
		return fold.Folded{}, err
	}
	stored.Heads = append(stored.Heads, heads)
	file, err := s.Snapshot()
	if err != nil {
		return fold.Folded{}, err
	}
	resume, start := begin(env, heads, file)
	stored.Plans = append(stored.Plans, resume)
	scans := make([]fold.Scanned, 0, len(resume.Delta))
	for _, r := range resume.Delta {
		scan, err := s.Read(r)
		if err != nil {
			return fold.Folded{}, err
		}
		stored.Reads = append(stored.Reads, command.Read{Range: r, Scan: scan})
		scans = append(scans, fold.Scanned{Range: r, Scan: scan})
	}
	folded := start(scans)
	stored.Folds = append(stored.Folds, folded.Facts)
	stored.Swaps = append(stored.Swaps, save(s, folded.Save))
	return folded, nil
}

// save is what became of the snapshot: kept when the fold extended it by
// nothing, else written, or not written and why.
func save(s *store.Store, sv fold.Save) command.Swap {
	switch sv := sv.(type) {
	case fold.Current:
		return command.Kept{}
	case fold.Written:
		swapped, err := s.Swap(fold.Tag(), sv.Marks, sv.Payload)
		if err != nil {
			return command.SwapFailed{Why: err}
		}
		return command.Swapped{Swapped: swapped}
	}
	panic(fmt.Sprintf("root: %T is not a Save", sv))
}

// begin is the plan the fold follows and how it starts, a pure decision:
// the store's delta onto a snapshot whose payload reads, or, when it does
// not, the plan of no snapshot, every stream from 0; and a genesis for the
// store's own reason. A genesis trusts the verdicts below the marks the
// store's plan held, with the file's exceptions.
func begin(env fold.Env, heads store.Heads, file store.SnapshotFile) (store.Resume, func([]fold.Scanned) fold.Folded) {
	resume := store.Plan(fold.Tag(), heads, file)
	switch b := resume.Base.(type) {
	case store.FromSaved:
		snap, err := fold.ReadSnapshot(b.Payload)
		if err != nil {
			// a payload that does not read is a snapshot that does not:
			// the store plans it as torn, every stream from 0, trusting none
			return begin(env, heads, store.TornSnapshot{Why: err})
		}
		return resume, func(scans []fold.Scanned) fold.Folded { return fold.Extend(env, snap, resume.Held, scans) }
	case store.Genesis:
		return resume, func(scans []fold.Scanned) fold.Folded {
			return fold.Genesis(env, b.Why, resume.Held, file, scans)
		}
	}
	panic(fmt.Sprintf("root: %T is not a Base", resume.Base))
}

// storeFailure is a verb that has no store to read, or whose store failed
// a step: the environment's, never lit2's.
type storeFailure struct{ err error }

func (f storeFailure) Error() string     { return "the store: " + f.err.Error() }
func (storeFailure) Class() reason.Class { return reason.ClassEnvironment }
func (storeFailure) Reason() string      { return "store" }
