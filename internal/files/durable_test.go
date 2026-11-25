package files_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
)

// The rows of what a write leaves on disk: which of its syncs ran, what a
// failed one makes of the result, and what becomes of a create's temps.
// Each fault is forced at a seam, and os is the oracle beside the port.

var errSync = errors.New("the disk refused the fsync")

// recorded is the files of base/dir with every fsync recorded in syncs and
// failed where fail says.
func recorded(base string, syncs *filestest.Syncs, fail func(string) error) files.Disk {
	return files.At(filestest.Seamed(files.DirAt(base, dir), filestest.Recording(syncs, fail)), files.NewDurability())
}

// failing is a fail that fails the sync of path alone.
func failing(path string) func(string) error {
	return func(p string) error {
		if p == path {
			return errSync
		}
		return nil
	}
}

// temps fails t unless the directory at path holds exactly the temps want,
// by os.
func temps(t *testing.T, path string, want ...string) {
	t.Helper()
	es, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range es {
		if len(e.Name()) > 5 && e.Name()[:5] == ".tmp-" {
			got = append(got, e.Name())
		}
	}
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s holds the temps %v; want %v", path, got, want)
	}
}

// existing is base, holding dir/a with "head" in it.
func existing(t *testing.T) string {
	base := t.TempDir()
	filestest.Create(t, files.At(files.DirAt(base, dir), files.NewDurability()), a, []byte("head"))
	return base
}

func TestAnAppendThatLandedIsNeverNotAppended(t *testing.T) {
	t.Run("a failed sync of its file is AppendedNotDurable, the bytes there", func(t *testing.T) {
		base := existing(t)
		f := files.DirAt(base, dir)
		got := recorded(base, &filestest.Syncs{}, failing(f.PathOf(a))).Append(a, []byte("abc"))
		if no, ok := got.(files.AppendedNotDurable); !ok || no.End != 7 || !errors.Is(no.Why, errSync) {
			t.Errorf("Append whose file's sync failed = %#v; want AppendedNotDurable at 7, by that sync", got)
		}
		holds(t, f.PathOf(a), "headabc")
	})
	t.Run("a durable append syncs its file alone: its name's step synced the name", func(t *testing.T) {
		base := existing(t)
		var syncs filestest.Syncs
		f := files.DirAt(base, dir)
		if got := recorded(base, &syncs, filestest.Passing).Append(a, []byte("abc")); got != (files.Appended{End: 7}) {
			t.Errorf("Append = %#v; want Appended at 7", got)
		}
		if got, want := syncs.Seen(), []string{f.PathOf(a)}; !slices.Equal(got, want) {
			t.Errorf("Append synced %v; want %v, the file", got, want)
		}
	})
}

func TestALosingCreateIsAsDurableAsTheWinner(t *testing.T) {
	t.Run("it syncs the directory after the link", func(t *testing.T) {
		base := existing(t)
		var syncs filestest.Syncs
		f := files.DirAt(base, dir)
		if got := recorded(base, &syncs, filestest.Passing).CreateOnce(a, []byte("late")); !reapedNone(got, files.Existing) {
			t.Errorf("the losing CreateOnce = %#v; want Placed, Existing", got)
		}
		if seen := syncs.Seen(); len(seen) == 0 || seen[len(seen)-1] != f.Path() {
			t.Errorf("the losing CreateOnce synced %v; want its directory last, after the link", seen)
		}
		holds(t, f.PathOf(a), "head")
	})
	t.Run("a failed directory sync is PlacedNotDurable, Existing", func(t *testing.T) {
		base := existing(t)
		got := recorded(base, &filestest.Syncs{}, failing(files.DirAt(base, dir).Path())).CreateOnce(a, []byte("late"))
		if p, ok := got.(files.PlacedNotDurable); !ok || p.Made != files.Existing || !errors.Is(p.Why, errSync) {
			t.Errorf("the losing CreateOnce whose directory sync failed = %#v; want PlacedNotDurable, Existing", got)
		}
	})
}

