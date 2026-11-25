package store_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

var (
	ida = store.CheckoutID{0xa}
	idb = store.CheckoutID{0xb}
)

// clone is one clone's store as a test reaches it: the ports a Store
// opens over, and, read from those ports by the store's own test seams,
// the files of its logs, for the subtests that write a log's raw bytes,
// and lit's repository, so the rest never disagrees with the ports.
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

// fresh is a fresh clone's store, its logs and lit's repository in
// memory, empty.
func fresh() clone {
	return cloneOf(store.RepositoryInMemory(filestest.Memory(), storetest.Entropy(1)))
}

// fetched is a fresh clone that fetched c's lit repository and holds none
// of c's logs.
func fetched(c clone) clone { return cloneOf(store.WithFiles(c.ports, filestest.Memory())) }

// stale fails t unless err is the refusal of a publish whose ref moved
// since its seal to the commit now.
func stale(t *testing.T, err error, ref store.Ref, now git.ObjectID) {
	t.Helper()
	var moved git.Moved
	if !errors.As(err, &moved) || moved.Name != ref.Name() || moved.Now != (git.Named{Target: now}) {
		t.Errorf("the stale publish failed with %v; want git.Moved naming %s at %s", err, ref.Name(), now)
	}
}

// torn is lit's repository's refs with one ref, which the twin holds,
// broken, as a ref file git cannot read lists: Broken in every listing the
// twin lists it in, by the twin's own rule of what a prefix holds, and in
// no listing's Heads.
type torn struct {
	git.Refs
	broken git.BrokenRef
}

