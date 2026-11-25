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
// emitter `[LAW:single-enforcer]`. A step that did not run left nothing,
// and a fresh store's heads are a block of zeros `[LAW:nothing-unseen]`.
// Whether the store opened is the invocation's place, not a step's
// result: see OpeningAt.
type StoreFacts struct {
	Heads     []store.Heads
	Reads     []store.Scanned
	Folds     []fold.FoldFacts
	Appends   []store.Appending
	Publishes []store.Publishing
	Creates   []Creation
	Temps     []Leftovers
}

// Leftovers is what one directory of the checkout's create-once files, or
// its store's logs' directory, was found to hold of the temps creates
// left, which no create of their name has reaped since: a crash's, said on
// every command until a create reaps them `[LAW:no-silent-failure]`. A
// directory is scanned, its temps none or some, or not scanned, and why,
// so a directory with no temp is never said as one nobody looked in.
//
//sumtype:decl
type Leftovers interface{ leftovers() }

// TempsScanned is the directory at Dir, listed: the temps it holds, none
// or some.
type TempsScanned struct {
	Dir   string
	Temps []string
}

// TempsNotScanned is the directory at Dir, whose temps were not read, and
// why.
type TempsNotScanned struct {
	Dir string
	Why error
}

func (TempsScanned) leftovers()    {}
func (TempsNotScanned) leftovers() {}

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
	return slices.ContainsFunc(f.Appends, landed)
}

func landed(a store.Appending) (frame bool) {
	switch a.(type) {
	case store.Appended:
		frame = true
	case store.AppendedNotDurable:
		frame = true
	case store.NotAppended:
		frame = false
	}
	return frame
}