func TestACreatedFileIsNeverAFailure(t *testing.T) {
	t.Run("a failed directory sync is PlacedNotDurable, Created, the file there", func(t *testing.T) {
		base := t.TempDir()
		f := files.DirAt(base, dir)
		got := recorded(base, &filestest.Syncs{}, failing(f.Path())).CreateOnce(a, []byte("x"))
		if p, ok := got.(files.PlacedNotDurable); !ok || p.Made != files.Created || !errors.Is(p.Why, errSync) {
			t.Errorf("CreateOnce whose directory sync failed = %#v; want PlacedNotDurable, Created", got)
		}
		holds(t, f.PathOf(a), "x")
	})
	t.Run("a temp it could not remove is Left, the file Created", func(t *testing.T) {
		base := t.TempDir()
		errRemove := errors.New("the directory refused the removal")
		seams := filestest.OS()
		seams.Remove = func(*os.Root, string) error { return errRemove }
		f := filestest.Seamed(files.DirAt(base, dir), seams)
		got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
		p, ok := got.(files.Placed)
		if !ok || p.Made != files.Created || len(p.Reap.Left) != 1 || p.Reap.Left[0].Name != ".tmp-a-0" || !errors.Is(p.Reap.Left[0].Why, errRemove) {
			t.Errorf("CreateOnce whose temp would not go = %#v; want Placed, Created, .tmp-a-0 Left", got)
		}
		holds(t, f.PathOf(a), "x")
		temps(t, f.Path(), ".tmp-a-0")
	})
	t.Run("a directory it could not read for temps is Unlisted, the file Created", func(t *testing.T) {
		base := t.TempDir()
		errList := errors.New("the directory refused the read")
		seams := filestest.OS()
		seams.List = func(string, *os.File) ([]fs.DirEntry, error) { return nil, errList }
		f := filestest.Seamed(files.DirAt(base, dir), seams)
		mkdir(t, f.Path())
		write(t, filepath.Join(f.Path(), ".tmp-a-0"), "a crash's")
		got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
		if p, ok := got.(files.Placed); !ok || p.Made != files.Created || !errors.Is(p.Reap.Unlisted, errList) || len(p.Reap.Reaped) != 0 {
			t.Errorf("CreateOnce whose directory would not read = %#v; want Placed, Created, Unlisted by that read", got)
		}
		holds(t, f.PathOf(a), "x")
		temps(t, f.Path(), ".tmp-a-0")
	})
}

func TestACreateThatPlacesNothingSaysTheTempItLeft(t *testing.T) {
	errRemove := errors.New("the directory refused the removal")
	unremovable := func(seams filestest.Seams) filestest.Seams {
		seams.Remove = func(*os.Root, string) error { return errRemove }
		return seams
	}
	left := func(t *testing.T, got files.Placing) error {
		t.Helper()
		no, ok := got.(files.NotPlaced)
		if !ok || len(no.Left) != 1 || no.Left[0].Name != ".tmp-a-0" || !errors.Is(no.Left[0].Why, errRemove) {
			t.Errorf("CreateOnce = %#v; want NotPlaced, .tmp-a-0 Left by the removal", got)
		}
		return no.Why
	}
	t.Run("a link refused", func(t *testing.T) {
		base := t.TempDir()
		f := filestest.Seamed(files.DirAt(base, dir), unremovable(filestest.OS()))
		mkdir(t, f.Path())
		mkdir(t, f.PathOf(a))
		notAFile(t, "CreateOnce over a directory", left(t, files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))))
		temps(t, f.Path(), ".tmp-a-0")
	})
	t.Run("a temp whose sync failed", func(t *testing.T) {
		base := t.TempDir()
		d := files.DirAt(base, dir)
		mkdir(t, d.Path())
		f := filestest.Seamed(d, unremovable(filestest.Recording(&filestest.Syncs{}, failing(filepath.Join(d.Path(), ".tmp-a-0")))))
		if why := left(t, files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))); !errors.Is(why, errSync) {
			t.Errorf("CreateOnce whose temp's sync failed failed with %v; want that sync", why)
		}
		temps(t, f.Path(), ".tmp-a-0")
	})
}

