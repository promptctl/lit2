// Package storetest is the behavioral suite every substrate of the store
// passes, and the contract suite of its Files port. Its assertions speak of
// logs, segments, publications, and refs (specifications/events-and-store.md,
// "Log, segments and refs"), never of file layouts or bytes beyond git's object ids.
package storetest

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Entropy is seeded entropy safe for concurrent reads, for a substrate's
// ports: logs created at once by many appenders each read an id.
func Entropy(seed byte) io.Reader { return &locked{r: rand.NewChaCha8([32]byte{seed})} }

type locked struct {
	mu sync.Mutex
	r  io.Reader
}

func (l *locked) Read(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(b)
}

// Fresh is a substrate: the ports of a fresh common dir, empty, each call.
type Fresh func(*testing.T) store.Ports

var (
	a = store.CheckoutID{0xa}
	b = store.CheckoutID{0xb}
)

func batch(t *testing.T, rs ...string) store.Batch {
	t.Helper()
	records := make([]store.Record, len(rs))
	for i, r := range rs {
		records[i] = store.Record(r)
	}
	bt, err := store.NewBatch(records...)
	if err != nil {
		t.Fatal(err)
	}
	return bt
}

func texts(bt store.Batch) []string {
	var ts []string
	for _, r := range bt.Records() {
		ts = append(ts, string(r))
	}
	return ts
}