// The store block as the line spells it: the opening, one object per step
// that ran, and the reads summed, with torn bytes per ref.
type (
	storeLine struct {
		Open    openLine      `json:"open"`
		Heads   []headsLine   `json:"heads,omitempty"`
		Read    *readLine     `json:"read,omitempty"`
		Append  []appendRow   `json:"append,omitempty"`
		Publish []publishLine `json:"publish,omitempty"`
		Create  []createRow   `json:"create,omitempty"`
		Temps   []tempsRow    `json:"temps,omitempty"`
	}
	// placedLine is a create that placed its file, durable: outcome
	// placed.
	placedLine struct {
		Path    string `json:"path"`
		Outcome string `json:"outcome"`
		Made    string `json:"made"`
		reapLine
	}
	// placedNotDurableLine is a create that placed its file and whose
	// sync failed: outcome not_durable, and why.
	placedNotDurableLine struct {
		Path    string `json:"path"`
		Outcome string `json:"outcome"`
		Made    string `json:"made"`
		Why     string `json:"why"`
		reapLine
	}
	// notPlacedLine is a create that placed nothing: outcome not_placed,
	// why, and its own temp when it could not remove it.
	notPlacedLine struct {
		Path      string     `json:"path"`
		Outcome   string     `json:"outcome"`
		Why       string     `json:"why"`
		TempsLeft []leftLine `json:"temps_left"`
	}
	// reapLine is what a create did about its name's temps: those it
	// removed, those it could not, and why, and why it could not list them.
	reapLine struct {
		TempsReaped []string   `json:"temps_reaped"`
		TempsLeft   []leftLine `json:"temps_left"`
		Unlisted    string     `json:"unlisted,omitempty"`
	}
	// logLine is how an append came by its log: looked_up; created, its
	// header the log's; existing, a racing create's header the log's; or
	// none, and the reap of its create, or the temp its create left. Its
	// log_sync is the sync of the log's name: synced, not_durable with
	// log_sync_why, or none when it placed no log.
	logLine struct {
		Log        string `json:"log"`
		LogSync    string `json:"log_sync"`
		LogSyncWhy string `json:"log_sync_why,omitempty"`
		reapLine
	}
	// scannedLine is a directory listed: scan scanned, and its temps,
	// none or some.
	scannedLine struct {
		Dir   string   `json:"dir"`
		Scan  string   `json:"scan"`
		Temps []string `json:"temps"`
	}
	// notScannedLine is a directory whose temps were not read: scan
	// not_scanned, and why.
	notScannedLine struct {
		Dir   string `json:"dir"`
		Scan  string `json:"scan"`
		Error string `json:"error"`
	}
	leftLine struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	// openLine is the store's opening. Why a store did not open is the
	// place block's to say, so it is said once.
	openLine struct {
		State string `json:"state"`
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
	// appendedLine is an append that landed its frame, durable: outcome
	// appended.
	appendedLine struct {
		Outcome  string `json:"outcome"`
		Records  int    `json:"records"`
		Bytes    int    `json:"bytes"`
		LogUntil uint64 `json:"log_until"`
		logLine
	}
	// appendedNotDurableLine is an append that landed its frame and whose
	// sync failed: outcome not_durable, and why.
	appendedNotDurableLine struct {
		Outcome  string `json:"outcome"`
		Records  int    `json:"records"`
		Bytes    int    `json:"bytes"`
		LogUntil uint64 `json:"log_until"`
		Why      string `json:"why"`
		logLine
	}
	// notAppendedLine is an append that wrote no frame: outcome
	// not_appended, and why.
	notAppendedLine struct {
		Outcome string `json:"outcome"`
		Why     string `json:"why"`
		logLine
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

// appendRow is one append as the line spells it; its outcome says which
// `[LAW:types-are-the-program]`.
//
//sumtype:decl
type appendRow interface{ appendRow() }

// createRow is one create as the line spells it; its outcome says which.
//
//sumtype:decl
type createRow interface{ createRow() }

// tempsRow is one directory's temps as the line spells it; its scan says
// which.
//
//sumtype:decl
type tempsRow interface{ tempsRow() }

func (appendedLine) appendRow()           {}
func (appendedNotDurableLine) appendRow() {}
func (notAppendedLine) appendRow()        {}
func (placedLine) createRow()             {}
func (placedNotDurableLine) createRow()   {}
func (notPlacedLine) createRow()          {}
func (scannedLine) tempsRow()             {}
func (notScannedLine) tempsRow()          {}

// storeBlock is the facts as the line's store block, opened at p or not.
func storeBlock(p Place, f StoreFacts) storeLine {
	l := storeLine{Open: open(p)}
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
	for _, a := range f.Appends {
		l.Append = append(l.Append, appendOf(a))
	}
	for _, p := range f.Publishes {
		l.Publish = append(l.Publish, publish(p))
	}
	for _, c := range f.Creates {
		l.Create = append(l.Create, createOf(c))
	}
	for _, left := range f.Temps {
		l.Temps = append(l.Temps, tempsOf(left))
	}
	return l
}

// tempsOf is a directory's temps: scanned, and those it holds, or not
// scanned, and why. Each builder below is a switch over its sum that
// gochecksumtype holds exhaustive, so no variant goes unspelled.
func tempsOf(l Leftovers) (row tempsRow) {
	switch l := l.(type) {
	case TempsScanned:
		row = scannedLine{Dir: l.Dir, Scan: "scanned", Temps: append([]string{}, l.Temps...)}
	case TempsNotScanned:
		row = notScannedLine{Dir: l.Dir, Scan: "not_scanned", Error: l.Why.Error()}
	}
	return row
}

// appendOf is an append as the line spells it: appended, not_durable with
// the failed sync, or not_appended and why, each with how it came by its
// log.
func appendOf(a store.Appending) (row appendRow) {
	switch a := a.(type) {
	case store.Appended:
		row = appendedLine{Outcome: "appended", Records: a.Records, Bytes: a.Bytes, LogUntil: uint64(a.Until.At), logLine: logOf(a.Log)}
	case store.AppendedNotDurable:
		row = appendedNotDurableLine{Outcome: "not_durable", Records: a.Records, Bytes: a.Bytes, LogUntil: uint64(a.Until.At), Why: a.Why.Error(), logLine: logOf(a.Log)}
	case store.NotAppended:
		row = notAppendedLine{Outcome: "not_appended", Why: a.Why.Error(), logLine: logOf(a.Log)}
	}
	return row
}

// logOf is how an append came by its log, and the sync of its name.
func logOf(l store.LogAttempt) (line logLine) {
	switch l := l.(type) {
	case store.NoLog:
		line = logLine{Log: "none", LogSync: "none", reapLine: reapLine{TempsReaped: []string{}, TempsLeft: leftOf(l.Left)}}
	case store.LogLookedUp:
		line = placedLog(l)
	case store.LogCreated:
		line = placedLog(l)
	case store.LogNotDurable:
		line = placedLog(l.Log)
		line.LogSync, line.LogSyncWhy = "not_durable", l.Why.Error()
	}
	return line
}

// placedLog is a log looked up or created, its name's sync passing.
func placedLog(l store.LogPlaced) (line logLine) {
	switch l := l.(type) {
	case store.LogLookedUp:
		line = logLine{Log: "looked_up", reapLine: reapOf(files.Reap{})}
	case store.LogCreated:
		line = logLine{Log: made(l.Made), reapLine: reapOf(l.Reap)}
	}
	line.LogSync = "synced"
	return line
}

// createOf is a create as the line spells it: placed, not_durable with the
// failed sync, or not_placed and why.
func createOf(c Creation) (row createRow) {
	switch p := c.Placing.(type) {
	case files.Placed:
		row = placedLine{Path: c.Path, Outcome: "placed", Made: made(p.Made), reapLine: reapOf(p.Reap)}
	case files.PlacedNotDurable:
		row = placedNotDurableLine{Path: c.Path, Outcome: "not_durable", Made: made(p.Made), Why: p.Why.Error(), reapLine: reapOf(p.Reap)}
	case files.NotPlaced:
		row = notPlacedLine{Path: c.Path, Outcome: "not_placed", Why: p.Why.Error(), TempsLeft: leftOf(p.Left)}
	}
	return row
}

func reapOf(r files.Reap) reapLine {
	l := reapLine{TempsReaped: append([]string{}, r.Reaped...), TempsLeft: leftOf(r.Left)}
	if r.Unlisted != nil {
		l.Unlisted = r.Unlisted.Error()
	}
	return l
}

func leftOf(ls []files.Leftover) []leftLine {
	l := []leftLine{}
	for _, left := range ls {
		l = append(l, leftLine{Name: left.Name, Why: left.Why.Error()})
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

// open is the opening of a store at p as the line spells it.
func open(p Place) (line openLine) {
	switch OpeningAt(p).(type) {
	case Opens:
		line = openLine{State: "opened"}
	case NoStore:
		line = openLine{State: "unopened"}
	}
	return line
}