func TestEveryDirectoryACreateMakesIsSyncedIntoItsParent(t *testing.T) {
	d := func(base string) files.Dir {
		return files.DirAt(base, files.MustName("lit")).Sub(files.MustName("store")).Sub(files.MustName("log"))
	}
	want := func(base, temp string) []string {
		lit := filepath.Join(base, "lit")
		store := filepath.Join(lit, "store")
		log := filepath.Join(store, "log")
		return []string{base, lit, store, filepath.Join(log, temp), log}
	}
	base := t.TempDir()
	for _, made := range []struct{ name, temp string }{{"made", "a"}, {"found, a racing writer's", "b"}} {
		t.Run("each element "+made.name, func(t *testing.T) {
			var syncs filestest.Syncs
			n := files.MustName(made.temp)
			files.At(filestest.Seamed(d(base), filestest.Recording(&syncs, filestest.Passing)), files.NewDurability()).CreateOnce(n, []byte("x"))
			if got, want := syncs.Seen(), want(base, ".tmp-"+made.temp+"-0"); !slices.Equal(got, want) {
				t.Errorf("CreateOnce synced %v; want %v: each parent after its entry, the temp, then the directory", got, want)
			}
		})
	}
	// a failed parent sync stops no create: the entry is there to every
	// later step, so the name is placed or found and said not durable
	for _, before := range []struct {
		name string
		made files.Made
	}{{"absent", files.Created}, {"there, a rerun", files.Existing}} {
		t.Run("a failed parent sync is PlacedNotDurable, the name "+before.name, func(t *testing.T) {
			base := t.TempDir()
			lit := filepath.Join(base, "lit")
			if before.made == files.Existing {
				filestest.Create(t, files.At(d(base), files.NewDurability()), a, []byte("x"))
			}
			got := files.At(filestest.Seamed(d(base), filestest.Recording(&filestest.Syncs{}, failing(lit))), files.NewDurability()).CreateOnce(a, []byte("x"))
			if p, ok := got.(files.PlacedNotDurable); !ok || p.Made != before.made || !errors.Is(p.Why, errSync) {
				t.Errorf("CreateOnce whose parent sync failed = %#v; want PlacedNotDurable, %v, by that sync", got, before.made)
			}
			holds(t, d(base).PathOf(a), "x")
		})
	}
}

// One port's sync that failed is answered to every later sync of it,
// though the disk's next would pass: a write that relies on what the
// failed sync held is never said durable.
func TestAFailedSyncIsAnsweredToEveryLaterSyncOfThePort(t *testing.T) {
	base := t.TempDir()
	d := files.DirAt(base, dir)
	mkdir(t, d.Path())
	once := errors.New("the disk refused the fsync, once")
	failed := false
	var syncs filestest.Syncs
	f := files.At(filestest.Seamed(d, filestest.Recording(&syncs, func(path string) error {
		if path == d.Path() && !failed {
			failed = true
			return once
		}
		return nil
	})), files.NewDurability())
	if p, ok := f.CreateOnce(a, []byte("x")).(files.PlacedNotDurable); !ok || !errors.Is(p.Why, once) {
		t.Fatalf("the first create = %#v; want PlacedNotDurable, by the failed sync", p)
	}
	seen := len(syncs.Seen())
	if p, ok := f.CreateOnce(files.MustName("b"), []byte("y")).(files.PlacedNotDurable); !ok || p.Made != files.Created || !errors.Is(p.Why, once) {
		t.Errorf("the second create = %#v; want PlacedNotDurable, Created, by the first failure", p)
	}
	if err := f.Sync(); !errors.Is(err, once) {
		t.Errorf("the port's Sync = %v; want the first failure", err)
	}
	for _, p := range syncs.Seen()[seen:] {
		if p == d.Path() {
			t.Errorf("the directory was synced again after its sync failed: %v", syncs.Seen()[seen:])
		}
	}
	// the file's own fsync is keyed by the file, which no failed sync of
	// it marks
	if got, ok := f.Append(a, []byte("z")).(files.Appended); !ok {
		t.Errorf("an append after it = %#v; want Appended, its file's sync its own", got)
	}
}

