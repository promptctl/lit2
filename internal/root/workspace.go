package root

import (
	"errors"
	"fmt"
	"io"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/verbs"
)

// inForce is an opened run under the version in force, loaded, beside the
// workspace file as this run read it: what every verb reads (§3.15).
type inForce struct {
	opened
	schema config.Schema
	// ref is the version's blob, the config-ref every event written under
	// it records.
	ref  git.ObjectID
	file verbs.File
}

// load loads the version the fold put in force and reads the file. Only an
// applied version is in force, so a store with none refuses every verb,
// naming the command that applies one, and a draft in the work tree never
// reaches a verb.
func (o opened) load() (inForce, error) {
	in, ok := o.folded.State.Workspace().(fold.InForce)
	if !ok {
		return inForce{}, workspaceFailure{err: fmt.Errorf("%w: lit2 %s applies the committed %s", errNoVersionInForce, commandname.Commit(), verbs.WorkspacePath())}
	}
	schema, err := config.Load(in.Active.Bytes, o.p.Reader)
	if err != nil {
		return inForce{}, verbs.Unreadable{Blob: in.Active.Blob, Err: err}
	}
	o.rec.Configured(command.ConfigFacts{Facts: schema.Facts(), Ref: in.Active.Blob.String()})
	_, file := o.workspaceFile()
	return inForce{opened: o, schema: schema, ref: in.Active.Blob, file: file}, nil
}

// verb runs do under the version in force.
func (r body) verb(do func(inForce) cli.Outcome) cli.Outcome {
	o, err := r.open()
	if err != nil {
		return failed(err)
	}
	v, err := o.load()
	if err != nil {
		return failed(err)
	}
	return do(v)
}

// fields is the field table help prints: the version in force's, or why
// there is none. Help never fails for want of one.
func (r body) fields() cli.Fields {
	o, err := r.open()
	if err != nil {
		return cli.NoFields{Why: err.Error()}
	}
	v, err := o.load()
	if err != nil {
		return cli.NoFields{Why: err.Error()}
	}
	return cli.ActiveFields{Table: v.schema.Table()}
}

// workspaceFile is .lit/workspace.toml as this run read it: its bytes, and
// the file as the board says it. It is never parsed here.
func (o opened) workspaceFile() ([]byte, verbs.File) {
	b, err := read(o.p.WorkTree(o.checkout), verbs.WorkspaceFile(), func(b []byte) ([]byte, error) { return b, nil })
	var none files.NotFound
	switch {
	case errors.As(err, &none):
		return nil, verbs.NoFile{}
	case err != nil:
		return nil, verbs.UnreadableFile{Err: err}
	}
	return b, verbs.OnDisk{Blob: git.HashBlob(o.checkout.Format, b)}
}

// commit is config commit: the file, once it is HEAD's, applied as the
// next version over every live one, through the one judge, Admit. A file
// that is absent, uncommitted or changed since HEAD is refused, naming the
// step that makes it HEAD's; a file already in force with nothing
// contending writes nothing.
func (o opened) commit() cli.Outcome {
	b, file := o.workspaceFile()
	var disk verbs.OnDisk
	switch f := file.(type) {
	case verbs.OnDisk:
		disk = f
	case verbs.NoFile:
		return cli.Failed{Failure: refuseCommit(fmt.Errorf("%w: there is no %s; write it, commit it, then lit2 %s", errNothingToApply, verbs.WorkspacePath(), commandname.Commit()))}
	case verbs.UnreadableFile:
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading %s: %w", verbs.WorkspacePath(), f.Err)}}
	}
	at, err := o.p.Code(o.checkout).AtHead(verbs.WorkspacePath())
	if err != nil {
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading the code repository: %w", err)}}
	}
	switch at := at.(type) {
	case git.Untracked:
		return cli.Failed{Failure: refuseCommit(fmt.Errorf("%[2]w: %[1]s is not in HEAD; git add %[1]s and git commit, then lit2 %[3]s", verbs.WorkspacePath(), errNotInHead, commandname.Commit()))}
	case git.NotAFile:
		return cli.Failed{Failure: refuseCommit(fmt.Errorf("%[3]w: HEAD holds %[1]s as mode %[2]s, not a regular file; commit %[1]s as a regular file, then lit2 %[4]s", verbs.WorkspacePath(), at.Mode, errNotAFileInHead, commandname.Commit()))}
	case git.Tracked:
		if at.Blob != disk.Blob {
			return cli.Failed{Failure: refuseCommit(fmt.Errorf("%[3]w: %[1]s differs from HEAD's %[2]s; git commit %[1]s, then lit2 %[4]s", verbs.WorkspacePath(), at.Blob, errDiffersFromHead, commandname.Commit()))}
		}
	}
	// version one is written under itself; any later under the version in
	// force, which the run loads, so its event says the version it ran under
	ref := disk.Blob
	if in, ok := o.folded.State.Workspace().(fold.InForce); ok {
		v, err := o.load()
		if err != nil {
			return failed(err)
		}
		if v.ref == disk.Blob && len(in.Contending) == 0 {
			return written(fmt.Fprintf(o.p.Out, "%s is already in force\n", disk.Blob))
		}
		ref = v.ref
	}
	return o.apply(b, disk.Blob, ref, fmt.Sprintf("applied %s\n", disk.Blob))
}

