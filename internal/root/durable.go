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
	unsynced []unsyncedWrite
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
		w.unsynced = append(w.unsynced, createdNotDurable{path: c.Path, placed: p})
	case files.Placed, files.NotPlaced:
	}
}

// appended is a, said on the event, and on the outcome when it landed not
// durable, or landed no frame after its log's name's sync failed. The
// error is an append that landed no frame.
func (w *writes) appended(a store.Appending) error {
	w.stored.Appends = append(w.stored.Appends, a)
	switch a := a.(type) {
	case store.NotAppended:
		if l, unsynced := a.Log.(store.LogNotDurable); unsynced {
			w.unsynced = append(w.unsynced, logNotDurable{l})
		}
		return storeFailure{err: fmt.Errorf("no whole frame was written: %w", a.Why)}
	case store.AppendedNotDurable:
		w.unsynced = append(w.unsynced, appendedNotDurable{a})
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
// it did not write, and a later sync can succeed falsely; the process's
// one keeper answers it to every later sync of that directory or file,
// whichever port asks, and this run is the one that says it. Each write is kept as the value its call returned,
// and spelled only by Error, so its sync's error stays in the chain
// `[LAW:types-are-the-program]`.
type notDurable struct{ writes []unsyncedWrite }

// unsyncedWrite is one write that landed with a sync that failed: a
// create's, or an append's.
//
//sumtype:decl
type unsyncedWrite interface {
	unsyncedWrite()
	// why is the sync's failure.
	why() error
}

// createdNotDurable is a create-once of the file at path that placed it
// not durable.
type createdNotDurable struct {
	path   string
	placed files.PlacedNotDurable
}

// appendedNotDurable is an append whose frame landed not durable.
type appendedNotDurable struct{ appended store.AppendedNotDurable }

// logNotDurable is a log an append looked up or created, whose name's sync
// failed, though the append then landed no frame: the log is there, and a
// power loss may take it.
type logNotDurable struct{ log store.LogNotDurable }

func (createdNotDurable) unsyncedWrite()  {}
func (appendedNotDurable) unsyncedWrite() {}
func (logNotDurable) unsyncedWrite()      {}
func (c createdNotDurable) why() error    { return c.placed.Why }
func (a appendedNotDurable) why() error {
	return a.appended.Why
}
func (l logNotDurable) why() error { return l.log.Why }

// spelled is u as the outcome names it.
func spelled(u unsyncedWrite) (s string) {
	switch u := u.(type) {
	case createdNotDurable:
		s = fmt.Sprintf("%s landed, and that it survives a power loss cannot be confirmed: a sync after it failed: %v", u.path, u.placed.Why)
	case appendedNotDurable:
		s = fmt.Sprintf("a frame of %d records landed in the store, and that it survives a power loss cannot be confirmed: a sync after it failed: %v; every later command reads it, so running this command again writes it twice", u.appended.Records, u.appended.Why)
	case logNotDurable:
		s = fmt.Sprintf("this checkout's log in the store is there, and that it survives a power loss cannot be confirmed: a sync of its name failed: %v", u.log.Why)
	}
	return s
}

func (n notDurable) Error() string {
	writes := make([]string, len(n.writes))
	for i, u := range n.writes {
		writes[i] = spelled(u)
	}
	return "written, and may not survive a power loss: " + strings.Join(writes, "; and ")
}

// Unwrap is every write's failed sync, so errors.Is finds the errno.
func (n notDurable) Unwrap() []error {
	whys := make([]error, len(n.writes))
	for i, u := range n.writes {
		whys[i] = u.why()
	}
	return whys
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
// which names the directory it is of, and its name there.
type onceFile struct {
	files files.Files
	name  files.Name
}

// path is f's path, as the event names it.
func (f onceFile) path() string { return f.files.Dir().PathOf(f.name) }

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
		id:        onceFile{files: private, name: command.NameOf(command.IDFile{})},
		key:       onceFile{files: shared, name: command.NameOf(command.PrincipalFile{})},
		pin:       onceFile{files: p.Pin(c), name: pinFile},
		founding:  onceFile{files: tree, name: foundingFile},
		workspace: onceFile{files: tree, name: verbs.WorkspaceFile()},
	}
}

func (o onceFiles) all() []onceFile { return []onceFile{o.id, o.key, o.pin, o.founding, o.workspace} }

// leftovers is, for each directory of place's once files, through p's
// ports, and its store's logs' directory, the temps creates left there, or
// that it was not scanned, and why, each listed once the run's work is
// done, so a temp a create of the run reaped is not said as left. Each
// port's directory is listed once, however many once files it holds. A create
// reaps its name's temps once it is placed, and the once files are
// created again only when a person reruns init, so a crash's temp is said
// on every command until then `[LAW:no-silent-failure]`. Outside a
// checkout there is no directory to scan.
func leftovers(p Process, place command.Place, stored *command.StoreFacts) (ls []command.Leftovers) {
	switch at := place.(type) {
	case command.InCheckout:
		ls = append(beside(p.onceFiles(at.Checkout).all()), logTemps(at.Checkout, stored))
	case command.NoCheckout:
	case command.Unlocatable:
	}
	return ls
}

// beside is the temps in each distinct directory of fs's ports, listed
// once each, in the order fs first names them.
func beside(fs []onceFile) []command.Leftovers {
	var ls []command.Leftovers
	seen := map[string]bool{}
	for _, f := range fs {
		dir := f.files.Dir().Path()
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if l, err := f.files.List(); err != nil {
			ls = append(ls, command.TempsNotScanned{Dir: dir, Why: err})
		} else {
			ls = append(ls, command.TempsScanned{Dir: dir, Temps: l.Temps})
		}
	}
	return ls
}

// logTemps is the temps of c's store's logs, listed now, or why they
// were not: no store opened, or a listing that failed.
func logTemps(c git.Checkout, stored *command.StoreFacts) (l command.Leftovers) {
	switch o := stored.Opening.(type) {
	case command.Opened:
		dir := o.Store.Logs().Path()
		if temps, err := o.Store.Temps(); err != nil {
			l = command.TempsNotScanned{Dir: dir, Why: err}
		} else {
			l = command.TempsScanned{Dir: dir, Temps: temps}
		}
	case command.Unopened:
		l = command.TempsNotScanned{Dir: store.LogsDir(c).Path(), Why: o.Why}
	}
	return l
}