// failingOnce is d whose fsync of its own directory fails with why, once,
// every other fsync recorded in syncs and run.
func failingOnce(d files.Dir, syncs *filestest.Syncs, why error) files.Dir {
	failed := false
	return filestest.Seamed(d, filestest.Recording(syncs, func(path string) error {
		if path == d.Path() && !failed {
			failed = true
			return why
		}
		return nil
	}))
}

// The process's one keeper answers a directory's failed sync to every
// port over that directory, though the port that asks would sync it
// fine, and to no port over another directory.
func TestAFailedSyncOfADirectoryIsAnsweredToEveryPortOverIt(t *testing.T) {
	base := t.TempDir()
	d, other := files.DirAt(base, dir), files.DirAt(base, files.MustName("other"))
	mkdir(t, d.Path())
	mkdir(t, other.Path())
	errEIO := fmt.Errorf("fsync %s: %w", d.Path(), syscall.EIO)
	u := files.NewDurability()
	var failing, passing filestest.Syncs
	first := files.At(failingOnce(d, &failing, errEIO), u)
	second := files.At(filestest.Seamed(d, filestest.Recording(&passing, filestest.Passing)), u)
	beside := files.At(filestest.Seamed(other, filestest.Recording(&passing, filestest.Passing)), u)
	if err := first.Sync(); !errors.Is(err, syscall.EIO) {
		t.Fatalf("the first port's Sync = %v; want the seam's EIO", err)
	}
	if p, ok := second.CreateOnce(a, []byte("x")).(files.PlacedNotDurable); !ok || p.Made != files.Created || !errors.Is(p.Why, syscall.EIO) {
		t.Errorf("the second port's create = %#v; want PlacedNotDurable, Created, by the first port's failure", p)
	}
	if slices.Contains(passing.Seen(), d.Path()) {
		t.Errorf("the second port synced %s again after its sync failed: %v", d.Path(), passing.Seen())
	}
	if p, ok := beside.CreateOnce(a, []byte("x")).(files.Placed); !ok || p.Made != files.Created {
		t.Errorf("a create in another directory = %#v; want Placed, Created, no other directory's failure its own", p)
	}
	if !slices.Contains(passing.Seen(), other.Path()) {
		t.Errorf("the other directory was not synced: %v", passing.Seen())
	}
}

// A sync that passes while another sync of its directory fails answers
// that failure: the keeper looks again once its fsync returns. The second
// sync is held in its fsync by a gate while the first fails.
func TestASyncThatPassedBesideAFailedOneAnswersTheFailure(t *testing.T) {
	base := t.TempDir()
	d := files.DirAt(base, dir)
	mkdir(t, d.Path())
	u := files.NewDurability()
	reached, release := make(chan struct{}), make(chan struct{})
	held := filestest.OS()
	run := held.Sync
	held.Sync = func(path string, f *os.File) error {
		if path == d.Path() {
			close(reached)
			<-release
		}
		return run(path, f)
	}
	var syncs filestest.Syncs
	failing := files.At(failingOnce(d, &syncs, syscall.EIO), u)
	passed := make(chan error, 1)
	go func() { passed <- files.At(filestest.Seamed(d, held), u).Sync() }()
	<-reached
	if err := failing.Sync(); !errors.Is(err, syscall.EIO) {
		t.Fatalf("the failing sync = %v; want EIO", err)
	}
	close(release)
	if err := <-passed; !errors.Is(err, syscall.EIO) {
		t.Errorf("the sync held while the other failed = %v; want that failure, never durable", err)
	}
}

