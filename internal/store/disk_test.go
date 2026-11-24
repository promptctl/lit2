package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

var (
	ida = store.CheckoutID{0xa}
	idb = store.CheckoutID{0xb}
)

func TestTwoWorktreesOfOneCloneShareOneStoreEachWithItsOwnLogAndRef(t *testing.T) {
	main := gittest.Clone(t)
	linked := filepath.Join(filepath.Dir(main.WorkTree), "linked")
	gittest.Git(t, main.WorkTree, "worktree", "add", "-q", linked)
	second, err := gittest.Runner(t).Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	if second.CommonDir != main.CommonDir || second.GitDir == main.GitDir {
		t.Fatalf("the linked worktree is %+v; want the main's common dir and its own git dir", second)
	}
	sa := store.Open(ida, store.DiskPorts(gittest.Runner(t), main, storetest.Entropy(1)), store.DefaultSegmentBound)
	sb := store.Open(idb, store.DiskPorts(gittest.Runner(t), second, storetest.Entropy(2)), store.DefaultSegmentBound)
	ba, bb := storetest.Numbered(t, 3, "a"), storetest.Numbered(t, 2, "b")
	storetest.AppendAll(t, sa, ba...)
	storetest.AppendAll(t, sb, bb...)
	storetest.PublishOwn(t, sa)
	storetest.PublishOwn(t, sb)
	fromA, fromB := storetest.All(ba), storetest.All(bb)
	for _, s := range []*store.Store{sa, sb} {
		if got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: ida})); !slices.Equal(got, fromA) {
			t.Errorf("a worktree reads a's stream as %v; want %v", got, fromA)
		}
		if got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: idb})); !slices.Equal(got, fromB) {
			t.Errorf("a worktree reads b's stream as %v; want %v", got, fromB)
		}
	}
	repo := filepath.Join(main.SharedDir(), "store", "repo")
	refs := strings.Fields(gittest.Git(t, repo, "for-each-ref", "--format=%(refname)", "refs/lit/"))
	want := []string{string(store.WriterRef{Checkout: ida}.Name()), string(store.WriterRef{Checkout: idb}.Name())}
	if !slices.Equal(refs, want) {
		t.Errorf("lit's repository holds the refs %v; want one per worktree, %v", refs, want)
	}
}