// AppendAll appends each batch in turn.
func AppendAll(t *testing.T, s *store.Store, bs ...store.Batch) []store.Appended {
	t.Helper()
	out := make([]store.Appended, len(bs))
	for i, bt := range bs {
		var err error
		if out[i], err = s.Append(bt); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// Numbered is n batches of one record each, prefix-0000 on.
func Numbered(t *testing.T, n int, prefix string) []store.Batch {
	t.Helper()
	bs := make([]store.Batch, n)
	for i := range bs {
		bs[i] = batch(t, fmt.Sprintf("%s-%04d", prefix, i))
	}
	return bs
}

func heads(t *testing.T, s *store.Store) store.Heads {
	t.Helper()
	h, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Unreadable) != 0 {
		t.Errorf("heads hold unreadable streams: %v", h.Unreadable)
	}
	return h
}

// stream is the stream of ref in h.
func stream(t *testing.T, h store.Heads, ref store.Ref) store.Stream {
	t.Helper()
	for _, s := range h.Streams {
		if s.Ref == ref {
			return s
		}
	}
	t.Fatalf("no stream of %s in %v", ref.Name(), h.Streams)
	return store.Stream{}
}

// Whole reads ref's stream from 0 and returns its scan.
func Whole(t *testing.T, s *store.Store, ref store.Ref) store.Scan {
	t.Helper()
	scan, err := s.Read(store.Range{Stream: stream(t, heads(t, s), ref), From: 0})
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

// Read is every record of a scan's frames, in order.
func Read(scan store.Scan) []string {
	var ts []string
	for _, f := range scan.Frames {
		ts = append(ts, texts(f.Batch)...)
	}
	return ts
}

// All is every record of bs, in order.
func All(bs []store.Batch) []string {
	var ts []string
	for _, bt := range bs {
		ts = append(ts, texts(bt)...)
	}
	return ts
}

// PublishOwn seals the store's own log and publishes it.
func PublishOwn(t *testing.T, s *store.Store) store.Published {
	t.Helper()
	sealed, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := sealed.(store.Publication)
	if !ok {
		t.Fatalf("sealing the own log made %#v; want a publication", sealed)
	}
	out, err := s.Publish(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
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

// Store holds fresh to the store's behavior.
func Store(t *testing.T, fresh Fresh) {
	t.Run("a fresh common dir reads zero refs, zero logs and no stream", func(t *testing.T) {
		t.Parallel()
		s := store.Open(a, fresh(t), store.DefaultSegmentBound)
		h := heads(t, s)
		if h.RefsRead != 0 || h.LogsRead != 0 || len(h.Streams) != 0 {
			t.Errorf("a fresh store's heads are %+v; want zero refs, zero logs, no stream", h)
		}
		if _, err := s.SealOwn(); !errors.As(err, new(store.NotFound)) {
			t.Errorf("sealing with no log failed with %v; want NotFound", err)
		}
	})

	t.Run("appended batches read back in order, each whole, at increasing offsets", func(t *testing.T) {
		t.Parallel()
		s := store.Open(a, fresh(t), store.DefaultSegmentBound)
		bs := []store.Batch{batch(t, "one"), batch(t, "two", "three"), batch(t, "four", "five", "six")}
		appended := AppendAll(t, s, bs...)
		if appended[0].Log != store.Created || appended[1].Log != store.Existing || appended[2].Records != 3 {
			t.Errorf("the appends were %+v; want the first to create the log", appended)
		}
		scan := Whole(t, s, store.WriterRef{Checkout: a})
		if len(scan.Frames) != len(bs) || len(scan.Torn) != 0 || scan.End != scan.Until || scan.End != appended[2].Until.At {
			t.Fatalf("the log reads %+v; want %d whole frames ending where the last append ended", scan, len(bs))
		}
		for i, f := range scan.Frames {
			if !slices.Equal(texts(f.Batch), texts(bs[i])) || i > 0 && f.At <= scan.Frames[i-1].At {
				t.Errorf("frame %d is %v at %d; want %v after the frame before it", i, texts(f.Batch), f.At, texts(bs[i]))
			}
		}
	})

	t.Run("a partial frame is not read or consumed, and is one torn span once a frame follows it", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, store.DefaultSegmentBound)
		AppendAll(t, s, batch(t, "before"))
		partial := store.Encode(batch(t, "lost in a crash"))
		partial = partial[:len(partial)/2]
		if _, err := p.Files.Append(store.LogPath(a), partial); err != nil {
			t.Fatal(err)
		}
		scan := Whole(t, s, store.WriterRef{Checkout: a})
		if !slices.Equal(Read(scan), []string{"before"}) || len(scan.Torn) != 0 || uint64(scan.Until-scan.End) != uint64(len(partial)) {
			t.Errorf("with a partial frame the log reads %+v; want the whole frame and the rest an incomplete tail", scan)
		}
		AppendAll(t, s, batch(t, "after"))
		scan = Whole(t, s, store.WriterRef{Checkout: a})
		if !slices.Equal(Read(scan), []string{"before", "after"}) || len(scan.Torn) != 1 || scan.Torn[0].Len() != uint64(len(partial)) || scan.End != scan.Until {
			t.Errorf("after one more append the log reads %+v; want both records and one torn span of %d bytes", scan, len(partial))
		}
	})

	t.Run("many appenders to one log lose no record", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		var want []string
		var wg sync.WaitGroup
		for g := range 8 {
			bs := Numbered(t, 4, fmt.Sprintf("g%d", g))
			want = append(want, All(bs)...)
			wg.Go(func() {
				s := store.Open(a, p, store.DefaultSegmentBound)
				for _, bt := range bs {
					if _, err := s.Append(bt); err != nil {
						t.Error(err)
						return
					}
				}
			})
		}
		wg.Wait()
		scan := Whole(t, store.Open(a, p, store.DefaultSegmentBound), store.WriterRef{Checkout: a})
		got := Read(scan)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || len(scan.Torn) != 0 {
			t.Errorf("%d concurrent appends read back %d records and %d torn spans; want each record once, none torn", len(want), len(got), len(scan.Torn))
		}
	})

	t.Run("many appends publish as one commit whose stream reads back the log", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, store.DefaultSegmentBound)
		bs := Numbered(t, 50, "event")
		AppendAll(t, s, bs...)
		out := PublishOwn(t, s)
		if out.Move != (store.RefCreated{}) || out.Prior != (store.NoPrior{}) || out.Kept != 0 || out.Sealed == 0 || len(out.New) != out.Sealed+2 {
			t.Errorf("the publish was %+v; want a created ref, no prior, and only its segments, one tree and one commit new", out)
		}
		// a clone that fetched lit's repository and holds no log of a
		other := fresh(t)
		other.Objects, other.Refs = p.Objects, p.Refs
		reader := store.Open(b, other, store.DefaultSegmentBound)
		scan := Whole(t, reader, store.WriterRef{Checkout: a})
		if !slices.Equal(Read(scan), All(bs)) || len(scan.Torn) != 0 || scan.End != scan.Until {
			t.Errorf("the publication reads back %d records; want the log's %d", len(Read(scan)), len(bs))
		}
	})

	t.Run("a second publish sends only the open segment's delta, and an idle one nothing", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, 256)
		AppendAll(t, s, Numbered(t, 30, "first")...)
		first := PublishOwn(t, s)
		before := segments(t, p.Objects, first.Commit)
		AppendAll(t, s, Numbered(t, 3, "more")...)
		second := PublishOwn(t, s)
		after := segments(t, p.Objects, second.Commit)
		if len(before) < 2 || second.Kept != len(before)-1 || len(second.New) != second.Sealed+2 ||
			second.Move != (store.RefReplaced{Previous: first.Commit}) || second.Prior != (store.KeptFrom{Commit: first.Commit}) {
			t.Errorf("the second publish was %+v over %d segments; want the %d full ones kept, the rest, a tree and a commit new", second, len(before), len(before)-1)
		}
		if !slices.Equal(after[:len(before)-1], before[:len(before)-1]) {
			t.Errorf("the full segments changed: %v, then %v", before, after)
		}
		idle := PublishOwn(t, s)
		if idle.Move != (store.RefUnchanged{}) || len(idle.New) != 0 || idle.NewBytes != 0 || idle.Commit != second.Commit {
			t.Errorf("an idle publish was %+v; want the ref unchanged and nothing new", idle)
		}
	})

	t.Run("a log with no whole frame has nothing to publish, and makes no ref", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, store.DefaultSegmentBound)
		AppendAll(t, s, batch(t, "one"))
		size, err := p.Files.Size(store.LogPath(a))
		if err != nil {
			t.Fatal(err)
		}
		header, err := p.Files.ReadAt(store.LogPath(a), 0, size-uint64(len(store.Encode(batch(t, "one")))))
		if err != nil {
			t.Fatal(err)
		}
		// the header and half a frame: what a crash in the first append leaves
		half := store.Encode(batch(t, "half"))
		crashed := fresh(t)
		if _, err := crashed.Files.CreateOnce(store.LogPath(a), append(header, half[:len(half)/2]...)); err != nil {
			t.Fatal(err)
		}
		s = store.Open(a, crashed, store.DefaultSegmentBound)
		sealed, err := s.SealOwn()
		if want := (store.NothingToPublish{Ref: store.WriterRef{Checkout: a}, Log: heads(t, s).Streams[0].Until.Log}); err != nil || sealed != want {
			t.Errorf("sealing a log with no whole frame made %#v, %v; want %#v", sealed, err, want)
		}
		if refs, bad, err := crashed.Refs.List("refs/lit/"); err != nil || len(refs)+len(bad) != 0 {
			t.Errorf("with nothing to publish the refs are %v, %v, %v; want none", refs, bad, err)
		}
	})

	t.Run("a ref whose publication does not read is resealed from 0, and says why", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, 64)
		bs := Numbered(t, 10, "event")
		AppendAll(t, s, bs...)
		ref := store.WriterRef{Checkout: a}
		blob, _, err := p.Objects.PutBlob([]byte("not a commit"))
		if err != nil {
			t.Fatal(err)
		}
		lost, err := git.ParseObjectID(git.SHA1, "ce013625030ba8dba906f756967f9e9ca394464a")
		if err != nil {
			t.Fatal(err)
		}
		for _, held := range []git.ObjectID{lost, blob} {
			if err := p.Refs.Replace(ref.Name(), held); err != nil {
				t.Fatal(err)
			}
			out := PublishOwn(t, s)
			resealed, ok := out.Prior.(store.Resealed)
			if !ok || resealed.Why == nil || out.Kept != 0 || out.Move != (store.RefReplaced{Previous: held}) {
				t.Errorf("over a ref holding %s the publish was %+v; want it resealed from 0 with a reason, the ref replaced", held, out)
			}
			// a clone that fetched lit's repository and holds no log of a
			other := fresh(t)
			other.Objects, other.Refs = p.Objects, p.Refs
			if got := Read(Whole(t, store.Open(b, other, 64), ref)); !slices.Equal(got, All(bs)) {
				t.Errorf("the resealed publication reads %v; want the log's %v", got, All(bs))
			}
		}
	})

	t.Run("a log shorter than its own publication is refused, not sealed", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, store.DefaultSegmentBound)
		AppendAll(t, s, batch(t, "one"))
		size, err := p.Files.Size(store.LogPath(a))
		if err != nil {
			t.Fatal(err)
		}
		short, err := p.Files.ReadAt(store.LogPath(a), 0, size)
		if err != nil {
			t.Fatal(err)
		}
		AppendAll(t, s, batch(t, "two"))
		PublishOwn(t, s)
		// the same repository beside a log holding only the first batch
		rewound := fresh(t)
		rewound.Objects, rewound.Refs = p.Objects, p.Refs
		if _, err := rewound.Files.CreateOnce(store.LogPath(a), short); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Open(a, rewound, store.DefaultSegmentBound).SealOwn(); !errors.As(err, new(store.ShorterThanPublished)) {
			t.Errorf("sealing a log shorter than its publication failed with %v; want ShorterThanPublished", err)
		}
	})

	t.Run("an inbox publication reads like a writer's, and its rewrite reads in its place", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		s := store.Open(a, p, store.DefaultSegmentBound)
		inbox := store.InboxRef{Inbox: store.InboxID{1}}
		publish := func(log store.LogID, records ...string) store.Published {
			t.Helper()
			pub, err := store.NewPublication(inbox, log, []store.Segment{store.Fresh{Start: 0, Bytes: store.Encode(batch(t, records...))}})
			if err != nil {
				t.Fatal(err)
			}
			out, err := s.Publish(pub)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		filed := publish(store.LogID{1}, "filed", "remotely")
		if got := Read(Whole(t, s, inbox)); filed.Move != (store.RefCreated{}) || !slices.Equal(got, []string{"filed", "remotely"}) {
			t.Errorf("the inbox reads %v after %+v; want its two records", got, filed)
		}
		rewrite := publish(store.LogID{2}, "filed")
		if rewrite.Move != (store.RefReplaced{Previous: filed.Commit}) {
			t.Errorf("the rewrite's publish was %+v; want it to replace the ref", rewrite)
		}
		if got := Read(Whole(t, s, inbox)); !slices.Equal(got, []string{"filed"}) {
			t.Errorf("after a rewrite the inbox reads %v; want only what the rewrite holds", got)
		}
	})

	t.Run("two checkouts over one common dir write their own logs and refs and read each other", func(t *testing.T) {
		t.Parallel()
		p := fresh(t)
		sa, sb := store.Open(a, p, store.DefaultSegmentBound), store.Open(b, p, store.DefaultSegmentBound)
		AppendAll(t, sa, batch(t, "from a"))
		AppendAll(t, sb, batch(t, "from b"))
		for _, s := range []*store.Store{sa, sb} {
			h := heads(t, s)
			if h.LogsRead != 2 || len(h.Streams) != 2 ||
				!slices.Equal(Read(Whole(t, s, store.WriterRef{Checkout: a})), []string{"from a"}) ||
				!slices.Equal(Read(Whole(t, s, store.WriterRef{Checkout: b})), []string{"from b"}) {
				t.Errorf("a checkout sees %+v; want both logs, each with its own record", h)
			}
		}
		PublishOwn(t, sa)
		PublishOwn(t, sb)
		refs, _, err := p.Refs.List("refs/lit/")
		if err != nil || len(refs) != 2 || refs[0].Name != (store.WriterRef{Checkout: a}).Name() || refs[1].Name != (store.WriterRef{Checkout: b}).Name() {
			t.Errorf("after both publish the refs are %v, %v; want one per checkout", refs, err)
		}
		if h := heads(t, sa); h.RefsRead != 2 || len(h.Streams) != 2 {
			t.Errorf("after both publish a checkout sees %+v; want two refs read and two streams, each from its log", h)
		}
	})

}