// A temp is a new file each time, so a later temp at a path whose earlier
// temp's fsync failed answers by its own fsync, never by that failure.
func TestATempsFailedSyncIsItsCreatesAlone(t *testing.T) {
	base := t.TempDir()
	d := files.DirAt(base, dir)
	mkdir(t, d.Path())
	temp := filepath.Join(d.Path(), ".tmp-a-0")
	failed := false
	var syncs filestest.Syncs
	u := files.NewDurability()
	f := files.At(filestest.Seamed(d, filestest.Recording(&syncs, func(path string) error {
		if path == temp && !failed {
			failed = true
			return syscall.EIO
		}
		return nil
	})), u)
	if p, ok := f.CreateOnce(a, []byte("x")).(files.NotPlaced); !ok || !errors.Is(p.Why, syscall.EIO) {
		t.Fatalf("the create whose temp's sync failed = %#v; want NotPlaced, by EIO", p)
	}
	if p, ok := f.CreateOnce(a, []byte("x")).(files.Placed); !ok || p.Made != files.Created {
		t.Errorf("the next create, its temp at that path = %#v; want Placed, Created", p)
	}
}

func TestACreateReapsItsNamesOtherTempsOnceItIsPlaced(t *testing.T) {
	for _, before := range []struct {
		name string
		made files.Made
	}{{"absent", files.Created}, {"there", files.Existing}} {
		t.Run("a stale temp is reaped and said, the name "+before.name, func(t *testing.T) {
			base := t.TempDir()
			f := files.DirAt(base, dir)
			if before.made == files.Existing {
				filestest.Create(t, files.At(f, files.NewDurability()), a, []byte("x"))
			} else {
				mkdir(t, f.Path())
			}
			write(t, filepath.Join(f.Path(), ".tmp-a-0"), "a crash's")
			got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
			if p, ok := got.(files.Placed); !ok || p.Made != before.made || !slices.Equal(p.Reap.Reaped, []string{".tmp-a-0"}) || len(p.Reap.Left) != 0 {
				t.Errorf("CreateOnce = %#v; want Placed, %v, .tmp-a-0 reaped", got, before.made)
			}
			temps(t, f.Path())
		})
	}
	t.Run("another name's temps are not its own", func(t *testing.T) {
		base := t.TempDir()
		f := files.DirAt(base, dir)
		mkdir(t, f.Path())
		write(t, filepath.Join(f.Path(), ".tmp-b-0"), "b's")
		write(t, filepath.Join(f.Path(), ".tmp-a-1-0"), "a-1's")
		// no create's temp: its count is no number
		write(t, filepath.Join(f.Path(), ".tmp-a-x"), "no one's")
		if got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x")); !reapedNone(got, files.Created) {
			t.Errorf("CreateOnce(a) = %#v; want Placed, Created, reaping nothing", got)
		}
		temps(t, f.Path(), ".tmp-a-1-0", ".tmp-a-x", ".tmp-b-0")
	})
	t.Run("a create that places nothing reaps nothing", func(t *testing.T) {
		base := t.TempDir()
		f := files.DirAt(base, dir)
		mkdir(t, f.Path())
		mkdir(t, f.PathOf(a))
		write(t, filepath.Join(f.Path(), ".tmp-a-0"), "a crash's")
		got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
		notAFile(t, "CreateOnce over a directory", notPlaced(t, got))
		if no, ok := got.(files.NotPlaced); ok && len(no.Left) != 0 {
			t.Errorf("CreateOnce over a directory left %v; want its own temp removed", no.Left)
		}
		temps(t, f.Path(), ".tmp-a-0")
	})
}

// reapedNone is whether p is Placed as made, having swept nothing.
func reapedNone(p files.Placing, made files.Made) bool {
	got, ok := p.(files.Placed)
	return ok && got.Made == made && len(got.Reap.Reaped) == 0 && len(got.Reap.Left) == 0 && got.Reap.Unlisted == nil
}

