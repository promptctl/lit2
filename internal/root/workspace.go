package root

import (
	"errors"
	"fmt"
	"io"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
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
		return inForce{}, workspaceFailure{err: fmt.Errorf("no version is in force: lit2 config commit applies the committed %s", verbs.WorkspaceFile)}
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
	b, err := read(o.p.WorkTree(o.checkout), verbs.WorkspaceFile, func(b []byte) ([]byte, error) { return b, nil })
	var none store.NotFound
	switch {
	case errors.As(err, &none):
		return nil, verbs.NoFile{}
	case err != nil:
		return nil, verbs.UnreadableFile{Err: err}
	}
	return b, verbs.OnDisk{Blob: git.HashBlob(o.store.Format, b)}
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
		return cli.Failed{Failure: commitRefused{why: fmt.Sprintf("there is no %s to apply: write it, commit it, then lit2 config commit", verbs.WorkspaceFile)}}
	case verbs.UnreadableFile:
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading %s: %w", verbs.WorkspaceFile, f.Err)}}
	}
	at, err := o.p.Code(o.checkout, o.store.Format).AtHead(verbs.WorkspaceFile)
	if err != nil {
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading the code repository: %w", err)}}
	}
	switch at := at.(type) {
	case git.Untracked:
		return cli.Failed{Failure: commitRefused{why: fmt.Sprintf("%[1]s is not in HEAD: git add %[1]s and git commit, then lit2 config commit", verbs.WorkspaceFile)}}
	case git.Tracked:
		if at.Blob != disk.Blob {
			return cli.Failed{Failure: commitRefused{why: fmt.Sprintf("%[1]s differs from HEAD's %[2]s: git commit %[1]s, then lit2 config commit", verbs.WorkspaceFile, at.Blob)}}
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
	act, err := verbs.Admit(o.folded.State, version, o.store.Format, o.p.Reader)
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
	if out, refused := o.refused(o.folded.State, m); refused {
		return out
	}
	if err := o.append(m); err != nil {
		return failed(err)
	}
	return written(io.WriteString(o.p.Out, said))
}

// found mints drafts, the founding enrollment first, as one frame under
// ref, judged on a fold that enrollment founds, and, unless the fold
// carries any of its events, appends it, pins the enrollment as the
// clone's founding and names it in FoundingFile, then says said.
func (o opened) found(shared, tree store.Files, drafts []events.Draft, ref git.ObjectID, said string) cli.Outcome {
	m, err := o.mint(drafts, ref)
	if err != nil {
		return failed(err)
	}
	founding := fold.FoundedBy{Enrollment: m.Events[0].ID()}
	if out, refused := o.refused(fold.Seed(founding), m); refused {
		return out
	}
	if err := o.append(m); err != nil {
		return failed(err)
	}
	won, err := pin(shared, founding.Enrollment)
	if err != nil {
		return failed(err)
	}
	o.rec.Policy.Founding = won
	if won != founding {
		return failed(foundingRaced{won: won})
	}
	if err := writeFoundingFile(tree, founding); err != nil {
		return failed(err)
	}
	return written(fmt.Fprintf(o.p.Out, "%sfounding %s named in %s: commit it, and every clone adopts this store\n", said, founding.Enrollment, FoundingFile))
}

// refused is the failure of the frame m, extended onto s, when the fold
// carries any of its events: a frame every replica would carry is never
// written `[LAW:single-enforcer]`. Its drafts write the root, which names
// no item or home, so a refusal reads no schema.
func (o opened) refused(onto fold.State, m events.Minted) (cli.Outcome, bool) {
	seen := verbs.World{View: evaluate.Of(o.folded.State, config.Schema{}, o.now)}
	outcomes := fold.Extend(o.p.Fold, onto, m.Events).Outcomes
	o.rec.Policy.Verdicts = command.Verdicts(m.Events, outcomes)
	if no, carried := verbs.Carried(seen, outcomes); carried {
		return cli.Failed{Failure: refusal{no}}, true
	}
	return nil, false
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
	ever, err := o.p.Code(o.checkout, o.store.Format).EverAt(verbs.WorkspaceFile, in.Active.Blob)
	if err != nil {
		return cli.Failed{Failure: workspaceFailure{err: fmt.Errorf("reading the code repository: %w", err)}}
	}
	history := "in HEAD's history: yes"
	if !ever {
		history = fmt.Sprintf("in HEAD's history: no, %s was never %s on this branch", in.Active.Blob, verbs.WorkspaceFile)
	}
	return written(io.WriteString(o.p.Out, history+"\n"))
}

// init is admin init: the checkout's id and the principal's key, each
// minted once, and the principal printed, with the recovery phrase when
// this run made the key; then the store's founding: a clone that names
// none adopts the one FoundingFile names, or with no file founds the
// store; then, in a store no version is in force in, the workspace's
// version one: the file in the work tree, or the default profile written
// there when there is none.
func (r body) init() cli.Outcome {
	in, ok := r.p.Place.(command.InCheckout)
	if !ok {
		return cli.Failed{Failure: storeFailure{err: fmt.Errorf("admin init makes a git checkout a writer, and this is none")}}
	}
	shared, private := r.p.IdentityFiles(in.Checkout)
	minted, fresh, err := MintIdentity(shared, private, r.p.Entropy)
	if err != nil {
		return cli.Failed{Failure: storeFailure{err: err}}
	}
	r.rec.Stored.Inits = append(r.rec.Stored.Inits, fresh)
	said := fmt.Sprintf("principal %s\n", minted.Key.Principal())
	if fresh.Key == store.Created {
		said += fmt.Sprintf("recovery phrase: %s\nwrite it down: it is the one way to restore this key\n", minted.Key.Phrase())
	}
	// the phrase is said before anything else can fail: it is shown once
	if out, isFailed := written(io.WriteString(r.p.Out, said)).(cli.Failed); isFailed {
		return out
	}
	opening := r.p.Open(in.Checkout, minted)
	r.rec.Stored.Opening = opening
	so, ok := opening.(command.Opened)
	if !ok {
		return cli.Failed{Failure: storeFailure{err: opening.(command.Unopened).Why}}
	}
	o, err := r.openAs(so, in.Checkout, minted)
	if err != nil {
		return failed(err)
	}
	tree := r.p.WorkTree(in.Checkout)
	if _, none := o.founding.(fold.NoFounding); none {
		named, err := foundingFileIn(tree)
		if err != nil {
			return failed(err)
		}
		if n, adopts := named.(fold.FoundedBy); adopts {
			if _, err := pin(shared, n.Enrollment); err != nil {
				return failed(err)
			}
			if o, err = r.openAs(so, in.Checkout, minted); err != nil {
				return failed(err)
			}
		}
	}
	// the file names the clone's founding, for the next clone to adopt
	if f, founded := o.founding.(fold.FoundedBy); founded {
		if err := writeFoundingFile(tree, f); err != nil {
			return failed(err)
		}
	}
	me := minted.Key.Principal()
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
		said = fmt.Sprintf("membership requested: an owner admits you with lit2 add /members '{principal: %q, role: contributor}'\n", me.String())
	case verbs.FoundingAbsent:
		r.rec.Policy.Bootstrap = "founding_absent"
		return cli.Failed{Failure: foundingAbsent(s)}
	case verbs.FoundedUnread:
		r.rec.Policy.Bootstrap = "founded_unread"
		return cli.Failed{Failure: foundedUnread(s)}
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
		b, err := versionOne(tree)
		if err != nil {
			return cli.Failed{Failure: workspaceFailure{err: err}}
		}
		ref = git.HashBlob(so.Format, b)
		act, err := verbs.Admit(o.folded.State, b, so.Format, r.p.Reader)
		if err != nil {
			return failed(err)
		}
		drafts, said = append(drafts, act), said+fmt.Sprintf("workspace %s applied as version one\n", ref)
	case len(drafts) == 0:
		return written(io.WriteString(r.p.Out, "no workspace version is in force: an owner applies one with lit2 config commit\n"))
	default:
		ref = git.HashBlob(so.Format, assets.DefaultWorkspace.Bytes())
	}
	if founds {
		return o.found(shared, tree, drafts, ref, said)
	}
	return o.judged(drafts, ref, said)
}

// versionOne is the file in the work tree, the default profile created
// there first when there is none; read back, so a race to create it
// applies the winner's.
func versionOne(tree store.Files) ([]byte, error) {
	if _, err := tree.CreateOnce(verbs.WorkspaceFile, assets.DefaultWorkspace.Bytes()); err != nil {
		return nil, fmt.Errorf("writing %s: %w", verbs.WorkspaceFile, err)
	}
	b, err := read(tree, verbs.WorkspaceFile, func(b []byte) ([]byte, error) { return b, nil })
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", verbs.WorkspaceFile, err)
	}
	return b, nil
}

// workspaceFailure is a run that cannot read the workspace's version, its
// file or the code repository: the environment's.
type workspaceFailure struct{ err error }

func (f workspaceFailure) Error() string     { return f.err.Error() }
func (workspaceFailure) Class() reason.Class { return reason.ClassEnvironment }
func (workspaceFailure) Reason() string      { return "workspace" }

// commitRefused is a file config commit will not apply until a step is
// taken, the step named.
type commitRefused struct{ why string }

func (c commitRefused) Error() string     { return c.why }
func (commitRefused) Class() reason.Class { return reason.ClassRefused }
func (commitRefused) Reason() string      { return "workspace" }
