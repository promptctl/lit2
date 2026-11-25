//go:build integration

package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// Two worktrees of one clone publish into the clone's one lit repository,
// each under its own ref, and read each other's logs: lit's repository
// and the logs are the common dir's, never a worktree's own git dir's.
func TestTwoWorktreesOfOneClonePublishIntoItsOneRepository(t *testing.T) {
	main := gittest.Clone(t)
	linked := main.WorkTree + "-linked"
	gittest.Git(t, main.WorkTree, "worktree", "add", "-q", linked)
	second, err := gittest.Runner(t).Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	if second.CommonDir != main.CommonDir || second.GitDir == main.GitDir {
		t.Fatalf("the linked worktree is %+v; want the main's common dir and its own git dir", second)
	}
	sa := store.Open(ida, store.DiskPorts(gittest.Runner(t), main, storetest.Entropy(1), files.NewDurability()), store.DefaultSegmentBound)
	sb := store.Open(idb, store.DiskPorts(gittest.Runner(t), second, storetest.Entropy(2), files.NewDurability()), store.DefaultSegmentBound)
	for _, s := range []*store.Store{sa, sb} {
		storetest.AppendAll(t, s, storetest.Numbered(t, 1, "event")...)
		storetest.PublishOwn(t, s)
	}
	want := []string{string(store.WriterRef{Checkout: ida}.Name()), string(store.WriterRef{Checkout: idb}.Name())}
	listed := strings.Fields(gittest.Git(t, store.RepositoryDir(main), "for-each-ref", "--format=%(refname)", string(store.Namespace)))
	if !slices.Equal(listed, want) {
		t.Errorf("lit's repository holds the refs %v; want one per worktree, %v", listed, want)
	}
	for _, s := range []*store.Store{sa, sb} {
		h := storetest.Heads(t, s)
		if h.LogsRead != 2 || h.RefsRead != 2 || len(h.Streams) != 2 {
			t.Errorf("a worktree's heads are %+v; want both logs and both refs read", h)
		}
	}
}

// Appends and publishes, a segment kept and another sealed, leave the code
// repository as it was, and git holds what they wrote to fsck --strict.
func TestPublishesLeaveTheCodeRepositoryAsItWasAndPassFsck(t *testing.T) {
	c := gittest.Clone(t)
	before := gittest.Stored(t, c.CommonDir)
	s := store.Open(ida, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(1), files.NewDurability()), 256)
	storetest.AppendAll(t, s, storetest.Numbered(t, 30, "event")...)
	first := storetest.PublishOwn(t, s)
	storetest.AppendAll(t, s, storetest.Numbered(t, 3, "more")...)
	second := storetest.PublishOwn(t, s)
	if second.Kept == 0 || second.Sealed == 0 {
		t.Fatalf("the second publish was %+v; want a segment kept and one sealed", second)
	}
	if after := gittest.Stored(t, c.CommonDir); after != before {
		t.Errorf("the code repository changed:\nbefore\n%s\nafter\n%s", before, after)
	}
	repo := store.RepositoryDir(c)
	gittest.Git(t, repo, "fsck", "--strict", "--no-dangling", first.Commit.String(), second.Commit.String())
	if got := gittest.Git(t, repo, "rev-parse", string(store.WriterRef{Checkout: ida}.Name())); got != second.Commit.String() {
		t.Errorf("git reads the writer's ref as %s; want the second publish's commit %s", got, second.Commit)
	}
	if parents := gittest.Git(t, repo, "rev-list", "--count", second.Commit.String()); parents != "1" {
		t.Errorf("the writer's ref holds a history of %s commits; want 1", parents)
	}
}

// The turns the two appending processes pass between them: each reads
// its turn from the first and hands the next on through the second.
const (
	turnIn  = 3
	turnOut = 4
)