// apply judges version and, admitted, writes it as one apply under ref,
// then says said.
func (o opened) apply(version []byte, blob, ref git.ObjectID, said string) cli.Outcome {
	act, err := verbs.Admit(o.folded.State, version, o.checkout.Format, o.p.Reader)
	if err != nil {
		return failed(err)
	}
	return o.judged([]events.Draft{act}, ref, said)
}

// judged mints drafts as one frame under ref and, unless the fold carries
// any of its events, appends it and says said.
func (o opened) judged(drafts []events.Draft, ref git.ObjectID, said string) cli.Outcome {
	m, err := o.mint(drafts, ref)
	if err != nil {
		return failed(err)
	}
	if _, out, refused := o.judge(o.folded.State, m); refused {
		return out
	}
	if err := o.append(m); err != nil {
		return failed(err)
	}
	return written(io.WriteString(o.p.Out, said))
}

// found mints drafts, the founding enrollment first, as one frame under
// ref, judged on a fold that enrollment founds, and, unless the fold
// carries any of its events, pins the enrollment as the clone's founding,
// appends the frame and names the enrollment in FoundingFile, then says
// said. The pin comes first: a frame that loses the race to pin is never
// written, since every replica would carry it `[LAW:single-enforcer]`.
func (o opened) found(once onceFiles, drafts []events.Draft, ref git.ObjectID, said string) cli.Outcome {
	m, err := o.mint(drafts, ref)
	if err != nil {
		return failed(err)
	}
	founding := fold.FoundedBy{Enrollment: m.Events[0].ID()}
	judged, out, refused := o.judge(fold.Seed(founding), m)
	if refused {
		return out
	}
	won, err := pin(o.w, once.pin, founding.Enrollment)
	if err != nil {
		return failed(err)
	}
	if won != founding {
		return failed(foundingRaced{won: won})
	}
	if err := o.append(m); err != nil {
		return failed(unwritten{err: err, pin: pinPath(o.checkout)})
	}
	o.rec.Policy.Founding, o.rec.Policy.Under = won, judged.State.Policy()
	if err := writeFoundingFile(o.w, once.founding, founding, pinPath(o.checkout)); err != nil {
		return failed(err)
	}
	return written(fmt.Fprintf(o.p.Out, "%sfounding %s named in %s: commit it, and every clone adopts this store\n", said, founding.Enrollment, foundingPath()))
}

// judge is the frame m extended onto s, and its failure when the fold
// carries any of its events: a frame every replica would carry is never
// written `[LAW:single-enforcer]`. Its drafts write the root, which names
// no item or home, so a refusal reads no schema.
func (o opened) judge(onto fold.State, m events.Minted) (fold.Extended, cli.Outcome, bool) {
	seen := verbs.World{View: evaluate.Of(o.folded.State, config.Schema{}, o.now)}
	ext := fold.Extend(o.p.Fold, onto, m.Events)
	o.rec.Policy.Verdicts = command.Verdicts(m.Events, ext.Outcomes)
	if no, carried := verbs.Carried(seen, ext.Outcomes); carried {
		return ext, cli.Failed{Failure: refusal{no}}, true
	}
	return ext, nil, false
}

// status is config status: the board's workspace line, and whether the
// version in force was ever committed on HEAD's branch. The line is said
// first, so a code repository that does not read refuses the history half
// alone.
func (o opened) status() cli.Outcome {
	_, file := o.workspaceFile()
	st := o.folded.State
	if out, isFailed := written(io.WriteString(o.p.Out, verbs.Workspace(st, file)+"\n")).(cli.Failed); isFailed {
		return out
	}
	in, ok := st.Workspace().(fold.InForce)
	if !ok {
		return cli.Done{}
	}
	ever, err := o.p.Code(o.checkout).EverAt(verbs.WorkspacePath(), in.Active.Blob)
	if err != nil {
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading the code repository: %w", err)}}
	}
	history := "in HEAD's history: yes"
	if !ever {
		history = fmt.Sprintf("in HEAD's history: no, %s was never %s on this branch", in.Active.Blob, verbs.WorkspacePath())
	}
	return written(io.WriteString(o.p.Out, history+"\n"))
}