// A loser whose temp the winner reaps between its temp and its link finds
// the name there when its link fails, and both creates complete, with no
// lock. The gate holds the loser in that gap while the winner runs.
func TestALoserWhoseTempIsReapedFindsTheNameThere(t *testing.T) {
	base := t.TempDir()
	f := files.DirAt(base, dir)
	mkdir(t, f.Path())
	held, release := make(chan struct{}), make(chan struct{})
	var syncs filestest.Syncs
	seams := filestest.Recording(&syncs, filestest.Passing)
	seams.Gate = func(path string) {
		if path == filepath.Join(f.Path(), ".tmp-a-0") {
			close(held)
			<-release
		}
	}
	loser := make(chan files.Placing, 1)
	go func() {
		loser <- files.At(filestest.Seamed(f, seams), files.NewDurability()).CreateOnce(a, []byte("loser"))
	}()
	<-held
	won := files.At(f, files.NewDurability()).CreateOnce(a, []byte("winner"))
	if p, ok := won.(files.Placed); !ok || p.Made != files.Created || !slices.Equal(p.Reap.Reaped, []string{".tmp-a-0"}) {
		t.Errorf("the winner = %#v; want Placed, Created, the loser's .tmp-a-0 reaped", won)
	}
	temps(t, f.Path())
	close(release)
	if got := <-loser; !reapedNone(got, files.Existing) {
		t.Errorf("the loser, its temp reaped = %#v; want Placed, Existing, reaping nothing", got)
	}
	if seen := syncs.Seen(); len(seen) == 0 || seen[len(seen)-1] != f.Path() {
		t.Errorf("the loser synced %v; want its directory last", seen)
	}
	holds(t, f.PathOf(a), "winner")
	temps(t, f.Path())
}

// A listing's temps are every create's temporary file, by the name's
// grammar; an entry named like one that holds no count is refused.
func TestAListingsTempsAreTheCreatesTemps(t *testing.T) {
	base := t.TempDir()
	f := files.DirAt(base, dir)
	mkdir(t, f.Path())
	for _, temp := range []string{".tmp-a-0", ".tmp-a-1-0", ".tmp-a-x", ".tmp-b-0"} {
		write(t, filepath.Join(f.Path(), temp), "left")
	}
	l, err := files.At(f, files.NewDurability()).List()
	if err != nil || !slices.Equal(l.Temps, []string{".tmp-a-0", ".tmp-a-1-0", ".tmp-b-0"}) || len(l.Refused) != 1 || l.Refused[0].Name != ".tmp-a-x" {
		t.Errorf("List = %+v, %v; want the three temps, and .tmp-a-x refused", l, err)
	}
}

// Only a regular file is a temp: a directory or a symlink named like one
// is refused by a listing, and no create's reap removes it.
func TestAnEntryNamedAsATempThatIsNoFileIsRefusedAndKept(t *testing.T) {
	base := t.TempDir()
	f := files.DirAt(base, dir)
	mkdir(t, f.Path())
	mkdir(t, filepath.Join(f.Path(), ".tmp-a-0"))
	symlink(t, t.TempDir(), filepath.Join(f.Path(), ".tmp-a-1"))
	l, err := files.At(f, files.NewDurability()).List()
	if err != nil || len(l.Temps) != 0 || len(l.Refused) != 2 {
		t.Errorf("List = %+v, %v; want no temp, and both entries refused", l, err)
	}
	for _, r := range l.Refused {
		if !errors.As(r.Why, new(files.NotAFile)) {
			t.Errorf("%s is refused by %v; want NotAFile", r.Name, r.Why)
		}
	}
	if got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x")); !reapedNone(got, files.Created) {
		t.Errorf("CreateOnce(a) = %#v; want Placed, Created, reaping nothing", got)
	}
	for _, e := range []string{".tmp-a-0", ".tmp-a-1"} {
		if _, err := os.Lstat(filepath.Join(f.Path(), e)); err != nil {
			t.Errorf("%s is gone, %v; want it kept", e, err)
		}
	}
}

