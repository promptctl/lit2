package store_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// clone is one clone's store as the suite reaches it: the ports a Store
// opens over, and, read from those ports by the store's own test seams,
// the files of its logs, for the subtests that write a log's raw bytes,
// and lit's repository, so neither can disagree with the ports.
type clone struct {
	ports   store.Ports
	logs    files.Files
	objects git.Objects
	refs    git.Refs
}

func cloneOf(p store.Ports) clone {
	objects, refs := store.RepositoryOf(p)
	return clone{ports: p, logs: store.FilesOf(p), objects: objects, refs: refs}
}

// fresh is a fresh clone's store in process, empty.
func fresh() clone { return cloneOf(store.MemoryPorts(filestest.Memory(), storetest.Entropy(1))) }

// fetched is a fresh clone's store that fetched c's lit repository and
// holds none of c's logs.
func fetched(c clone) clone { return cloneOf(store.WithFiles(c.ports, filestest.Memory())) }

var (
	a = store.CheckoutID{0xa}
	b = store.CheckoutID{0xb}
)

// stale fails t unless err is the refusal of a publish whose ref moved
// since its seal to the commit now.
func stale(t *testing.T, err error, ref store.Ref, now git.ObjectID) {
	t.Helper()
	var moved git.Moved
	if !errors.As(err, &moved) || moved.Name != ref.Name() || moved.Now != (git.Named{Target: now}) {
		t.Errorf("the stale publish failed with %v; want git.Moved naming %s at %s", err, ref.Name(), now)
	}
}