// init is admin init: the checkout's id and the principal's key, each
// minted once, and the principal printed, with the recovery phrase when
// this run made the key; then the store's founding: a clone that names
// none adopts the one FoundingFile names, or with no file founds the
// store; then, in a store no version is in force in, the workspace's
// version one: the file in the work tree, or the default profile written
// there when there is none. The mint moves the place: the run ends in its
// checkout with the identity the mint read back, so the store init opens,
// the principal it prints and the place its event says, the cell the mint
// moves, are that one read's, whatever the mint did, and a panic after the
// mint is recorded in the minted place.
func (r body) init() cli.Outcome {
	in, err := command.CheckoutAt(r.place.Place())
	if err != nil {
		return cli.Failed{Failure: storeFailure{err: fmt.Errorf("%s makes a git checkout a writer: %w", commandname.Init(), err)}}
	}
	once := r.p.onceFiles(in.Checkout)
	minting := MintIdentity(once.id, once.key, r.p.Entropy)
	// every file init creates is on its event, by path, durable or not
	for _, c := range minting.creations() {
		r.w.created(c)
	}
	r.place.Move(command.InCheckout{Checkout: in.Checkout, Identity: minting.identity()})
	return r.enroll(in.Checkout, once, minting)
}

// enroll is admin init in c after its mint.
func (r body) enroll(c git.Checkout, once onceFiles, minting Minting) cli.Outcome {
	switch m := minting.(type) {
	case NotMinted:
		return cli.Failed{Failure: storeFailure{err: m.Why}}
	case Found:
		if out, isFailed := written(fmt.Fprintf(r.p.Out, "principal %s\n", m.Minted.Key.Principal())).(cli.Failed); isFailed {
			return out
		}
	case Made:
		if err := showPhrase(r.p.Out, m.Minted.Key, once.key); err != nil {
			return cli.Failed{Failure: err}
		}
	case MadeUnread:
		// the phrase is said whatever the readback did: it is shown once
		unread := storeFailure{err: m.Why}
		if err := showPhrase(r.p.Out, m.Key, once.key); err != nil {
			return cli.Failed{Failure: both{Failure: err, also: unread}}
		}
		return cli.Failed{Failure: unread}
	}
	tree := r.p.WorkTree(c)
	if err := adopt(r.w, once.pin, tree); err != nil {
		return failed(err)
	}
	o, err := r.openFounded()
	if err != nil {
		return failed(err)
	}
	// the file names the clone's founding, for the next clone to adopt
	if f, founded := o.founding.(fold.FoundedBy); founded {
		if err := writeFoundingFile(r.w, once.founding, f, pinPath(c)); err != nil {
			return failed(err)
		}
	}
	me := o.id.Key.Principal()
	var drafts []events.Draft
	// an owner applies version one: set /workspace is the owner role's
	said, owns, founds := "", false, false
	switch s := verbs.Bootstrap(o.folded.State, me).(type) {
	case verbs.Found:
		r.rec.Policy.Bootstrap = "enroll"
		drafts, said, owns, founds = append(drafts, events.Act{About: events.Root{}, Payload: s.Enroll}), fmt.Sprintf("enrolled: policy v1, owner %s\n", me), true, true
	case verbs.Listed:
		r.rec.Policy.Bootstrap = "already_member"
		owns = s.Role == verbs.Owner
	case verbs.Ask:
		r.rec.Policy.Bootstrap = "request"
		drafts = append(drafts, events.Act{About: events.Root{}, Payload: events.Request{}})
		said = fmt.Sprintf("membership requested: an owner admits you with lit2 %s /members '{principal: %q, role: contributor}'\n", commandname.Add(), me.String())
	case verbs.Unjudged:
		r.rec.Policy.Bootstrap = "unjudged"
		return failed(unjudged(s.Policy, c))
	case verbs.Asked:
		r.rec.Policy.Bootstrap = "already_requested"
		return written(fmt.Fprintf(r.p.Out, "membership requested: no owner has admitted %s yet\n", me))
	}
	// the version the frame is written under: the one in force; with none,
	// the work tree's, which an owner applies as version one in the same
	// frame, and for a requester, which applies nothing and so writes no
	// file, the default profile's
	_, inForce := o.folded.State.Workspace().(fold.InForce)
	var ref git.ObjectID
	switch {
	case inForce && len(drafts) == 0:
		return cli.Done{}
	case inForce:
		v, err := o.load()
		if err != nil {
			return failed(err)
		}
		ref = v.ref
	case owns:
		b, err := versionOne(r.w, once.workspace)
		if err != nil {
			return cli.Failed{Failure: workspaceFailure{err: err}}
		}
		ref = git.HashBlob(c.Format, b)
		act, err := verbs.Admit(o.folded.State, b, c.Format, r.p.Reader)
		if err != nil {
			return failed(err)
		}
		drafts, said = append(drafts, act), said+fmt.Sprintf("workspace %s applied as version one\n", ref)
	case len(drafts) == 0:
		return written(fmt.Fprintf(r.p.Out, "no workspace version is in force: an owner applies one with lit2 %s\n", commandname.Commit()))
	default:
		ref = git.HashBlob(c.Format, assets.DefaultWorkspace.Bytes())
	}
	if founds {
		return o.found(once, drafts, ref, said)
	}
	return o.judged(drafts, ref, said)
}

