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

// open reads every head and every stream from 0, and folds what it read;
// each step's result lands on stored as it returns, so a failing step
// leaves the steps before it on the event.
func open(env fold.Env, s *store.Store, stored *command.StoreFacts) (fold.Folded, error) {
	heads, err := s.Heads()
	if err != nil {
		return fold.Folded{}, err
	}
	stored.Heads = append(stored.Heads, heads)
	scans := make([]fold.Scanned, 0, len(heads.Streams))
	for _, st := range heads.Streams {
		r := store.Range{Stream: st, From: 0}
		scan, err := s.Read(r)
		if err != nil {
			return fold.Folded{}, err
		}
		stored.Reads = append(stored.Reads, command.Read{Range: r, Scan: scan})
		scans = append(scans, fold.Scanned{Range: r, Scan: scan})
	}
	folded := fold.Replay(env, scans)
	stored.Folds = append(stored.Folds, folded.Facts)
	return folded, nil
}

// storeFailure is a verb that has no store to read, or whose store failed
// a step: the environment's, never lit2's.
type storeFailure struct{ err error }

func (f storeFailure) Error() string     { return "the store: " + f.err.Error() }
func (storeFailure) Class() reason.Class { return reason.ClassEnvironment }
func (storeFailure) Reason() string      { return "store" }
