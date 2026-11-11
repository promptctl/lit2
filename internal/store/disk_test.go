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
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

var (
	ida = store.CheckoutID{0xa}
	idb = store.CheckoutID{0xb}
)

func appendN(t *testing.T, s *store.Store, n int, prefix string) []string {
	t.Helper()
	var want []string
	for i := range n {
		r := fmt.Sprintf("%s-%04d", prefix, i)
		b, err := store.NewBatch(store.Record(r))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append(b); err != nil {
			t.Fatal(err)
		}
		want = append(want, r)
	}
	return want
}

func publish(t *testing.T, s *store.Store) store.Published {
	t.Helper()
	p, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Publish(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func records(t *testing.T, s *store.Store, ref store.Ref) []string {
	t.Helper()
	h, err := s.Heads()
	if err != nil || len(h.Unreadable) != 0 {
		t.Fatalf("Heads = %+v, %v", h, err)
	}
	for _, st := range h.Streams {
		if st.Ref != ref {
			continue
		}
		scan, err := s.Read(store.Range{Stream: st, From: 0})
		if err != nil {
			t.Fatal(err)
		}
		var rs []string
		for _, f := range scan.Frames {
			for _, r := range f.Batch.Records() {
				rs = append(rs, string(r))
			}
		}
		return rs
	}
	t.Fatalf("no stream of %s", ref.Name())
	return nil
}

func TestTwoWorktreesOfOneCloneShareOneStoreEachWithItsOwnLogAndRef(t *testing.T) {
	main := clone(t)
	linked := filepath.Join(filepath.Dir(main.WorkTree), "linked")
	run(t, main.WorkTree, "worktree", "add", "-q", linked)
	second, err := git.Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	if second.CommonDir != main.CommonDir || second.GitDir == main.GitDir {
		t.Fatalf("the linked worktree is %+v; want the main's common dir and its own git dir", second)
	}
	sa := store.Open(ida, store.DiskPorts(main, storetest.Entropy(1)), store.DefaultSegmentBound)
	sb := store.Open(idb, store.DiskPorts(second, storetest.Entropy(2)), store.DefaultSegmentBound)
	fromA, fromB := appendN(t, sa, 3, "a"), appendN(t, sb, 2, "b")
	publish(t, sa)
	publish(t, sb)
	for _, s := range []*store.Store{sa, sb} {
		if got := records(t, s, store.WriterRef{Checkout: ida}); !slices.Equal(got, fromA) {
			t.Errorf("a worktree reads a's stream as %v; want %v", got, fromA)
		}
		if got := records(t, s, store.WriterRef{Checkout: idb}); !slices.Equal(got, fromB) {
			t.Errorf("a worktree reads b's stream as %v; want %v", got, fromB)
		}
	}
	repo := filepath.Join(main.SharedDir(), "store", "repo")
	refs := strings.Fields(run(t, repo, "for-each-ref", "--format=%(refname)", "refs/lit/"))
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
	return run(t, c.WorkTree, "for-each-ref") + "\n" + strings.Join(files, "\n")
}

func TestAThousandAppendsAndAPublishLeaveTheCodeRepositoryAsItWasAndPassFsck(t *testing.T) {
	c := clone(t)
	before := codeRepository(t, c)
	s := store.Open(ida, store.DiskPorts(c, storetest.Entropy(1)), 4096)
	appendN(t, s, 1000, "event")
	first := publish(t, s)
	appendN(t, s, 10, "more")
	second := publish(t, s)
	if after := codeRepository(t, c); after != before {
		t.Errorf("the code repository changed:\nbefore\n%s\nafter\n%s", before, after)
	}
	repo := filepath.Join(c.SharedDir(), "store", "repo")
	run(t, repo, "fsck", "--strict", "--no-dangling", first.Commit.String(), second.Commit.String())
	if got := run(t, repo, "rev-parse", string(store.WriterRef{Checkout: ida}.Name())); got != second.Commit.String() {
		t.Errorf("git reads the writer's ref as %s; want the second publish's commit %s", got, second.Commit)
	}
	if parents := run(t, repo, "rev-list", "--count", second.Commit.String()); parents != "1" {
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
	appendN(t, store.Open(ida, ports, store.DefaultSegmentBound), n, os.Getenv("LIT2_PREFIX"))
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
	got := records(t, s, store.WriterRef{Checkout: ida})
	if len(got) != 200 || len(slices.Compact(slices.Sorted(slices.Values(got)))) != 200 {
		t.Errorf("two processes of 100 appends each left %d records; want 200, each once", len(got))
	}
}