func TestACreateOverAMissingBaseIsRefused(t *testing.T) {
	base := filepath.Join(t.TempDir(), "missing")
	if err := notPlaced(t, files.At(files.DirAt(base, dir), files.NewDurability()).CreateOnce(a, []byte("x"))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("CreateOnce over a missing base failed with %v; want fs.ErrNotExist", err)
	}
	if _, err := os.Lstat(base); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the base is there, %v; want nothing made", err)
	}
}

// A Disk's Sync is its directory's.
func TestADiskSyncsItsDirectory(t *testing.T) {
	base := t.TempDir()
	var syncs filestest.Syncs
	d := filestest.Seamed(files.DirAt(base, dir), filestest.Recording(&syncs, filestest.Passing))
	mkdir(t, d.Path())
	if err := files.At(d, files.NewDurability()).Sync(); err != nil {
		t.Fatal(err)
	}
	if got := syncs.Seen(); !slices.Equal(got, []string{d.Path()}) {
		t.Errorf("Sync synced %v; want %s", got, d.Path())
	}
	failed := filestest.Seamed(files.DirAt(base, dir), filestest.Recording(&filestest.Syncs{}, failing(d.Path())))
	if err := files.At(failed, files.NewDurability()).Sync(); !errors.Is(err, errSync) {
		t.Errorf("Sync whose fsync failed = %v; want that fsync", err)
	}
}

// A Dir's sync checks it by the step and fsyncs its directory.
func TestADirIsSyncedByTheStep(t *testing.T) {
	base := t.TempDir()
	var syncs filestest.Syncs
	d := filestest.Seamed(files.DirAt(base, dir), filestest.Recording(&syncs, filestest.Passing))
	mkdir(t, d.Path())
	if err := files.NewDurability().Sync(d); err != nil {
		t.Fatal(err)
	}
	if got := syncs.Seen(); !slices.Equal(got, []string{d.Path()}) {
		t.Errorf("Sync synced %v; want %s", got, d.Path())
	}
	linked, outside := t.TempDir(), t.TempDir()
	var none filestest.Syncs
	symlink(t, outside, filepath.Join(linked, dir.String()))
	notTheDirectory(t, "Sync", files.NewDurability().Sync(filestest.Seamed(files.DirAt(linked, dir), filestest.Recording(&none, filestest.Passing))))
	if got := none.Seen(); len(got) != 0 {
		t.Errorf("Sync through a link synced %v; want nothing", got)
	}
}

// Every failed sync of a create is in its answer: a parent's and the
// directory's are of two paths, and a create that places nothing still
// says the parent's.
func TestACreateSaysEveryFailedSync(t *testing.T) {
	d := func(base string) files.Dir {
		return files.DirAt(base, files.MustName("lit")).Sub(files.MustName("log"))
	}
	errDir := errors.New("the disk refused a second fsync")
	fail := func(base string, also map[string]error) func(string) error {
		return func(p string) error {
			if p == filepath.Join(base, "lit") {
				return errSync
			}
			return also[p]
		}
	}
	t.Run("placed", func(t *testing.T) {
		base := t.TempDir()
		f := filestest.Seamed(d(base), filestest.Recording(&filestest.Syncs{}, fail(base, map[string]error{d(base).Path(): errDir})))
		got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
		if p, ok := got.(files.PlacedNotDurable); !ok || !errors.Is(p.Why, errSync) || !errors.Is(p.Why, errDir) {
			t.Errorf("CreateOnce whose parent and directory syncs failed = %#v; want PlacedNotDurable, by both", got)
		}
	})
	t.Run("not placed", func(t *testing.T) {
		base := t.TempDir()
		f := filestest.Seamed(d(base), filestest.Recording(&filestest.Syncs{}, fail(base, map[string]error{filepath.Join(d(base).Path(), ".tmp-a-0"): errDir})))
		got := files.At(f, files.NewDurability()).CreateOnce(a, []byte("x"))
		if p, ok := got.(files.NotPlaced); !ok || !errors.Is(p.Why, errSync) || !errors.Is(p.Why, errDir) {
			t.Errorf("CreateOnce whose parent and temp syncs failed = %#v; want NotPlaced, by both", got)
		}
	})
}
