package root

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

// A write whose bytes landed and whose sync then failed is written: every
// later read sees it, so the command goes on as it would after a durable
// write, says what it wrote, and only then fails, naming each such write
// `[LAW:dataflow-not-control-flow]`. The facts the event records are what
// says so, the one source of which writes were not durable
// `[LAW:one-source-of-truth]`.

// durable is o, unless o is Done and a write the facts record landed but
// may not survive a power loss: then the run fails notDurable. A run that
// failed after such a write keeps its own failure, and its event says the
// write.
func durable(f command.StoreFacts, o cli.Outcome) cli.Outcome {
	if _, done := o.(cli.Done); !done {
		return o
	}
	if nd := notDurableOf(f); len(nd.writes) > 0 {
		return cli.Failed{Failure: nd}
	}
	return o
}

// notDurableOf is every write f records that landed and whose sync failed.
func notDurableOf(f command.StoreFacts) notDurable {
	nd := notDurable{}
	for _, a := range f.Appends {
		switch a := a.(type) {
		case store.AppendedNotDurable:
			nd.writes = append(nd.writes, fmt.Sprintf("a frame of %d records is in the store, and a sync after it failed: %v; every later command reads it, so running this command again may write it twice", a.Records, a.Why))
		case store.Appended, store.NotAppended:
		}
	}
	for _, c := range f.Creates {
		switch p := c.Placing.(type) {
		case files.PlacedNotDurable:
			nd.writes = append(nd.writes, fmt.Sprintf("%s is written, and a sync after it failed: %v; running lit2 admin init again syncs it", c.Path, p.Why))
		case files.Placed, files.NotPlaced:
		}
	}
	return nd
}

// notDurable is a run whose writes all landed, some of them with a sync
// that failed: written, but a power loss may undo them. A failed sync is
// never retried, since after one the kernel may already have dropped what
// it did not write, and a second sync can succeed falsely.
type notDurable struct{ writes []string }

func (n notDurable) Error() string {
	return "written, and may not survive a power loss: " + strings.Join(n.writes, "; and ")
}
func (notDurable) Class() reason.Class { return reason.ClassEnvironment }
func (notDurable) Reason() string      { return "durability" }

// recorded is a port whose every create-once is recorded by record, the
// file named by its path at: what the event says of each file a command
// created, durable or not `[LAW:single-enforcer]`.
type recorded struct {
	files.Files
	at     func(files.Name) string
	record func(command.Creation)
}

func (r recorded) CreateOnce(n files.Name, b []byte) files.Placing {
	p := r.Files.CreateOnce(n, b)
	r.record(command.Creation{Path: r.at(n), Placing: p})
	return p
}

// recording is fs with every create recorded on r's event, each file named
// by its path in d.
func (r body) recording(fs files.Files, d files.Dir) files.Files {
	return recorded{Files: fs, at: d.PathOf, record: func(c command.Creation) { r.rec.Stored.Creates = append(r.rec.Stored.Creates, c) }}
}