// codeRepository is what lit must never change in the code repository:
// its refs as git lists them, and every file under objects/ and refs/.
func codeRepository(t *testing.T, c git.Checkout) string {
	t.Helper()
	var files []string
	for _, dir := range []string{"objects", "refs"} {
		err := filepath.WalkDir(filepath.Join(c.CommonDir, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			files = append(files, fmt.Sprintf("%s %d %v", p, info.Size(), info.ModTime()))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return gittest.Git(t, c.WorkTree, "for-each-ref") + "\n" + strings.Join(files, "\n")
}

func TestAThousandAppendsAndAPublishLeaveTheCodeRepositoryAsItWasAndPassFsck(t *testing.T) {
	c := gittest.Clone(t)
	before := codeRepository(t, c)
	s := store.Open(ida, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(1)), 4096)
	storetest.AppendAll(t, s, storetest.Numbered(t, 1000, "event")...)
	first := storetest.PublishOwn(t, s)
	storetest.AppendAll(t, s, storetest.Numbered(t, 10, "more")...)
	second := storetest.PublishOwn(t, s)
	if after := codeRepository(t, c); after != before {
		t.Errorf("the code repository changed:\nbefore\n%s\nafter\n%s", before, after)
	}
	repo := filepath.Join(c.SharedDir(), "store", "repo")
	gittest.Git(t, repo, "fsck", "--strict", "--no-dangling", first.Commit.String(), second.Commit.String())
	if got := gittest.Git(t, repo, "rev-parse", string(store.WriterRef{Checkout: ida}.Name())); got != second.Commit.String() {
		t.Errorf("git reads the writer's ref as %s; want the second publish's commit %s", got, second.Commit)
	}
	if parents := gittest.Git(t, repo, "rev-list", "--count", second.Commit.String()); parents != "1" {
		t.Errorf("the writer's ref holds a history of %s commits; want 1", parents)
	}
}

// TestAppendingProcess is the child of the two-process test: with
// LIT2_STORE_ROOT set it says it is ready, waits for the parent's go, then
// appends LIT2_APPENDS records to the log there, prints when its first
// append began and its last ended, and exits; run directly, it has no
// store to append to and skips.
func TestAppendingProcess(t *testing.T) {
	root := os.Getenv("LIT2_STORE_ROOT")
	if root == "" {
		t.Skip("the child of TestTwoProcessesAppendingToOneLogLoseNoRecord, run only by it with LIT2_STORE_ROOT set")
	}
	var n int
	if _, err := fmt.Sscan(os.Getenv("LIT2_APPENDS"), &n); err != nil {
		t.Fatal(err)
	}
	prefix, barrier := os.Getenv("LIT2_PREFIX"), os.Getenv("LIT2_BARRIER")
	repo := gittest.Runner(t).RepositoryAt(filepath.Join(root, "repo"))
	ports := store.Ports{Files: store.FilesAt(root), Objects: repo, Refs: repo, Entropy: storetest.Entropy(byte(os.Getpid()))}
	s, batches := store.Open(ida, ports, store.DefaultSegmentBound), storetest.Numbered(t, n, prefix)
	if err := os.WriteFile(filepath.Join(barrier, "ready-"+prefix), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return exists(t, filepath.Join(barrier, "go")) })
	first := time.Now()
	storetest.AppendAll(t, s, batches...)
	fmt.Printf("appended %d %d\n", first.UnixNano(), time.Now().UnixNano())
}

// await polls done, briefly, until it holds or the test's deadline, a
// minute when the test has none, passes.
func await(t *testing.T, done func() bool) {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Minute)
	}
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("the barrier did not open by the test's deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

// exists says whether a file is at path.
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// Two processes appending to one log at once lose no record. Both wait on
// a barrier, each ready before the parent says go, so their appends
// start together, and each says when its appends began and ended, so the
// test fails if they never overlapped.
func TestTwoProcessesAppendingToOneLogLoseNoRecord(t *testing.T) {
	root, barrier := filepath.Join(t.TempDir(), "store"), t.TempDir()
	// the children end by the test's own deadline, so a hung child fails
	// this test rather than the whole binary's timeout
	ctx := t.Context()
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	prefixes := []string{"first", "second"}
	outs := make([]bytes.Buffer, len(prefixes))
	exited := make(chan error, len(prefixes))
	var wg sync.WaitGroup
	for i, prefix := range prefixes {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAppendingProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "LIT2_STORE_ROOT="+root, "LIT2_APPENDS=100", "LIT2_PREFIX="+prefix, "LIT2_BARRIER="+barrier)
		cmd.Stdout, cmd.Stderr = &outs[i], &outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			err := cmd.Wait()
			if err != nil {
				t.Errorf("the %s appender: %v\n%s", prefix, err, &outs[i])
			}
			exited <- err
		})
	}
	// a child that exits before it is ready never will be, so the barrier
	// stops waiting for it
	await(t, func() bool {
		if len(exited) > 0 {
			return true
		}
		return exists(t, filepath.Join(barrier, "ready-first")) && exists(t, filepath.Join(barrier, "ready-second"))
	})
	if err := os.WriteFile(filepath.Join(barrier, "go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	spans := make([][2]int64, len(prefixes))
	for i := range prefixes {
		said := 0
		for line := range strings.Lines(outs[i].String()) {
			if span, ok := strings.CutPrefix(line, "appended "); ok {
				said, _ = fmt.Sscan(span, &spans[i][0], &spans[i][1])
			}
		}
		if said != 2 {
			t.Fatalf("the %s appender said %q; want when its appends began and ended", prefixes[i], &outs[i])
		}
	}
	t.Logf("the appenders ran over %v and %v", spans[0], spans[1])
	if spans[0][0] >= spans[1][1] || spans[1][0] >= spans[0][1] {
		t.Errorf("the appenders ran over %v and %v, one after the other; want them overlapping", spans[0], spans[1])
	}
	repo := gittest.Runner(t).RepositoryAt(filepath.Join(root, "repo"))
	s := store.Open(idb, store.Ports{Files: store.FilesAt(root), Objects: repo, Refs: repo, Entropy: storetest.Entropy(3)}, store.DefaultSegmentBound)
	got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: ida}))
	if len(got) != 200 || len(slices.Compact(slices.Sorted(slices.Values(got)))) != 200 {
		t.Errorf("two processes of 100 appends each left %d records; want 200, each once", len(got))
	}
}

