package verbs

import (
	"fmt"
	"path"
	"strings"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
)

// Admit judges applying version, in the code repository's object format f,
// to st: it parses under running, then passes CheckApply against every
// live version, and is the apply's draft, naming every live apply in Over,
// so it resolves any conflict standing (§3.15). It is the one judge of an
// apply, whether init applies version one, `config commit` the committed
// file, or a later promote a sandbox's last draft. Every fault comes in one
// refusal, none overridable.
func Admit(st fold.State, version []byte, f git.ObjectFormat, running config.ReaderVersion) (events.Act, error) {
	next, err := config.Load(version, running)
	if err != nil {
		return events.Act{}, VersionRefused{Refused: err.(*config.Refused)}
	}
	var live []config.Schema
	var over []events.EventID
	switch w := st.Workspace().(type) {
	case fold.NoVersion:
	case fold.InForce:
		over = w.Live
		for _, v := range liveVersions(w) {
			l, err := config.Load(v.Bytes, running)
			if err != nil {
				return events.Act{}, Unreadable{Blob: v.Blob, Err: err}
			}
			live = append(live, l)
		}
	}
	if faults := evaluate.CheckApply(st, live, next); len(faults) > 0 {
		return events.Act{}, VersionRefused{Refused: &config.Refused{Source: "the workspace version", Refusals: faults}}
	}
	return events.Act{About: events.Root{}, Payload: events.NewWorkspaceApply(version, f, over)}, nil
}

// liveVersions is the active version and each contending one; the live
// applies that agree with the active version are its bytes.
func liveVersions(w fold.InForce) []fold.Version {
	return append([]fold.Version{w.Active}, w.Contending...)
}

// VersionRefused is a version the check refuses: every fault, keyed.
type VersionRefused struct{ Refused *config.Refused }

func (v VersionRefused) Error() string     { return v.Refused.Error() }
func (VersionRefused) Class() reason.Class { return reason.ClassRefused }
func (VersionRefused) Reason() string      { return "workspace" }

// Unreadable is a live version this release does not read, one a newer
// lit2 applied: the environment's, the upgrade being the step.
type Unreadable struct {
	Blob git.ObjectID
	Err  error
}

func (u Unreadable) Error() string {
	return fmt.Sprintf("the live version %s does not read under this release: %v", u.Blob, u.Err)
}
func (Unreadable) Class() reason.Class { return reason.ClassEnvironment }
func (Unreadable) Reason() string      { return "workspace" }

// File is `.lit/workspace.toml` as the root read it this run: git's id for
// its bytes, none, or why it could not be read. It is never parsed, so a
// draft that does not parse stops nothing (§3.15).
//
//sumtype:decl
type File interface{ file() }

// OnDisk is a file and git's id for its bytes.
type OnDisk struct{ Blob git.ObjectID }

// NoFile is a checkout with no `.lit/workspace.toml`.
type NoFile struct{}

// UnreadableFile is a file that is there and did not read.
type UnreadableFile struct{ Err error }

func (OnDisk) file()         {}
func (NoFile) file()         {}
func (UnreadableFile) file() {}

// The work tree's .lit directory and the workspace file in it. A path of
// either, in the work tree or in the code repository's tree, is derived
// from these, never written out `[LAW:one-source-of-truth]`.
var (
	litDir        = files.MustName(".lit")
	workspaceFile = files.MustName("workspace.toml")
)

// LitDir is the work tree's .lit directory's name.
func LitDir() files.Name { return litDir }

// WorkspaceFile is the workspace file's name in .lit.
func WorkspaceFile() files.Name { return workspaceFile }

// LitPath is the path of the file n in .lit from the work tree's root,
// slash-separated, as git's tree and every message name it.
func LitPath(n files.Name) string { return path.Join(litDir.String(), n.String()) }

// WorkspacePath is the workspace file's LitPath.
func WorkspacePath() string { return LitPath(workspaceFile) }

// workspace is the root's workspace row: its one line.
func workspace(w World, _ address.Node) ([]string, error) {
	return []string{Workspace(w.State(), w.File)}, nil
}

// Workspace is the one line on the version in force: its blob, whether the
// file matches it, and every version contending with it. It is the one
// place drift is said, on the board and by config status; no other command
// mentions it.
func Workspace(st fold.State, f File) string {
	in, ok := st.Workspace().(fold.InForce)
	if !ok {
		return "no version in force: lit2 config commit applies one"
	}
	parts := []string{short(in.Active.Blob) + " in force", WorkspacePath() + " " + fileState(in.Active.Blob, f)}
	for _, c := range in.Contending {
		parts = append(parts, fmt.Sprintf("contended by %s, by %s: lit2 config commit resolves", short(c.Blob), writer(c.Stamp.By)))
	}
	return strings.Join(parts, " · ")
}

// fileState is how the file stands against the active blob.
func fileState(active git.ObjectID, f File) string {
	switch f := f.(type) {
	case OnDisk:
		if f.Blob == active {
			return "matches"
		}
		return fmt.Sprintf("differs (%s): commit it, then lit2 config commit", short(f.Blob))
	case NoFile:
		return "is absent"
	case UnreadableFile:
		return fmt.Sprintf("does not read: %v", f.Err)
	}
	panic(fmt.Sprintf("verbs: %T is not a File", f))
}

// short is a blob id as a person reads it, git's default abbreviation.
func short(id git.ObjectID) string { return id.String()[:7] }
