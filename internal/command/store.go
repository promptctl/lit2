package command

import (
	"fmt"
	"maps"
	"slices"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
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
	Reads     []store.Scanned
	Folds     []fold.FoldFacts
	Appends   []store.Appending
	Publishes []store.Publishing
	Creates   []Creation
}

// Creation is one create-once a command ran: the file's path, and what
// the create did, which a durable write and one whose sync failed say
// apart `[LAW:no-silent-failure]`.
type Creation struct {
	Path    string
	Placing files.Placing
}

// Landed is whether any append the facts hold wrote its frame, durable or
// not: what the verbs block calls written.
func (f StoreFacts) Landed() bool {
	for _, a := range f.Appends {
		switch a.(type) {
		case store.Appended, store.AppendedNotDurable:
			return true
		case store.NotAppended:
		}
	}
	return false
}

// Opening is the store an invocation's checkout opened, once, or why it has
// none.
//
//sumtype:decl
type Opening interface{ opening() }

// Opened is a minted checkout's store.
type Opened struct{ Store *store.Store }

// Unopened is a place with no store: outside a checkout, in one not yet
// minted, or one whose writer cannot be read.
type Unopened struct{ Why error }

func (Opened) opening()   {}
func (Unopened) opening() {}

func (f StoreFacts) empty() bool {
	return f.Opening == nil && len(f.Heads)+len(f.Reads)+len(f.Folds)+len(f.Appends)+len(f.Publishes)+len(f.Creates) == 0
}

// The store block as the line spells it: one object per step that ran,
// and the reads summed, with torn bytes per ref.
type (
	storeLine struct {
		Open    *openLine     `json:"open,omitempty"`
		Heads   []headsLine   `json:"heads,omitempty"`
		Read    *readLine     `json:"read,omitempty"`
		Append  []any         `json:"append,omitempty"`
		Publish []publishLine `json:"publish,omitempty"`
		Create  []any         `json:"create,omitempty"`
	}
	// createLine is a create that placed its file, durable or not; a
	// create that placed none is a notPlacedLine.
	createLine struct {
		Path    string `json:"path"`
		Outcome string `json:"outcome"`
		Made    string `json:"made"`
		Why     string `json:"why,omitempty"`
		sweptLine
	}
	notPlacedLine struct {
		Path    string `json:"path"`
		Outcome string `json:"outcome"`
		Why     string `json:"why"`
	}
	// sweptLine is what a create did about its name's other temps: those
	// it removed, and those it could not, and why.
	sweptLine struct {
		TempsReaped []string   `json:"temps_reaped"`
		TempsLeft   []leftLine `json:"temps_left"`
	}
	leftLine struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	openLine struct {
		State string `json:"state"`
		Why   string `json:"why,omitempty"`
	}
	headsLine struct {
		RefsRead   int             `json:"refs_read"`
		LogsRead   int             `json:"logs_read"`
		Streams    int             `json:"streams"`
		Unreadable []badStreamLine `json:"unreadable"`
		Temps      []string        `json:"temps"`
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
	// appendLine is an append that landed its frame, durable or not; an
	// append that wrote none is a notAppendedLine.
	appendLine struct {
		Outcome    string `json:"outcome"`
		Records    int    `json:"records"`
		Bytes      int    `json:"bytes"`
		LogUntil   uint64 `json:"log_until"`
		LogCreated bool   `json:"log_created"`
		Why        string `json:"why,omitempty"`
		sweptLine
	}
	notAppendedLine struct {
		Outcome string `json:"outcome"`
		Why     string `json:"why"`
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
		hl := headsLine{RefsRead: h.RefsRead, LogsRead: h.LogsRead, Streams: len(h.Streams), Unreadable: []badStreamLine{}, Temps: append([]string{}, h.Temps...)}
		for _, u := range h.Unreadable {
			hl.Unreadable = append(hl.Unreadable, badStreamLine{Name: u.Name, Why: u.Why.Error()})
		}
		l.Heads = append(l.Heads, hl)
	}
	if len(f.Reads) > 0 {
		l.Read = reads(f.Reads)
	}
	for _, a := range f.Appends {
		l.Append = append(l.Append, appendOf(a))
	}
	for _, p := range f.Publishes {
		l.Publish = append(l.Publish, publish(p))
	}
	for _, c := range f.Creates {
		l.Create = append(l.Create, createOf(c))
	}
	return l
}

// appendOf is an append as the line spells it: appended, not_durable with
// the failed sync, or not_appended and why.
func appendOf(a store.Appending) any {
	landed := func(outcome string, a store.Appended, why string) appendLine {
		return appendLine{Outcome: outcome, Records: a.Records, Bytes: a.Bytes, LogUntil: uint64(a.Until.At), LogCreated: created(a.Log), Why: why, sweptLine: swept(a.Swept)}
	}
	switch a := a.(type) {
	case store.Appended:
		return landed("appended", a, "")
	case store.AppendedNotDurable:
		return landed("not_durable", a.Appended, a.Why.Error())
	case store.NotAppended:
		return notAppendedLine{Outcome: "not_appended", Why: a.Why.Error()}
	}
	panic(fmt.Sprintf("command: %T is not an Appending", a))
}

// createOf is a create as the line spells it: placed, not_durable with the
// failed sync, or not_placed and why.
func createOf(c Creation) any {
	switch p := c.Placing.(type) {
	case files.Placed:
		return createLine{Path: c.Path, Outcome: "placed", Made: made(p.Made), sweptLine: swept(p.Swept)}
	case files.PlacedNotDurable:
		return createLine{Path: c.Path, Outcome: "not_durable", Made: made(p.Made), Why: p.Why.Error(), sweptLine: swept(p.Swept)}
	case files.NotPlaced:
		return notPlacedLine{Path: c.Path, Outcome: "not_placed", Why: p.Why.Error()}
	}
	panic(fmt.Sprintf("command: %T is not a Placing", c.Placing))
}

func swept(s files.Swept) sweptLine {
	l := sweptLine{TempsReaped: append([]string{}, s.Reaped...), TempsLeft: []leftLine{}}
	for _, left := range s.Left {
		l.TempsLeft = append(l.TempsLeft, leftLine{Name: left.Name, Why: left.Why.Error()})
	}
	return l
}

func made(m files.Made) string {
	if created(m) {
		return "created"
	}
	return "existing"
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

func reads(rs []store.Scanned) *readLine {
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

func created(o files.Made) bool {
	switch o {
	case files.Created:
		return true
	case files.Existing:
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
		return &openLine{State: "opened"}
	case Unopened:
		return &openLine{State: "unopened", Why: o.Why.Error()}
	}
	panic(fmt.Sprintf("command: %T is not an Opening", o))
}