// segments is the publication a commit holds, as its tree's entries.
func segments(t *testing.T, objects git.Objects, commit git.ObjectID) []git.Entry {
	t.Helper()
	c, err := objects.Commit(commit)
	if err != nil {
		t.Fatal(err)
	}
	es, err := objects.Tree(c.Tree)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// TestStore holds the store to its behavior, over the memory twin, whose
// ports the files and git port contracts hold to the disk.
func TestStore(t *testing.T) {
	t.Run("a fresh common dir reads zero refs, zero logs and no stream", func(t *testing.T) {
		t.Parallel()
		s := store.Open(a, fresh().ports, store.DefaultSegmentBound)
		h := storetest.Heads(t, s)
		if h.RefsRead != 0 || h.LogsRead != 0 || len(h.Streams) != 0 {
			t.Errorf("a fresh store's heads are %+v; want zero refs, zero logs, no stream", h)
		}
		if _, err := s.SealOwn(); !errors.As(err, new(files.NotFound)) {
			t.Errorf("sealing with no log failed with %v; want NotFound", err)
		}
	})

	t.Run("appended batches read back in order, each whole, at increasing offsets", func(t *testing.T) {
		t.Parallel()
		s := store.Open(a, fresh().ports, store.DefaultSegmentBound)
		bs := []store.Batch{storetest.Batch(t, "one"), storetest.Batch(t, "two", "three"), storetest.Batch(t, "four", "five", "six")}
		appended := storetest.AppendAll(t, s, bs...)
		first, created := appended[0].Log.(store.LogCreated)
		if !created || first.Made != files.Created || appended[1].Log != (store.LogLookedUp{}) || appended[2].Records != 3 {
			t.Errorf("the appends were %+v; want the first to create the log and the next to look it up", appended)
		}
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: a})
		if len(scan.Frames) != len(bs) || len(scan.Torn) != 0 || scan.End != scan.Until || scan.End != appended[2].Until.At {
			t.Fatalf("the log reads %+v; want %d whole frames ending where the last append ended", scan, len(bs))
		}
		for i, f := range scan.Frames {
			if !slices.Equal(storetest.Texts(f.Batch), storetest.Texts(bs[i])) || i > 0 && f.At <= scan.Frames[i-1].At {
				t.Errorf("frame %d is %v at %d; want %v after the frame before it", i, storetest.Texts(f.Batch), f.At, storetest.Texts(bs[i]))
			}
		}
	})

	t.Run("a partial frame is not read or consumed, and is one torn span once a frame follows it", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "before"))
		partial := store.Encode(storetest.Batch(t, "lost in a crash"))
		partial = partial[:len(partial)/2]
		filestest.Append(t, p.logs, store.LogName(a), partial)
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: a})
		if !slices.Equal(storetest.Read(scan), []string{"before"}) || len(scan.Torn) != 0 || uint64(scan.Until-scan.End) != uint64(len(partial)) {
			t.Errorf("with a partial frame the log reads %+v; want the whole frame and the rest an incomplete tail", scan)
		}
		storetest.AppendAll(t, s, storetest.Batch(t, "after"))
		scan = storetest.Whole(t, s, store.WriterRef{Checkout: a})
		if !slices.Equal(storetest.Read(scan), []string{"before", "after"}) || len(scan.Torn) != 1 || scan.Torn[0].Len() != uint64(len(partial)) || scan.End != scan.Until {
			t.Errorf("after one more append the log reads %+v; want both records and one torn span of %d bytes", scan, len(partial))
		}
	})

	t.Run("many appenders to one log lose no record", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		var want []string
		var wg sync.WaitGroup
		for g := range 8 {
			bs := storetest.Numbered(t, 25, fmt.Sprintf("g%d", g))
			want = append(want, storetest.All(bs)...)
			wg.Go(func() {
				s := store.Open(a, p.ports, store.DefaultSegmentBound)
				for _, bt := range bs {
					if got, ok := s.Append(bt).(store.Appended); !ok {
						t.Errorf("Append = %#v; want Appended", got)
						return
					}
				}
			})
		}
		wg.Wait()
		scan := storetest.Whole(t, store.Open(a, p.ports, store.DefaultSegmentBound), store.WriterRef{Checkout: a})
		got := storetest.Read(scan)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || len(scan.Torn) != 0 {
			t.Errorf("%d concurrent appends read back %d records and %d torn spans; want each record once, none torn", len(want), len(got), len(scan.Torn))
		}
	})

	t.Run("many appends publish as one commit whose stream reads back the log", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		bs := storetest.Numbered(t, 1000, "event")
		storetest.AppendAll(t, s, bs...)
		out := storetest.PublishOwn(t, s)
		if out.Move != (store.RefCreated{}) || out.Prior != (store.NoPrior{}) || out.Kept != 0 || out.Sealed == 0 || len(out.New) != out.Sealed+2 {
			t.Errorf("the publish was %+v; want a created ref, no prior, and only its segments, one tree and one commit new", out)
		}
		// a clone that fetched lit's repository and holds no log of a
		other := fetched(p)
		reader := store.Open(b, other.ports, store.DefaultSegmentBound)
		scan := storetest.Whole(t, reader, store.WriterRef{Checkout: a})
		if !slices.Equal(storetest.Read(scan), storetest.All(bs)) || len(scan.Torn) != 0 || scan.End != scan.Until {
			t.Errorf("the publication reads back %d records; want the log's %d", len(storetest.Read(scan)), len(bs))
		}
	})

	t.Run("a second publish sends only the open segment's delta, and an idle one nothing", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, 256)
		storetest.AppendAll(t, s, storetest.Numbered(t, 30, "first")...)
		first := storetest.PublishOwn(t, s)
		before := segments(t, p.objects, first.Commit)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "more")...)
		second := storetest.PublishOwn(t, s)
		after := segments(t, p.objects, second.Commit)
		if len(before) < 2 || second.Kept != len(before)-1 || len(second.New) != second.Sealed+2 ||
			second.Move != (store.RefReplaced{Previous: first.Commit}) || second.Prior != (store.KeptFrom{Commit: first.Commit}) {
			t.Errorf("the second publish was %+v over %d segments; want the %d full ones kept, the rest, a tree and a commit new", second, len(before), len(before)-1)
		}
		if !slices.Equal(after[:len(before)-1], before[:len(before)-1]) {
			t.Errorf("the full segments changed: %v, then %v", before, after)
		}
		idle := storetest.PublishOwn(t, s)
		if idle.Move != (store.RefUnchanged{}) || len(idle.New) != 0 || idle.NewBytes != 0 || idle.Commit != second.Commit {
			t.Errorf("an idle publish was %+v; want the ref unchanged and nothing new", idle)
		}
	})

	t.Run("a publication sealed before another publish of its log is refused, and the ref keeps the later one", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "first")...)
		early := storetest.SealOwn(t, s)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "more")...)
		later := storetest.PublishOwn(t, s)
		ref := store.WriterRef{Checkout: a}
		_, err := s.Publish(early)
		stale(t, err, ref, later.Commit)
		got, err := p.refs.List(ref.Name())
		if err != nil {
			t.Fatal(err)
		}
		if want := []git.RefHead{{Name: ref.Name(), Target: later.Commit}}; !slices.Equal(got.Heads, want) {
			t.Errorf("after the refused publish the ref is %v; want the later publish's %v", got, want)
		}
	})

	t.Run("two publications of one sealed state both succeed, converging on one commit", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "event")...)
		first, second := storetest.SealOwn(t, s), storetest.SealOwn(t, s)
		one, err := s.Publish(first)
		if err != nil {
			t.Fatal(err)
		}
		two, err := s.Publish(second)
		if err != nil || two.Commit != one.Commit || two.Move != (store.RefUnchanged{}) {
			t.Errorf("the second publish of one state was %+v, %v; want it unchanged at %s", two, err, one.Commit)
		}
	})

	t.Run("an idle publication sealed before another publish moved its ref is refused, not reported unchanged", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "event")...)
		storetest.PublishOwn(t, s)
		idle := storetest.SealOwn(t, s)
		storetest.AppendAll(t, s, storetest.Numbered(t, 1, "more")...)
		later := storetest.PublishOwn(t, s)
		_, err := s.Publish(idle)
		stale(t, err, store.WriterRef{Checkout: a}, later.Commit)
	})

	t.Run("a log with no whole frame has nothing to publish, and makes no ref", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "one"))
		size, err := p.logs.Size(store.LogName(a))
		if err != nil {
			t.Fatal(err)
		}
		header, err := p.logs.ReadAt(store.LogName(a), 0, size-uint64(len(store.Encode(storetest.Batch(t, "one")))))
		if err != nil {
			t.Fatal(err)
		}
		// the header and half a frame: what a crash in the first append leaves
		half := store.Encode(storetest.Batch(t, "half"))
		crashed := fresh()
		filestest.Create(t, crashed.logs, store.LogName(a), append(header, half[:len(half)/2]...))
		s = store.Open(a, crashed.ports, store.DefaultSegmentBound)
		sealed, err := s.SealOwn()
		if want := (store.NothingToPublish{Ref: store.WriterRef{Checkout: a}, Log: storetest.Heads(t, s).Streams[0].Until.Log}); err != nil || sealed != want {
			t.Errorf("sealing a log with no whole frame made %#v, %v; want %#v", sealed, err, want)
		}
		if refs, err := crashed.refs.List(store.Namespace); err != nil || len(refs.Heads)+len(refs.Broken) != 0 {
			t.Errorf("with nothing to publish the refs are %v, %v; want none", refs, err)
		}
	})

	t.Run("a ref whose publication does not read is resealed from 0, and says why", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, 64)
		bs := storetest.Numbered(t, 10, "event")
		storetest.AppendAll(t, s, bs...)
		ref := store.WriterRef{Checkout: a}
		held, err := gittest.Put(p.objects, []byte("not a commit"))
		if err != nil {
			t.Fatal(err)
		}
		if err := p.refs.Replace(ref.Name(), git.Absent{}, held); err != nil {
			t.Fatal(err)
		}
		out := storetest.PublishOwn(t, s)
		resealed, ok := out.Prior.(store.Resealed)
		var none store.NoPublication
		var notLit git.NotLit
		if !ok || !errors.As(resealed.Why, &none) || none.Commit != held || !errors.As(resealed.Why, &notLit) || notLit.ID != held ||
			out.Kept != 0 || out.Move != (store.RefReplaced{Previous: held}) {
			t.Errorf("over a ref holding %s the publish was %+v; want it resealed from 0 for NoPublication of %s, not lit's, the ref replaced", held, out, held)
		}
		// a clone that fetched lit's repository and holds no log of a
		other := fetched(p)
		if got := storetest.Read(storetest.Whole(t, store.Open(b, other.ports, 64), ref)); !slices.Equal(got, storetest.All(bs)) {
			t.Errorf("the resealed publication reads %v; want the log's %v", got, storetest.All(bs))
		}
	})

	t.Run("a log shorter than its own publication is refused, not sealed", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "one"))
		size, err := p.logs.Size(store.LogName(a))
		if err != nil {
			t.Fatal(err)
		}
		short, err := p.logs.ReadAt(store.LogName(a), 0, size)
		if err != nil {
			t.Fatal(err)
		}
		storetest.AppendAll(t, s, storetest.Batch(t, "two"))
		storetest.PublishOwn(t, s)
		// the same repository beside a log holding only the first batch
		rewound := fetched(p)
		filestest.Create(t, rewound.logs, store.LogName(a), short)
		if _, err := store.Open(a, rewound.ports, store.DefaultSegmentBound).SealOwn(); !errors.As(err, new(store.ShorterThanPublished)) {
			t.Errorf("sealing a log shorter than its publication failed with %v; want ShorterThanPublished", err)
		}
	})

	t.Run("an inbox publication reads like a writer's, and its rewrite reads in its place", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(a, p.ports, store.DefaultSegmentBound)
		inbox := store.InboxRef{Inbox: store.InboxID{1}}
		publish := func(from git.Was, log store.LogID, records ...string) store.Published {
			t.Helper()
			pub, err := store.NewPublication(inbox, from, log, []store.Segment{store.Fresh{Start: 0, Bytes: store.Encode(storetest.Batch(t, records...))}})
			if err != nil {
				t.Fatal(err)
			}
			out, err := s.Publish(pub)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		filed := publish(git.Absent{}, store.LogID{1}, "filed", "remotely")
		if got := storetest.Read(storetest.Whole(t, s, inbox)); filed.Move != (store.RefCreated{}) || !slices.Equal(got, []string{"filed", "remotely"}) {
			t.Errorf("the inbox reads %v after %+v; want its two records", got, filed)
		}
		rewrite := publish(git.Named{Target: filed.Commit}, store.LogID{2}, "filed")
		if rewrite.Move != (store.RefReplaced{Previous: filed.Commit}) {
			t.Errorf("the rewrite's publish was %+v; want it to replace the ref", rewrite)
		}
		if got := storetest.Read(storetest.Whole(t, s, inbox)); !slices.Equal(got, []string{"filed"}) {
			t.Errorf("after a rewrite the inbox reads %v; want only what the rewrite holds", got)
		}
	})

	t.Run("two checkouts over one common dir write their own logs and refs and read each other", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		sa, sb := store.Open(a, p.ports, store.DefaultSegmentBound), store.Open(b, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, sa, storetest.Batch(t, "from a"))
		storetest.AppendAll(t, sb, storetest.Batch(t, "from b"))
		for _, s := range []*store.Store{sa, sb} {
			h := storetest.Heads(t, s)
			if h.LogsRead != 2 || len(h.Streams) != 2 ||
				!slices.Equal(storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: a})), []string{"from a"}) ||
				!slices.Equal(storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: b})), []string{"from b"}) {
				t.Errorf("a checkout sees %+v; want both logs, each with its own record", h)
			}
		}
		storetest.PublishOwn(t, sa)
		storetest.PublishOwn(t, sb)
		l, err := p.refs.List(store.Namespace)
		refs := l.Heads
		if err != nil || len(refs) != 2 || refs[0].Name != (store.WriterRef{Checkout: a}).Name() || refs[1].Name != (store.WriterRef{Checkout: b}).Name() {
			t.Errorf("after both publish the refs are %v, %v; want one per checkout", refs, err)
		}
		if h := storetest.Heads(t, sa); h.RefsRead != 2 || len(h.Streams) != 2 {
			t.Errorf("after both publish a checkout sees %+v; want two refs read and two streams, each from its log", h)
		}
	})
}

