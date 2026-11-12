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
	return fold.Env{Registry: fold.NewRegistry(fold.FirstFacts()...), Suites: events.NewSuites(events.Ed25519()), Workers: workers}
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
// returns, so a failing step leaves the steps before it on the event.
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
	resume := store.Plan(fold.Tag(), heads, file)
	stored.Plans = append(stored.Plans, resume)
	ranges, start := begin(env, heads, file, resume)
	scans := make([]fold.Scanned, 0, len(ranges))
	for _, r := range ranges {
		scan, err := s.Read(r)
		if err != nil {
			return fold.Folded{}, err
		}
		stored.Reads = append(stored.Reads, command.Read{Range: r, Scan: scan})
		scans = append(scans, fold.Scanned{Range: r, Scan: scan})
	}
	folded := start(scans)
	stored.Folds = append(stored.Folds, folded.Facts)
	swapped, err := s.Swap(fold.Tag(), folded.Save.Marks, folded.Save.Payload)
	if err != nil {
		return fold.Folded{}, err
	}
	stored.Swaps = append(stored.Swaps, swapped)
	return folded, nil
}

// begin is what the fold reads and how it starts, a pure decision: the
// delta onto a snapshot whose payload reads, or every stream from 0 for a
// genesis, which trusts whatever snapshot file there is.
func begin(env fold.Env, heads store.Heads, file store.SnapshotFile, resume store.Resume) ([]store.Range, func([]fold.Scanned) fold.Folded) {
	switch b := resume.Base.(type) {
	case store.FromSaved:
		saved, ok := file.(store.Saved)
		if !ok {
			panic(fmt.Sprintf("root: the store resumed from %T, which is not a saved snapshot", file))
		}
		snap, err := fold.ReadSnapshot(b.Payload)
		if err != nil {
			// every stream from 0 is the plan of no snapshot
			return store.Plan(fold.Tag(), heads, store.NoSnapshot{}).Delta, func(scans []fold.Scanned) fold.Folded {
				return fold.Genesis(env, fold.PayloadUnreadable{Err: err}, file, scans)
			}
		}
		return resume.Delta, func(scans []fold.Scanned) fold.Folded { return fold.Extend(env, snap, saved.Marks, scans) }
	case store.Genesis:
		return resume.Delta, func(scans []fold.Scanned) fold.Folded {
			return fold.Genesis(env, fold.Missed{Miss: b.Why}, file, scans)
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
