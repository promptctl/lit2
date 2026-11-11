package store_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
	second, err := git.Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	if second.CommonDir != main.CommonDir || second.GitDir == main.GitDir {
		t.Fatalf("the linked worktree is %+v; want the main's common dir and its own git dir", second)
	}
	sa := store.Open(ida, store.DiskPorts(main, storetest.Entropy(1)), store.DefaultSegmentBound)
	sb := store.Open(idb, store.DiskPorts(second, storetest.Entropy(2)), store.DefaultSegmentBound)
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
	s := store.Open(ida, store.DiskPorts(c, storetest.Entropy(1)), 4096)
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
// LIT2_STORE_ROOT set it appends LIT2_APPENDS records to the log there and
// exits; run directly, it has no store to append to and does nothing.
func TestAppendingProcess(t *testing.T) {
	root := os.Getenv("LIT2_STORE_ROOT")
	if root == "" {
		return
	}
	var n int
	if _, err := fmt.Sscan(os.Getenv("LIT2_APPENDS"), &n); err != nil {
		t.Fatal(err)
	}
	repo := git.RepositoryAt(filepath.Join(root, "repo"))
	ports := store.Ports{Files: store.FilesAt(root), Objects: repo, Refs: repo, Entropy: storetest.Entropy(byte(os.Getpid()))}
	storetest.AppendAll(t, store.Open(ida, ports, store.DefaultSegmentBound), storetest.Numbered(t, n, os.Getenv("LIT2_PREFIX"))...)
}

func TestTwoProcessesAppendingToOneLogLoseNoRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var wg sync.WaitGroup
	for _, prefix := range []string{"first", "second"} {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestAppendingProcess$", "-test.count=1")
			cmd.Env = append(os.Environ(), "LIT2_STORE_ROOT="+root, "LIT2_APPENDS=100", "LIT2_PREFIX="+prefix)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("the %s appender: %v\n%s", prefix, err, out)
			}
		})
	}
	wg.Wait()
	repo := git.RepositoryAt(filepath.Join(root, "repo"))
	s := store.Open(idb, store.Ports{Files: store.FilesAt(root), Objects: repo, Refs: repo, Entropy: storetest.Entropy(3)}, store.DefaultSegmentBound)
	got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: ida}))
	if len(got) != 200 || len(slices.Compact(slices.Sorted(slices.Values(got)))) != 200 {
		t.Errorf("two processes of 100 appends each left %d records; want 200, each once", len(got))
	}
}

// Only a file can hold a torn ref, so the case is the disk's: a ref file
// that holds no object id, beside its writer's log, is the writer's stream
// and not unreadable, and is resealed from 0 and created again, at the
// commit the log's bytes make; an idle publish then leaves the file as it is.
func TestATornWriterRefIsResealedAndCreatedAgain(t *testing.T) {
	c := gittest.Clone(t)
	s := store.Open(ida, store.DiskPorts(c, storetest.Entropy(4)), store.DefaultSegmentBound)
	storetest.AppendAll(t, s, storetest.Numbered(t, 3, "event")...)
	first := storetest.PublishOwn(t, s)
	ref := filepath.Join(c.SharedDir(), "store", "repo", filepath.FromSlash(string(store.WriterRef{Checkout: ida}.Name())))
	if err := os.WriteFile(ref, []byte("torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	// its log is here, so the writer is one stream, read from the log
	if h, err := s.Heads(); err != nil || len(h.Streams) != 1 || len(h.Unreadable) != 0 {
		t.Errorf("with its own ref torn a writer's heads are %+v, %v; want its one stream and nothing unreadable", h, err)
	}
	out := storetest.PublishOwn(t, s)
	if _, ok := out.Prior.(store.Resealed); !ok || out.Move != (store.RefCreated{}) || out.Commit != first.Commit {
		t.Errorf("over a torn ref the publish was %+v; want it resealed, the ref created again at the same commit", out)
	}
	before, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	idle := storetest.PublishOwn(t, s)
	after, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	if idle.Move != (store.RefUnchanged{}) || !os.SameFile(before, after) {
		t.Errorf("an idle publish was %+v and replaced the ref file: %v; want the ref unchanged and its file kept", idle, !os.SameFile(before, after))
	}
}

// Whoever can read a clone's objects reads its logs, refs and snapshot
// too: the store's files are 0644, as go-git makes the objects.
func TestTheStoresFilesAreAsReadableAsItsObjects(t *testing.T) {
	c := gittest.Clone(t)
	s := store.Open(ida, store.DiskPorts(c, storetest.Entropy(5)), store.DefaultSegmentBound)
	storetest.AppendAll(t, s, storetest.Numbered(t, 1, "event")...)
	storetest.PublishOwn(t, s)
	if _, err := s.Swap(store.FoldTag("v1"), []store.Mark{}, []byte("state")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(c.SharedDir(), "store")
	for _, p := range []string{string(store.LogPath(ida)), string(store.SnapshotPath), "repo/" + string(store.WriterRef{Checkout: ida}.Name())} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s is %v; want -rw-r--r--", p, info.Mode().Perm())
		}
	}
}