// A ref broken by something other than git, which writes every ref
// whole, a commit that will not read, and a segment whose header reads but
// whose body does not are each one unreadable stream, said, hiding no
// other, and the read that finds them writes nothing.
func TestABrokenRefOrObjectIsOneUnreadableStream(t *testing.T) {
	c := gittest.Clone(t)
	publish := func(id store.CheckoutID, seed byte) store.Published {
		s := store.Open(id, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(seed)), store.DefaultSegmentBound)
		// each writer's own records, so no two share a segment's blob
		storetest.AppendAll(t, s, storetest.Numbered(t, 2, id.String())...)
		return storetest.PublishOwn(t, s)
	}
	publish(ida, 4)
	pb := publish(idb, 5)
	publish(store.CheckoutID{0xc}, 6)
	pd := publish(store.CheckoutID{0xd}, 8)
	repo := filepath.Join(c.SharedDir(), "store", "repo")
	cut := func(id git.ObjectID, keep int) {
		t.Helper()
		path := filepath.Join(repo, "objects", id.String()[:2], id.String()[2:])
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
	ref := filepath.Join(repo, filepath.FromSlash(string(store.WriterRef{Checkout: ida}.Name())))
	if err := os.WriteFile(ref, []byte("torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	ports := store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(7))
	ports.Files = store.NewMemoryFiles()
	// d's one segment, its loose file cut to half, so its header reads and
	// its body does not, as a filesystem that lost a write would leave it
	commit, err := ports.Objects.Commit(pd.Commit)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := ports.Objects.Tree(commit.Tree)
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
	s := store.Open(ida, store.DiskPorts(gittest.Runner(t), c, storetest.Entropy(5)), store.DefaultSegmentBound)
	storetest.AppendAll(t, s, storetest.Numbered(t, 1, "event")...)
	out := storetest.PublishOwn(t, s)
	root := filepath.Join(c.SharedDir(), "store")
	object, err := os.Stat(filepath.Join(root, "repo", "objects", out.Commit.String()[:2], out.Commit.String()[2:]))
	if err != nil {
		t.Fatal(err)
	}
	objectPerm := object.Mode().Perm()
	if objectPerm&0o444 == 0 {
		t.Fatalf("git made the commit's object file %v, readable by no one", objectPerm)
	}
	log, err := os.Stat(filepath.Join(root, filepath.FromSlash(string(store.LogPath(ida)))))
	if err != nil {
		t.Fatal(err)
	}
	// the read bits of git's objects, and no write bit but the owner's
	if perm := log.Mode().Perm(); perm&^0o200 != objectPerm {
		t.Errorf("the log is %v; want %v, git's object file's, with at most the owner's write bit", perm, objectPerm)
	}
	// git writes the ref, so its write bits are git's; its read bits are
	// the objects'
	ref, err := os.Stat(filepath.Join(root, "repo", filepath.FromSlash(string(store.WriterRef{Checkout: ida}.Name()))))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ref.Mode().Perm()&0o444, objectPerm&0o444; got != want {
		t.Errorf("the ref is %v, readable as %v; want readable as git's object file, %v", ref.Mode().Perm(), got, want)
	}
}
