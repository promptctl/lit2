package command

import (
	"fmt"
	"maps"
	"slices"

	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// StoreFacts is what the store's steps, and the folds over what they read,
// returned during one invocation, in the order they ran. Neither the store
// nor the fold emits anything itself: their results carry their facts, and
// the body that called them hands them here, so the event keeps one
// emitter `[LAW:single-enforcer]`. The root records the store's opening
// for every invocation, opened or why not; any other step that did not run
// left nothing, and a fresh store's heads are a block of zeros
// `[LAW:nothing-unseen]`.
type StoreFacts struct {
	Opening   Opening
	Heads     []store.Heads
	Reads     []Read
	Plans     []store.Resume
	Folds     []fold.FoldFacts
	Swaps     []store.Swapped
	Appends   []store.Appended
	Publishes []store.Publishing
}

// Read is one range a fold read and what the scan found in it.
type Read struct {
	Range store.Range
	Scan  store.Scan
}

// Opening is the store an invocation's checkout opened, once, or why it has
// none.
//
//sumtype:decl
type Opening interface{ opening() }

// Opened is a minted checkout's store, and its code repository's object
// format, read once beside it for the workspace file's config-ref.
type Opened struct {
	Store  *store.Store
	Format git.ObjectFormat
}

// Unopened is a place with no store: outside a checkout, in one not yet
// minted, or one whose writer or object format cannot be read.
type Unopened struct{ Why error }

func (Opened) opening()   {}
func (Unopened) opening() {}

func (f StoreFacts) empty() bool {
	return f.Opening == nil && len(f.Heads)+len(f.Reads)+len(f.Plans)+len(f.Folds)+len(f.Swaps)+len(f.Appends)+len(f.Publishes) == 0
}

// The store block as the line spells it: one object per step that ran,
// and the reads summed over the delta, with torn bytes per ref.
type (
	storeLine struct {
		Open    *openLine     `json:"open,omitempty"`
		Heads   []headsLine   `json:"heads,omitempty"`
		Read    *readLine     `json:"read,omitempty"`
		Plan    []planLine    `json:"plan,omitempty"`
		Swap    []swapLine    `json:"swap,omitempty"`
		Append  []appendLine  `json:"append,omitempty"`
		Publish []publishLine `json:"publish,omitempty"`
	}
	openLine struct {
		State        string `json:"state"`
		ObjectFormat string `json:"object_format,omitempty"`
		Why          string `json:"why,omitempty"`
	}
	headsLine struct {
		RefsRead   int             `json:"refs_read"`
		LogsRead   int             `json:"logs_read"`
		Streams    int             `json:"streams"`
		Unreadable []badStreamLine `json:"unreadable"`
	}
	badStreamLine struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	readLine struct {
		FramesRead          int        `json:"frames_read"`
		BytesRead           uint64     `json:"bytes_read"`
		IncompleteTailBytes uint64     `json:"incomplete_tail_bytes"`
		Torn                []tornLine `json:"torn"`
	}
	tornLine struct {
		Ref   string `json:"ref"`
		Spans int    `json:"torn_spans"`
		Bytes uint64 `json:"torn_bytes"`
	}
	planLine struct {
		Snapshot string `json:"snapshot"`
		Ref      string `json:"ref,omitempty"`
		Why      string `json:"why,omitempty"`
		Delta    int    `json:"delta"`
	}
	swapLine struct {
		SnapshotBytes int `json:"snapshot_bytes"`
	}
	appendLine struct {
		Records    int    `json:"records"`
		Bytes      int    `json:"bytes"`
		LogUntil   uint64 `json:"log_until"`
		LogCreated bool   `json:"log_created"`
	}
	publishLine struct {
		Outcome        string `json:"outcome"`
		SegmentsKept   int    `json:"segments_kept"`
		SegmentsSealed int    `json:"segments_sealed"`
		ObjectsNew     int    `json:"objects_new"`
		BytesNew       uint64 `json:"bytes_new"`
		RefMove        string `json:"ref_move"`
		Previous       string `json:"previous,omitempty"`
		Commit         string `json:"commit,omitempty"`
		Prior          string `json:"prior"`
		PriorCommit    string `json:"prior_commit,omitempty"`
		PriorWhy       string `json:"prior_why,omitempty"`
	}
)

// storeBlock is the facts as the line's store block, or nil when no store
// step ran.
func storeBlock(f StoreFacts) *storeLine {
	if f.empty() {
		return nil
	}
	l := &storeLine{Open: open(f.Opening)}
	for _, h := range f.Heads {
		hl := headsLine{RefsRead: h.RefsRead, LogsRead: h.LogsRead, Streams: len(h.Streams), Unreadable: []badStreamLine{}}
		for _, u := range h.Unreadable {
			hl.Unreadable = append(hl.Unreadable, badStreamLine{Name: u.Name, Why: u.Why.Error()})
		}
		l.Heads = append(l.Heads, hl)
	}
	if len(f.Reads) > 0 {
		l.Read = reads(f.Reads)
	}
	for _, r := range f.Plans {
		l.Plan = append(l.Plan, plan(r))
	}
	for _, s := range f.Swaps {
		l.Swap = append(l.Swap, swapLine{SnapshotBytes: s.Bytes})
	}
	for _, a := range f.Appends {
		l.Append = append(l.Append, appendLine{Records: a.Records, Bytes: a.Bytes, LogUntil: uint64(a.Until.At), LogCreated: created(a.Log)})
	}
	for _, p := range f.Publishes {
		l.Publish = append(l.Publish, publish(p))
	}
	return l
}

// publish is a publish as the line spells it. Nothing to publish touched no
// ref and kept nothing before it.
func publish(p store.Publishing) publishLine {
	switch p := p.(type) {
	case store.Published:
		pl := publishLine{Outcome: "published", SegmentsKept: p.Kept, SegmentsSealed: p.Sealed, ObjectsNew: len(p.New), BytesNew: p.NewBytes, Commit: p.Commit.String()}
		pl.RefMove, pl.Previous = refMove(p.Move)
		pl.Prior, pl.PriorCommit, pl.PriorWhy = prior(p.Prior)
		return pl
	case store.NothingToPublish:
		return publishLine{Outcome: "nothing_to_publish", RefMove: "untouched", Prior: "none"}
	}
	panic(fmt.Sprintf("command: %T is not a Publishing", p))
}

func reads(rs []Read) *readLine {
	l := &readLine{Torn: []tornLine{}}
	torn := map[string]tornLine{}
	for _, r := range rs {
		l.FramesRead += len(r.Scan.Frames)
		l.BytesRead += uint64(r.Scan.Until - r.Range.From)
		l.IncompleteTailBytes += uint64(r.Scan.Until - r.Scan.End)
		name := string(r.Range.Stream.Ref.Name())
		t := torn[name]
		for _, s := range r.Scan.Torn {
			t.Spans++
			t.Bytes += s.Len()
		}
		if t.Spans > 0 {
			t.Ref = name
			torn[name] = t
		}
	}
	for _, name := range slices.Sorted(maps.Keys(torn)) {
		l.Torn = append(l.Torn, torn[name])
	}
	return l
}

// plan is a resume as hit, extended, or refold with its miss.
func plan(r store.Resume) planLine {
	switch b := r.Base.(type) {
	case store.FromSaved:
		if len(r.Delta) == 0 {
			return planLine{Snapshot: "hit"}
		}
		return planLine{Snapshot: "extended", Delta: len(r.Delta)}
	case store.Genesis:
		l := refold(b.Why)
		l.Delta = len(r.Delta)
		return l
	}
	panic(fmt.Sprintf("command: %T is not a Base", r.Base))
}

// refold is a genesis's miss: why the snapshot was not resumed.
func refold(m store.Miss) planLine {
	switch m := m.(type) {
	case store.Absent:
		return planLine{Snapshot: "refold:absent"}
	case store.Torn:
		return planLine{Snapshot: "refold:torn", Why: m.Why.Error()}
	case store.TagChanged:
		return planLine{Snapshot: "refold:tag_changed"}
	case store.Rewritten:
		return planLine{Snapshot: "refold:rewritten", Ref: string(m.Ref.Name())}
	case store.Withdrawn:
		return planLine{Snapshot: "refold:withdrawn", Ref: string(m.Ref.Name())}
	}
	panic(fmt.Sprintf("command: %T is not a Miss", m))
}

func created(o store.Made) bool {
	switch o {
	case store.Created:
		return true
	case store.Existing:
		return false
	}
	panic(fmt.Sprintf("command: %d is not a Made", uint8(o)))
}

func refMove(m store.RefMove) (move, previous string) {
	switch m := m.(type) {
	case store.RefUnchanged:
		return "unchanged", ""
	case store.RefCreated:
		return "created", ""
	case store.RefReplaced:
		return "replaced", m.Previous.String()
	}
	panic(fmt.Sprintf("command: %T is not a RefMove", m))
}

// prior is what a publish kept of the publication before it: kept, none,
// or resealed from 0 and why.
func prior(p store.Prior) (kept, commit, why string) {
	switch p := p.(type) {
	case store.KeptFrom:
		return "kept", p.Commit.String(), ""
	case store.NoPrior:
		return "none", "", ""
	case store.Resealed:
		return "resealed", "", p.Why.Error()
	}
	panic(fmt.Sprintf("command: %T is not a Prior", p))
}

// open is the opening as the line spells it, absent when none was recorded.
func open(o Opening) *openLine {
	switch o := o.(type) {
	case nil:
		return nil
	case Opened:
		return &openLine{State: "opened", ObjectFormat: o.Format.String()}
	case Unopened:
		return &openLine{State: "unopened", Why: o.Why.Error()}
	}
	panic(fmt.Sprintf("command: %T is not an Opening", o))
}