// meeting is logs whose first two reads of a log that is not there wait
// for each other, so two first appends both find no log and both create
// one: the race forced, never left to the scheduler
// `[LAW:no-ambient-temporal-coupling]`.
type meeting struct {
	files.Files
	met     sync.WaitGroup
	arrived atomic.Int32
}

func (m *meeting) ReadAt(n files.Name, off, size uint64) ([]byte, error) {
	b, err := m.Files.ReadAt(n, off, size)
	if errors.As(err, new(files.NotFound)) && m.arrived.Add(1) <= 2 {
		m.met.Done()
		m.met.Wait()
	}
	return b, err
}

func TestTwoFirstAppendsRacingKeepOneLog(t *testing.T) {
	logs := &meeting{Files: filestest.Memory()}
	logs.met.Add(2)
	p := store.MemoryPorts(logs, storetest.Entropy(1))
	records := []string{"first", "second"}
	got := make([]store.Appending, len(records))
	var wg sync.WaitGroup
	for i, r := range records {
		wg.Go(func() { got[i] = store.Open(a, p, store.DefaultSegmentBound).Append(storetest.Batch(t, r)) })
	}
	wg.Wait()
	s := store.Open(a, p, store.DefaultSegmentBound)
	h := storetest.Heads(t, s)
	if len(h.Streams) != 1 {
		t.Fatalf("after two first appends the heads are %+v; want one stream", h)
	}
	made := map[files.Made]int{}
	for i, g := range got {
		appended, ok := g.(store.Appended)
		created, isCreate := appended.Log.(store.LogCreated)
		if !ok || !isCreate || appended.Until.Log != h.Streams[0].Until.Log {
			t.Errorf("append %d = %#v; want Appended, by a create, at the log the header names, %v", i, g, h.Streams[0].Until.Log)
		}
		made[created.Made]++
	}
	if made[files.Created] != 1 || made[files.Existing] != 1 {
		t.Errorf("the two creates made %v; want one Created and one Existing", made)
	}
	read := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: a}))
	slices.Sort(read)
	if !slices.Equal(read, []string{"first", "second"}) {
		t.Errorf("the log reads %v; want both records", read)
	}
}