// versionOne is the file in the work tree, at file, the default profile
// created there first when there is none; read back, so a race to create
// it applies the winner's.
func versionOne(w *writes, file onceFile) ([]byte, error) {
	if err := w.create(file, assets.DefaultWorkspace.Bytes()); err != nil {
		return nil, fmt.Errorf("writing %s: %w", verbs.WorkspacePath(), err)
	}
	b, err := read(file.files, file.name, func(b []byte) ([]byte, error) { return b, nil })
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", verbs.WorkspacePath(), err)
	}
	return b, nil
}

// workspaceFailure is a run that cannot read the workspace's version, its
// file or the code repository: the environment's.
type workspaceFailure struct{ err error }

func (f workspaceFailure) Error() string     { return f.err.Error() }
func (f workspaceFailure) Unwrap() error     { return f.err }
func (workspaceFailure) Class() reason.Class { return reason.ClassEnvironment }
func (workspaceFailure) Reason() string      { return "workspace" }

// errNoVersionInForce is a verb run with no workspace version applied.
var errNoVersionInForce = errors.New("no version in force")

// commitRefused is a file config commit will not apply until a step is
// taken, the step named; its cause wraps which file it is of the four.
// It is built from its cause by refuseCommit.
type commitRefused struct{ cause error }

// refuseCommit is the refusal caused by cause.
func refuseCommit(cause error) commitRefused { return commitRefused{cause: cause} }

// The four files config commit refuses: none in the work tree, one HEAD
// does not hold, one HEAD holds as no regular file, and one HEAD holds
// other bytes of. Each refusal's message names the step that would apply it.
var (
	errNothingToApply  = errors.New("nothing to apply")
	errNotInHead       = errors.New("an uncommitted file")
	errNotAFileInHead  = errors.New("a committed file that is no regular file")
	errDiffersFromHead = errors.New("a file with uncommitted changes")
)

func (c commitRefused) Error() string     { return c.cause.Error() }
func (c commitRefused) Unwrap() error     { return c.cause }
func (commitRefused) Class() reason.Class { return reason.ClassRefused }
func (commitRefused) Reason() string      { return "workspace" }

// showPhrase prints the principal of key, created at file, and its
// recovery phrase, shown this once. A phrase that could not be written
// fails the run, EPIPE among the causes: a reader gone before the phrase
// reached it is a phrase no one saw `[LAW:no-silent-failure]`.
func showPhrase(out io.Writer, key events.Key, file onceFile) reason.Failure {
	if _, err := fmt.Fprintf(out, "principal %s\nrecovery phrase: %s\nwrite it down: it is the one way to restore this key\n", key.Principal(), key.Phrase()); err != nil {
		return phraseUnshown{err: err, key: file.path()}
	}
	return nil
}

// phraseUnshown is the recovery phrase of a key this run created that
// could not be written to stdout.
type phraseUnshown struct {
	err error
	key string
}

func (p phraseUnshown) Error() string {
	return fmt.Sprintf("the recovery phrase of the key created at %s was not shown: writing it to stdout: %v; the key works, and lit2 shows its phrase only when it creates it", p.key, p.err)
}
func (p phraseUnshown) Unwrap() error     { return p.err }
func (phraseUnshown) Class() reason.Class { return reason.ClassEnvironment }
func (phraseUnshown) Reason() string      { return "output" }