func (r torn) List(prefix git.RefName) (git.Listing, error) {
	l, err := r.Refs.List(prefix)
	n := len(l.Heads)
	l.Heads = slices.DeleteFunc(l.Heads, func(h git.RefHead) bool { return h.Name == r.broken.Name })
	if len(l.Heads) < n {
		l.Broken = append(l.Broken, r.broken)
	}
	return l, err
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

// TestTheStoresBehavior is the store's behavior, over its twins: logs,
// segments, publications and refs (architecture.md §3.16). The twins are
// the disk's by their port contracts, so this suite runs once.
func TestTheStoresBehavior(t *testing.T) {
	t.Run("a fresh common dir reads zero refs, zero logs and no stream", func(t *testing.T) {
		t.Parallel()
		s := store.Open(ida, fresh().ports, store.DefaultSegmentBound)
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
		s := store.Open(ida, fresh().ports, store.DefaultSegmentBound)
		bs := []store.Batch{storetest.Batch(t, "one"), storetest.Batch(t, "two", "three"), storetest.Batch(t, "four", "five", "six")}
		appended := storetest.AppendAll(t, s, bs...)
		first, created := appended[0].Log.(store.LogCreated)
		if !created || first.Made != files.Created || appended[1].Log != (store.LogLookedUp{}) || appended[2].Records != 3 {
			t.Errorf("the appends were %+v; want the first to create the log and the next to look it up", appended)
		}
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: ida})
		if len(scan.Frames) != len(bs) || len(scan.Torn) != 0 || scan.InFlight().Len() != 0 || scan.End != appended[2].Until.At {
			t.Fatalf("the log reads %+v; want %d whole frames ending where the last append ended", scan, len(bs))
		}
		for i, f := range scan.Frames {
			got, want := storetest.Read(store.Scan{Frames: scan.Frames[i : i+1]}), storetest.All(bs[i:i+1])
			if !slices.Equal(got, want) || i > 0 && f.At <= scan.Frames[i-1].At {
				t.Errorf("frame %d is %v at %d; want %v after the frame before it", i, got, f.At, want)
			}
		}
	})

	t.Run("a partial frame is not read or consumed, and is one torn span once a frame follows it", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "before"))
		partial := store.Encode(storetest.Batch(t, "lost in a crash"))
		partial = partial[:len(partial)/2]
		filestest.Append(t, p.logs, store.LogName(ida), partial)
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: ida})
		if !slices.Equal(storetest.Read(scan), []string{"before"}) || len(scan.Torn) != 0 || scan.InFlight() != (store.Span{From: scan.End, To: scan.End + store.Offset(len(partial))}) {
			t.Errorf("with a partial frame the log reads %+v; want the whole frame and the rest an incomplete tail", scan)
		}
		storetest.AppendAll(t, s, storetest.Batch(t, "after"))
		scan = storetest.Whole(t, s, store.WriterRef{Checkout: ida})
		if !slices.Equal(storetest.Read(scan), []string{"before", "after"}) || len(scan.Torn) != 1 || scan.Torn[0].Len() != uint64(len(partial)) || scan.InFlight().Len() != 0 {
			t.Errorf("after one more append the log reads %+v; want both records and one torn span of %d bytes", scan, len(partial))
		}
	})

	t.Run("many appends publish as one commit whose stream reads back the log", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		bs := storetest.Numbered(t, 50, "event")
		storetest.AppendAll(t, s, bs...)
		out := storetest.PublishOwn(t, s)
		if out.Move != (store.RefCreated{}) || out.Prior != (store.NoPrior{}) || out.Kept != 0 || out.Sealed == 0 || len(out.New) != out.Sealed+2 {
			t.Errorf("the publish was %+v; want a created ref, no prior, and only its segments, one tree and one commit new", out)
		}
		// a clone that fetched lit's repository and holds no log of a
		other := fetched(p)
		reader := store.Open(idb, other.ports, store.DefaultSegmentBound)
		scan := storetest.Whole(t, reader, store.WriterRef{Checkout: ida})
		if !slices.Equal(storetest.Read(scan), storetest.All(bs)) || len(scan.Torn) != 0 || scan.InFlight().Len() != 0 {
			t.Errorf("the publication reads back %d records; want the log's %d", len(storetest.Read(scan)), len(bs))
		}
	})

	t.Run("a second publish sends only the open segment's delta, and an idle one nothing", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, 256)
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
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "first")...)
		early := storetest.SealOwn(t, s)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "more")...)
		later := storetest.PublishOwn(t, s)
		ref := store.WriterRef{Checkout: ida}
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
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
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
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Numbered(t, 3, "event")...)
		storetest.PublishOwn(t, s)
		idle := storetest.SealOwn(t, s)
		storetest.AppendAll(t, s, storetest.Numbered(t, 1, "more")...)
		later := storetest.PublishOwn(t, s)
		_, err := s.Publish(idle)
		stale(t, err, store.WriterRef{Checkout: ida}, later.Commit)
	})

	t.Run("a log with no whole frame has nothing to publish, and makes no ref", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "one"))
		size, err := p.logs.Size(store.LogName(ida))
		if err != nil {
			t.Fatal(err)
		}
		header, err := p.logs.ReadAt(store.LogName(ida), 0, size-uint64(len(store.Encode(storetest.Batch(t, "one")))))
		if err != nil {
			t.Fatal(err)
		}
		// the header and half a frame: what a crash in the first append leaves
		half := store.Encode(storetest.Batch(t, "half"))
		crashed := fresh()
		filestest.Create(t, crashed.logs, store.LogName(ida), append(header, half[:len(half)/2]...))
		s = store.Open(ida, crashed.ports, store.DefaultSegmentBound)
		sealed, err := s.SealOwn()
		if want := (store.NothingToPublish{Ref: store.WriterRef{Checkout: ida}, Log: storetest.Heads(t, s).Streams[0].Until.Log}); err != nil || sealed != want {
			t.Errorf("sealing a log with no whole frame made %#v, %v; want %#v", sealed, err, want)
		}
		if refs, err := crashed.refs.List(store.Namespace); err != nil || len(refs.Heads)+len(refs.Broken) != 0 {
			t.Errorf("with nothing to publish the refs are %v, %v; want none", refs, err)
		}
	})

	t.Run("a ref whose publication does not read is resealed from 0, and says why", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, 64)
		bs := storetest.Numbered(t, 10, "event")
		storetest.AppendAll(t, s, bs...)
		ref := store.WriterRef{Checkout: ida}
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
		if got := storetest.Read(storetest.Whole(t, store.Open(idb, other.ports, 64), ref)); !slices.Equal(got, storetest.All(bs)) {
			t.Errorf("the resealed publication reads %v; want the log's %v", got, storetest.All(bs))
		}
	})

	t.Run("a log shorter than its own publication is refused, not sealed", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, s, storetest.Batch(t, "one"))
		size, err := p.logs.Size(store.LogName(ida))
		if err != nil {
			t.Fatal(err)
		}
		short, err := p.logs.ReadAt(store.LogName(ida), 0, size)
		if err != nil {
			t.Fatal(err)
		}
		storetest.AppendAll(t, s, storetest.Batch(t, "two"))
		storetest.PublishOwn(t, s)
		// the same repository beside a log holding only the first batch
		rewound := fetched(p)
		filestest.Create(t, rewound.logs, store.LogName(ida), short)
		if _, err := store.Open(ida, rewound.ports, store.DefaultSegmentBound).SealOwn(); !errors.As(err, new(store.ShorterThanPublished)) {
			t.Errorf("sealing a log shorter than its publication failed with %v; want ShorterThanPublished", err)
		}
	})

	t.Run("an inbox publication reads like a writer's, and its rewrite reads in its place", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
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

	t.Run("a broken ref is one unreadable stream that hides no other, and its writer's seal says it", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		for _, id := range []store.CheckoutID{ida, idb} {
			s := store.Open(id, p.ports, store.DefaultSegmentBound)
			storetest.AppendAll(t, s, storetest.Batch(t, "from "+id.String()))
			storetest.PublishOwn(t, s)
		}
		broken := git.BrokenRef{Name: store.WriterRef{Checkout: ida}.Name(), Why: `it holds "torn", no object id`}
		refs := torn{Refs: p.refs, broken: broken}
		reader := store.Open(store.CheckoutID{0xff}, store.WithRefs(fetched(p).ports, refs), store.DefaultSegmentBound)
		h, read, err := reader.ReadAll()
		if err != nil || len(h.Unreadable) != 1 || h.Unreadable[0].Name != string(broken.Name) || !errors.As(h.Unreadable[0].Why, new(git.BrokenRef)) || h.RefsRead != 2 ||
			len(read) != 1 || read[0].Range.Stream.Ref != (store.WriterRef{Checkout: idb}) || !slices.Equal(storetest.Read(read[0].Scan), []string{"from " + idb.String()}) {
			t.Errorf("with a's ref broken a reader's heads are %+v and reads %d, %v; want a's ref unreadable, by it, and b's stream read whole", h, len(read), err)
		}
		if _, err := store.Open(ida, store.WithRefs(p.ports, refs), store.DefaultSegmentBound).SealOwn(); !errors.As(err, new(git.BrokenRef)) {
			t.Errorf("a's seal over its broken ref = %v; want the BrokenRef", err)
		}
	})
	t.Run("two checkouts over one common dir write their own logs and refs and read each other", func(t *testing.T) {
		t.Parallel()
		p := fresh()
		sa, sb := store.Open(ida, p.ports, store.DefaultSegmentBound), store.Open(idb, p.ports, store.DefaultSegmentBound)
		storetest.AppendAll(t, sa, storetest.Batch(t, "from a"))
		storetest.AppendAll(t, sb, storetest.Batch(t, "from b"))
		for _, s := range []*store.Store{sa, sb} {
			h := storetest.Heads(t, s)
			if h.LogsRead != 2 || len(h.Streams) != 2 ||
				!slices.Equal(storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: ida})), []string{"from a"}) ||
				!slices.Equal(storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: idb})), []string{"from b"}) {
				t.Errorf("a checkout sees %+v; want both logs, each with its own record", h)
			}
		}
		storetest.PublishOwn(t, sa)
		storetest.PublishOwn(t, sb)
		l, err := p.refs.List(store.Namespace)
		refs := l.Heads
		if err != nil || len(refs) != 2 || refs[0].Name != (store.WriterRef{Checkout: ida}).Name() || refs[1].Name != (store.WriterRef{Checkout: idb}).Name() {
			t.Errorf("after both publish the refs are %v, %v; want one per checkout", refs, err)
		}
		if h := storetest.Heads(t, sa); h.RefsRead != 2 || len(h.Streams) != 2 {
			t.Errorf("after both publish a checkout sees %+v; want two refs read and two streams, each from its log", h)
		}
	})
}