// TestAppendingProcess is the child of the two-process test: with
// LIT2_WORKTREE set it appends LIT2_APPENDS records to the log of the
// store of the checkout there, each on its own turn, and exits; run
// directly, it has no store to append to and skips.
func TestAppendingProcess(t *testing.T) {
	worktree := os.Getenv("LIT2_WORKTREE")
	if worktree == "" {
		t.Skip("the child of TestTwoProcessesAppendingToOneLogLoseNoRecord, run only by it with LIT2_WORKTREE set")
	}
	var n int
	if _, err := fmt.Sscan(os.Getenv("LIT2_APPENDS"), &n); err != nil {
		t.Fatal(err)
	}
	g := gittest.Runner(t)
	c, err := g.Locate(worktree)
	if err != nil {
		t.Fatal(err)
	}
	disk := store.DiskPorts(g, c, storetest.Entropy(byte(os.Getpid())), files.NewDurability())
	turns := &turnTaking{Files: store.FilesOf(disk), t: t, in: os.NewFile(turnIn, "turn in"), out: os.NewFile(turnOut, "turn out")}
	storetest.AppendAll(t, store.Open(ida, store.WithFiles(disk, turns), store.DefaultSegmentBound), storetest.Numbered(t, n, os.Getenv("LIT2_PREFIX"))...)
}

// turnTaking is Files whose every append waits for its turn, a byte read
// from in, and hands the next turn on, a byte written to out, once the
// append has landed. The other process reads out and writes in, so its
// appends fall between this one's. Append is the one call of a store's
// append that writes the log; creating the log is CreateOnce, whose race
// is its own and needs no turn.
type turnTaking struct {
	files.Files
	t       *testing.T
	in, out *os.File
}

func (f *turnTaking) Append(n files.Name, b []byte) files.Appending {
	if _, err := io.ReadFull(f.in, make([]byte, 1)); err != nil {
		f.t.Fatalf("waiting for a turn: %v", err)
	}
	got := f.Files.Append(n, b)
	if _, landed := got.(files.Appended); !landed {
		return got
	}
	if _, err := f.out.Write([]byte{1}); err != nil {
		f.t.Fatalf("handing on a turn: %v", err)
	}
	return got
}

// Two processes appending to one log lose no record. Their appends are
// made to alternate, a turn passed between them through two pipes, so
// every append of one lands between two of the other's
// [LAW:verifiable-goals]: the interleaving is forced, never left to the
// scheduler.
func TestTwoProcessesAppendingToOneLogLoseNoRecord(t *testing.T) {
	c := gittest.Clone(t)
	// the children end by the test's own deadline, so a hung child fails
	// this test rather than the whole binary's timeout; and a child that
	// fails strands the other on a turn it will never get, so either's
	// failure ends both
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	// toSecond carries the first's turns on to the second, toFirst the
	// second's back; this process holds both ends of each until the
	// children end, so the last turn handed on has a reader
	toSecond, toFirst := pipe(t), pipe(t)
	const appends = 100
	prefixes := []string{"first", "second"}
	ends := [][2]*os.File{{toFirst.r, toSecond.w}, {toSecond.r, toFirst.w}}
	outs := make([]bytes.Buffer, len(prefixes))
	var wg sync.WaitGroup
	for i, prefix := range prefixes {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAppendingProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "LIT2_WORKTREE="+c.WorkTree, fmt.Sprintf("LIT2_APPENDS=%d", appends), "LIT2_PREFIX="+prefix)
		cmd.ExtraFiles = ends[i][:] // turnIn and turnOut, after stdin, stdout and stderr
		cmd.Stdout, cmd.Stderr = &outs[i], &outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			if err := cmd.Wait(); err != nil {
				cancel()
				t.Errorf("the %s appender: %v\n%s", prefix, err, &outs[i])
			}
		})
	}
	// the first turn is the first's
	if _, err := toFirst.w.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	s := store.Open(idb, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(3), files.NewDurability()), store.DefaultSegmentBound)
	got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: ida}))
	var alternating []string
	for i := range appends {
		for _, prefix := range prefixes {
			alternating = append(alternating, fmt.Sprintf("%s-%04d", prefix, i))
		}
	}
	if !slices.Equal(got, alternating) {
		t.Errorf("the log holds %v; want the two processes' appends alternating, each once, %v", got, alternating)
	}
}

