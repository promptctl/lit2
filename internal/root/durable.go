package root

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// A write whose bytes landed and whose sync then failed is written: every
// later read sees it, so the command goes on as it would after a durable
// write, says what it wrote, and only then fails, naming each such write
// `[LAW:dataflow-not-control-flow]`.

// writes is every write a run made, each said on its event and, when it
// landed with a sync that failed, on its outcome, both from the one value
// the write returned: the one place a run's writes are judged
// `[LAW:single-enforcer]`.
type writes struct {
	stored   *command.StoreFacts
	unsynced []string
}

// create creates f holding b, once. The error is a create that placed
// nothing; one that placed its file is no failure, durable or not.
func (w *writes) create(f onceFile, b []byte) error {
	p := f.files.CreateOnce(f.name, b)
	w.created(command.Creation{Path: f.path(), Placing: p})
	if no, failed := p.(files.NotPlaced); failed {
		return no.Why
	}
	return nil
}

// created is c, said on the event, and on the outcome when it landed not
// durable.
func (w *writes) created(c command.Creation) {
	w.stored.Creates = append(w.stored.Creates, c)
	switch p := c.Placing.(type) {
	case files.PlacedNotDurable:
		w.unsynced = append(w.unsynced, fmt.Sprintf("%s landed, and that it survives a power loss cannot be confirmed: a sync after it failed: %v", c.Path, p.Why))
	case files.Placed, files.NotPlaced:
	}
}

// appended is a, said on the event, and on the outcome when it landed not
// durable. The error is an append that landed no frame.
func (w *writes) appended(a store.Appending) error {
	w.stored.Appends = append(w.stored.Appends, a)
	switch a := a.(type) {
	case store.NotAppended:
		return storeFailure{err: fmt.Errorf("no whole frame was written: %w", a.Why)}
	case store.AppendedNotDurable:
		w.unsynced = append(w.unsynced, fmt.Sprintf("a frame of %d records landed in the store, and that it survives a power loss cannot be confirmed: a sync after it failed: %v; every later command reads it, so running this command again writes it twice", a.Records, a.Why))
	case store.Appended:
	}
	return nil
}

// end is o, with every write that landed not durable: a run that was Done
// fails notDurable, and one that failed carries both failures, its own
// class first.
func (w *writes) end(o cli.Outcome) cli.Outcome {
	if len(w.unsynced) == 0 {
		return o
	}
	nd := notDurable{writes: w.unsynced}
	switch o := o.(type) {
	case cli.Done:
		return cli.Failed{Failure: nd}
	case cli.Failed:
		return cli.Failed{Failure: both{Failure: o.Failure, also: nd}}
	}
	panic(fmt.Sprintf("root: %T is not an Outcome", o))
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

// both is a run that failed twice: its class and reason are the first
// failure's, and its message and its chain hold both
// `[LAW:no-silent-failure]`.
type both struct {
	reason.Failure
	also reason.Failure
}

func (b both) Error() string   { return b.Failure.Error() + "; and " + b.also.Error() }
func (b both) Unwrap() []error { return []error{b.Failure, b.also} }

// onceFile is a file a create-once writes outside the store: its port,
// the directory the port is of, and its name there.
type onceFile struct {
	files files.Files
	dir   files.Dir
	name  files.Name
}

// path is f's path, as the event names it.
func (f onceFile) path() string { return f.dir.PathOf(f.name) }

// onceFiles is every file of a checkout a create-once writes outside the
// store: the identity's two, the founding pin, and the work tree's two.
type onceFiles struct {
	id, key, pin, founding, workspace onceFile
}

// onceFiles is c's, through p's ports: the one declaration of them init's
// creates and the leftovers scan both read `[LAW:one-source-of-truth]`.
func (p Process) onceFiles(c git.Checkout) onceFiles {
	shared, private := p.IdentityFiles(c)
	tree := p.WorkTree(c)
	return onceFiles{
		id:        onceFile{files: private, dir: command.DirOf(command.IDFile{}, c), name: command.NameOf(command.IDFile{})},
		key:       onceFile{files: shared, dir: command.DirOf(command.PrincipalFile{}, c), name: command.NameOf(command.PrincipalFile{})},
		pin:       onceFile{files: p.Pin(c), dir: pinDir(c), name: pinFile},
		founding:  onceFile{files: tree, dir: litDir(c), name: foundingFile},
		workspace: onceFile{files: tree, dir: litDir(c), name: verbs.WorkspaceFile()},
	}
}

func (o onceFiles) all() []onceFile { return []onceFile{o.id, o.key, o.pin, o.founding, o.workspace} }

// leftovers is the temps beside each of c's once files, through p's ports,
// and in the logs' directory of the store opening opened, or why they did
// not read. A create reaps its name's temps once it is placed, and the
// once files are created again only when a person reruns init, so a
// crash's temp is said on every command until then
// `[LAW:no-silent-failure]`.
func leftovers(p Process, c git.Checkout, opening command.Opening) []command.Leftovers {
	var ls []command.Leftovers
	beside := func(dir string, temps []string, err error) {
		switch {
		case err != nil:
			ls = append(ls, command.TempsUnread{Dir: dir, Why: err})
		case len(temps) > 0:
			ls = append(ls, command.TempsBeside{Dir: dir, Temps: temps})
		}
	}
	for _, f := range p.onceFiles(c).all() {
		temps, err := f.files.Temps(f.name)
		beside(f.dir.Path(), temps, err)
	}
	switch o := opening.(type) {
	case command.Opened:
		temps, err := o.Store.Temps()
		beside(store.LogsDir(c).Path(), temps, err)
	case command.Unopened:
	}
	return ls
}