// pipeEnds is a pipe's two ends.
type pipeEnds struct{ r, w *os.File }

// pipe is a pipe whose ends close when t ends.
func pipe(t *testing.T) pipeEnds {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(r.Close(), w.Close()); err != nil {
			t.Error(err)
		}
	})
	return pipeEnds{r: r, w: w}
}

// A ref broken by something other than git, which writes every ref
// whole, a commit that will not read, and a segment whose header reads but
// whose body does not are each one unreadable stream, said, hiding no
// other, and the read that finds them writes nothing.
func TestABrokenRefOrObjectIsOneUnreadableStream(t *testing.T) {
	c := gittest.Clone(t)
	publish := func(id store.CheckoutID, seed byte) store.Published {
		s := store.Open(id, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(seed), files.NewDurability()), store.DefaultSegmentBound)
		// each writer's own records, so no two share a segment's blob
		storetest.AppendAll(t, s, storetest.Numbered(t, 2, id.String())...)
		return storetest.PublishOwn(t, s)
	}
	publish(ida, 4)
	pb := publish(idb, 5)
	publish(store.CheckoutID{0xc}, 6)
	pd := publish(store.CheckoutID{0xd}, 8)
	repo := store.RepositoryDir(c)
	cut := func(id git.ObjectID, keep int) {
		t.Helper()
		path := gittest.LooseObject(repo, id)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b[:min(keep, len(b)/2)], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ref := gittest.LooseRef(repo, store.WriterRef{Checkout: ida}.Name())
	if err := os.WriteFile(ref, []byte("torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a reader holding no log
	ports := store.WithFiles(store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(7), files.NewDurability()), filestest.Memory())
	objects, _ := store.RepositoryOf(ports)
	// d's one segment, its loose file cut to half, so its header reads and
	// its body does not, as a filesystem that lost a write would leave it
	commit, err := objects.Commit(pd.Commit)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := objects.Tree(commit.Tree)
	if err != nil {
		t.Fatal(err)
	}
	cut(segs[0].Blob, 1<<20)
	// b's commit, cut to its first byte
	cut(pb.Commit, 1)
	h, read, err := store.Open(store.CheckoutID{0xff}, ports, store.DefaultSegmentBound).ReadAll()
	names := []string{}
	for _, u := range h.Unreadable {
		names = append(names, u.Name)
	}
	want := []string{string(store.WriterRef{Checkout: ida}.Name()), string(store.WriterRef{Checkout: idb}.Name()), string(store.WriterRef{Checkout: store.CheckoutID{0xd}}.Name())}
	if err != nil || !slices.Equal(names, want) || len(h.Streams) != 1 || h.Streams[0].Ref != (store.WriterRef{Checkout: store.CheckoutID{0xc}}) || len(read) != 1 || len(storetest.Read(read[0].Scan)) != 2 {
		t.Errorf("with a's ref torn, b's commit and d's segment cut short a reader's heads are %+v and reads %d, %v; want %v unreadable and c's stream read whole", h, len(read), err, want)
	}
	if got, err := os.ReadFile(ref); err != nil || string(got) != "torn" {
		t.Errorf("a read changed the broken ref: %q, %v", got, err)
	}
}

// Whoever can read a clone's objects reads its logs and refs too: who may
// read each is who may read an object file git made in the store's
// repository. A log, made through DiskPorts, is written by its owner alone.
func TestTheStoresFilesAreAsReadableAsItsObjects(t *testing.T) {
	c := gittest.Clone(t)
	s := store.Open(ida, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(5), files.NewDurability()), store.DefaultSegmentBound)
	storetest.AppendAll(t, s, storetest.Numbered(t, 1, "event")...)
	out := storetest.PublishOwn(t, s)
	repo := store.RepositoryDir(c)
	object, err := os.Stat(gittest.LooseObject(repo, out.Commit))
	if err != nil {
		t.Fatal(err)
	}
	objectPerm := object.Mode().Perm()
	if objectPerm&0o444 == 0 {
		t.Fatalf("git made the commit's object file %v, readable by no one", objectPerm)
	}
	log, err := os.Stat(store.LogsDir(c).PathOf(store.LogName(ida)))
	if err != nil {
		t.Fatal(err)
	}
	// the read bits of git's objects, and no write bit but the owner's
	if perm := log.Mode().Perm(); perm&^0o200 != objectPerm {
		t.Errorf("the log is %v; want %v, git's object file's, with at most the owner's write bit", perm, objectPerm)
	}
	// git writes the ref, so its write bits are git's; its read bits are
	// the objects'
	ref, err := os.Stat(gittest.LooseRef(repo, store.WriterRef{Checkout: ida}.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ref.Mode().Perm()&0o444, objectPerm&0o444; got != want {
		t.Errorf("the ref is %v, readable as %v; want readable as git's object file, %v", ref.Mode().Perm(), got, want)
	}
}

// An entry of the logs' directory that is no log's file, a link, a
// directory or a name no file has, is one unreadable log each, said by its
// name and why, hiding no other.
func TestAnEntryOfTheLogsThatIsNoFileIsAnUnreadableLog(t *testing.T) {
	c := gittest.Clone(t)
	s := store.Open(ida, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(9), files.NewDurability()), store.DefaultSegmentBound)
	storetest.AppendAll(t, s, storetest.Numbered(t, 1, "a")...)
	logs := store.LogsDir(c).Path()
	if err := os.Symlink(store.LogName(ida).String(), filepath.Join(logs, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(logs, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, `back\slash`), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]func(error) bool{
		`back\slash`: func(err error) bool { return errors.Is(err, files.ErrNotAName) },
		"dir": func(err error) bool {
			var n files.NotAFile
			return errors.As(err, &n) && n.Mode.IsDir()
		},
		"link": func(err error) bool {
			var n files.NotAFile
			return errors.As(err, &n) && n.Mode.Type() == fs.ModeSymlink
		},
	}
	if len(h.Unreadable) != len(want) {
		t.Errorf("the heads' unreadable are %+v; want the link, the directory and the backslash", h.Unreadable)
	}
	for _, u := range h.Unreadable {
		if is, ok := want[u.Name]; !ok || !is(u.Why) {
			t.Errorf("%s is unreadable for %v; want each of the link, the directory and the backslash unreadable for what it is", u.Name, u.Why)
		}
	}
	if len(h.Streams) != 1 || h.Streams[0].Ref != (store.WriterRef{Checkout: ida}) || h.LogsRead != 4 {
		t.Errorf("the heads are %+v; want a's stream, read from its log, beside them, and 4 logs read", h)
	}
}

// A store directory that is a link is not the clone's: lit's repository's
// first write is refused, as the logs are, and nothing is written where
// the link points.
func TestAStoreDirThatIsALinkIsRefusedForTheRepository(t *testing.T) {
	c := gittest.Clone(t)
	at, elsewhere := store.StoreDir(c).Path(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, at); err != nil {
		t.Fatal(err)
	}
	objects, _ := store.RepositoryOf(store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(10), files.NewDurability()))
	if _, err := objects.PutBlobs([][]byte{[]byte("x")}); !errors.As(err, new(files.NotTheDirectory)) {
		t.Errorf("the repository's first write through a linked store directory failed with %v; want NotTheDirectory", err)
	}
	if es, err := os.ReadDir(elsewhere); err != nil || len(es) != 0 {
		t.Errorf("the link's target holds %v, %v; want nothing", es, err)
	}
}
