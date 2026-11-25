package files_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/panics"
)

// The rows only the disk faces: a symlink, or anything else, where a
// directory or a file of it should be. Each asserts with os, an oracle
// beside the port, that nothing was read or written at the link's target.

var (
	dir = files.MustName("dir")
	a   = files.MustName("a")
)

// holds fails t unless the file at path holds want.
func holds(t *testing.T, path, want string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Errorf("%s holds %q, %v; want %q, as it was", path, b, err, want)
	}
}

// empty fails t unless the directory at path holds nothing.
func empty(t *testing.T, path string) {
	t.Helper()
	if es, err := os.ReadDir(path); err != nil || len(es) != 0 {
		t.Errorf("%s holds %v, %v; want nothing", path, es, err)
	}
}

func symlink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// notAFile fails t unless err is NotAFile.
func notAFile(t *testing.T, call string, err error) {
	t.Helper()
	if !errors.As(err, new(files.NotAFile)) {
		t.Errorf("%s failed with %v; want NotAFile", call, err)
	}
}

// notTheDirectory fails t unless err is NotTheDirectory.
func notTheDirectory(t *testing.T, call string, err error) {
	t.Helper()
	if !errors.As(err, new(files.NotTheDirectory)) {
		t.Errorf("%s failed with %v; want NotTheDirectory", call, err)
	}
}

// notPlaced is why p placed nothing, failing t unless it is NotPlaced.
func notPlaced(t *testing.T, p files.Placing) error {
	t.Helper()
	no, ok := p.(files.NotPlaced)
	if !ok {
		t.Errorf("CreateOnce = %#v; want NotPlaced", p)
	}
	return no.Why
}

// notAppended is why a appended nothing, failing t unless it is
// NotAppended.
func notAppended(t *testing.T, a files.Appending) error {
	t.Helper()
	no, ok := a.(files.NotAppended)
	if !ok {
		t.Errorf("Append = %#v; want NotAppended", a)
	}
	return no.Why
}

// refusesEveryCall fails t unless every call on f refuses the name a, by
// want, and returns no bytes; a listing refuses the directory or lists a
// among the entries it refuses.
func refusesEveryCall(t *testing.T, f files.Files, want func(*testing.T, string, error)) {
	t.Helper()
	want(t, "CreateOnce", notPlaced(t, f.CreateOnce(a, []byte("inside"))))
	want(t, "Append", notAppended(t, f.Append(a, []byte("inside"))))
	b, err := f.ReadAt(a, 0, 100)
	want(t, "ReadAt", err)
	if len(b) != 0 {
		t.Errorf("ReadAt read %q; want nothing", b)
	}
	b, err = f.ReadAll(a)
	want(t, "ReadAll", err)
	if len(b) != 0 {
		t.Errorf("ReadAll read %q; want nothing", b)
	}
	_, err = f.Size(a)
	want(t, "Size", err)
	l, err := f.List()
	refusals := []error{err}
	for _, r := range l.Refused {
		refusals = append(refusals, r)
	}
	want(t, "List", errors.Join(refusals...))
	if slices.Contains(l.Names, a) {
		t.Errorf("List = %v; want a refused, not a file", l)
	}
}

// overLinkedName is the files of base/dir, where dir/a is a symlink to
// target, which holds "target".
func overLinkedName(t *testing.T, target func(dirPath string) (link, path string)) (files.Disk, string) {
	base := t.TempDir()
	mkdir(t, filepath.Join(base, dir.String()))
	link, path := target(filepath.Join(base, dir.String()))
	write(t, path, "target")
	symlink(t, link, filepath.Join(base, dir.String(), a.String()))
	return files.At(files.DirAt(base, dir)), path
}

func TestANameThatIsNotARegularFileIsRefused(t *testing.T) {
	t.Run("a link out of the directory is NotAFile to every call", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "target")
		f, target := overLinkedName(t, func(string) (string, string) { return outside, outside })
		refusesEveryCall(t, f, notAFile)
		holds(t, target, "target")
	})
	t.Run("a link to a file in the same directory is NotAFile to every call", func(t *testing.T) {
		f, target := overLinkedName(t, func(d string) (string, string) { return "b", filepath.Join(d, "b") })
		refusesEveryCall(t, f, notAFile)
		holds(t, target, "target")
	})
	t.Run("create-once over a link to nothing is NotAFile, and makes nothing at its target", func(t *testing.T) {
		base, outside := t.TempDir(), filepath.Join(t.TempDir(), "nothing")
		mkdir(t, filepath.Join(base, dir.String()))
		symlink(t, outside, filepath.Join(base, dir.String(), a.String()))
		notAFile(t, "CreateOnce over a dangling symlink", notPlaced(t, files.At(files.DirAt(base, dir)).CreateOnce(a, []byte("inside"))))
		empty(t, filepath.Dir(outside))
	})
	t.Run("a directory at a name is NotAFile to every call", func(t *testing.T) {
		base := t.TempDir()
		mkdir(t, filepath.Join(base, dir.String()))
		mkdir(t, filepath.Join(base, dir.String(), a.String()))
		refusesEveryCall(t, files.At(files.DirAt(base, dir)), notAFile)
	})
}

// linkedDir is base and the files of base/dir, where dir is what leave
// leaves at its path.
func linkedDir(t *testing.T, leave func(t *testing.T, base, at string)) (string, files.Disk) {
	base := t.TempDir()
	leave(t, base, filepath.Join(base, dir.String()))
	return base, files.At(files.DirAt(base, dir))
}

func TestADirectoryThatIsNotItselfIsRefused(t *testing.T) {
	t.Run("a link to a directory inside its base", func(t *testing.T) {
		base, f := linkedDir(t, func(t *testing.T, base, at string) {
			mkdir(t, filepath.Join(base, "real"))
			// relative, as a link committed to a work tree is; os.Root
			// follows one that stays inside its root
			symlink(t, "real", at)
		})
		refusesEveryCall(t, f, notTheDirectory)
		empty(t, filepath.Join(base, "real"))
	})
	t.Run("a link to a directory outside its base", func(t *testing.T) {
		outside := t.TempDir()
		_, f := linkedDir(t, func(t *testing.T, _, at string) { symlink(t, outside, at) })
		refusesEveryCall(t, f, notTheDirectory)
		empty(t, outside)
	})
	t.Run("a dangling link", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "nothing")
		_, f := linkedDir(t, func(t *testing.T, _, at string) { symlink(t, outside, at) })
		refusesEveryCall(t, f, notTheDirectory)
		empty(t, filepath.Dir(outside))
	})
	t.Run("a regular file", func(t *testing.T) {
		_, f := linkedDir(t, func(t *testing.T, _, at string) { write(t, at, "file") })
		refusesEveryCall(t, f, notTheDirectory)
	})
	t.Run("a middle element linked inside its base", func(t *testing.T) {
		base := t.TempDir()
		middle := files.MustName("middle")
		mkdir(t, filepath.Join(base, "real"))
		mkdir(t, filepath.Join(base, "real", dir.String()))
		symlink(t, "real", filepath.Join(base, middle.String()))
		f := files.At(files.DirAt(base, middle).Sub(dir))
		refusesEveryCall(t, f, notTheDirectory)
		empty(t, filepath.Join(base, "real", dir.String()))
	})
	t.Run("a middle element linked outside its base", func(t *testing.T) {
		base, outside := t.TempDir(), t.TempDir()
		middle := files.MustName("middle")
		mkdir(t, filepath.Join(outside, dir.String()))
		symlink(t, outside, filepath.Join(base, middle.String()))
		f := files.At(files.DirAt(base, middle).Sub(dir))
		refusesEveryCall(t, f, notTheDirectory)
		empty(t, filepath.Join(outside, dir.String()))
	})
}

func TestTheZeroDirIsRefused(t *testing.T) {
	f := files.At(files.Dir{})
	if err := notPlaced(t, f.CreateOnce(a, []byte("x"))); !errors.Is(err, files.ErrNoDir) {
		t.Errorf("CreateOnce failed with %v; want ErrNoDir", err)
	}
	if l, err := f.List(); !errors.Is(err, files.ErrNoDir) {
		t.Errorf("List = %v, %v; want ErrNoDir", l, err)
	}
	if err := files.NewDurability().Make(files.Dir{}); !errors.Is(err, files.ErrNoDir) {
		t.Errorf("Make failed with %v; want ErrNoDir", err)
	}
}

// A base that is not absolute is a bug: no Dir is made from it.
func TestADirFromNoAbsoluteBaseIsABug(t *testing.T) {
	for _, base := range []string{"", "relative"} {
		panics.With(t, files.ErrNotABase, func() { files.DirAt(base, dir) })
	}
}

// Every entry of a listed directory that is no file of the port is
// refused, by its path and why, beside the files; a create-once's
// temporary file is listed as one, so a leftover is said, never dropped,
// and an entry with the temps' prefix that is no name's temp is refused.
func TestAListRefusesEveryEntryThatIsNoFile(t *testing.T) {
	base := t.TempDir()
	at := filepath.Join(base, dir.String())
	mkdir(t, at)
	write(t, filepath.Join(at, a.String()), "a")
	write(t, filepath.Join(at, ".tmp-a-0"), "temp")
	symlink(t, a.String(), filepath.Join(at, "link"))
	mkdir(t, filepath.Join(at, "sub"))
	write(t, filepath.Join(at, `back\slash`), "x")
	write(t, filepath.Join(at, ".tmp-x"), "x")
	l, err := files.At(files.DirAt(base, dir)).List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Names, []files.Name{a}) {
		t.Errorf("List names %v; want a alone", l.Names)
	}
	if !slices.Equal(l.Temps, []string{".tmp-a-0"}) {
		t.Errorf("List temps %v; want .tmp-a-0, a create's leftover", l.Temps)
	}
	want := map[string]func(error) bool{
		".tmp-x": func(err error) bool {
			return errors.Is(err, files.ErrNotAName) && strings.Contains(err.Error(), filepath.Join(at, ".tmp-x"))
		},
		`back\slash`: func(err error) bool {
			return errors.Is(err, files.ErrNotAName) && strings.Contains(err.Error(), filepath.Join(at, `back\slash`))
		},
		"link": func(err error) bool {
			var n files.NotAFile
			return errors.As(err, &n) && n.Path == filepath.Join(at, "link") && n.Mode.Type() == fs.ModeSymlink
		},
		"sub": func(err error) bool {
			var n files.NotAFile
			return errors.As(err, &n) && n.Path == filepath.Join(at, "sub") && n.Mode.IsDir()
		},
	}
	if len(l.Refused) != len(want) {
		t.Errorf("List refused %v; want %d entries, the link, the directory, the backslash and .tmp-x", l.Refused, len(want))
	}
	for _, r := range l.Refused {
		if is, ok := want[r.Name]; !ok || !is(r) {
			t.Errorf("List refused %s by %v; want each of %v refused by its path and why", r.Name, r.Why, slices.Collect(maps.Keys(want)))
		}
	}
}

// swapped is base/dir, gated so that do runs when the step, between its
// look and its open, reaches path: the gate files' tests alone set.
func swapped(base, path string, do func()) files.Dir {
	return files.Gated(files.DirAt(base, dir), func(p string) {
		if p == path {
			do()
		}
	})
}

// changed fails t unless err is Changed, at path.
func changed(t *testing.T, call, path string, err error) {
	t.Helper()
	var c files.Changed
	if !errors.As(err, &c) || c.Path != path {
		t.Errorf("%s failed with %v; want Changed at %s", call, err, path)
	}
}

// The step's checks of what it opened refuse an entry swapped after its
// look: an entry of the kind wanted that is another one is Changed, and
// one of another kind is the kind's own refusal.
func TestAnEntrySwappedAfterItsLookIsRefused(t *testing.T) {
	t.Run("a directory swapped for a link to another", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		d := swapped(base, at, func() {
			if err := os.Rename(at, filepath.Join(base, "old")); err != nil {
				t.Fatal(err)
			}
			mkdir(t, filepath.Join(base, "real"))
			symlink(t, "real", at)
		})
		changed(t, "CreateOnce through a directory swapped for a link to another", at, notPlaced(t, files.At(d).CreateOnce(a, []byte("x"))))
		empty(t, filepath.Join(base, "real"))
	})
	t.Run("a file swapped for a link to another", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		write(t, filepath.Join(at, a.String()), "a")
		write(t, filepath.Join(at, "b"), "b")
		d := swapped(base, filepath.Join(at, a.String()), func() {
			if err := os.Remove(filepath.Join(at, a.String())); err != nil {
				t.Fatal(err)
			}
			symlink(t, "b", filepath.Join(at, a.String()))
		})
		changed(t, "Append to a file swapped for a link to another", filepath.Join(at, a.String()), notAppended(t, files.At(d).Append(a, []byte("x"))))
		holds(t, filepath.Join(at, "b"), "b")
	})
	t.Run("a file swapped for a directory", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		write(t, filepath.Join(at, a.String()), "a")
		d := swapped(base, filepath.Join(at, a.String()), func() {
			if err := os.Remove(filepath.Join(at, a.String())); err != nil {
				t.Fatal(err)
			}
			mkdir(t, filepath.Join(at, a.String()))
		})
		b, err := files.At(d).ReadAll(a)
		var refused files.NotAFile
		if !errors.As(err, &refused) || !refused.Mode.IsDir() {
			t.Errorf("ReadAll of a file swapped for a directory = %q, %v; want NotAFile, a directory", b, err)
		}
	})
}

// Check and Make take the step every call takes, holding nothing open:
// an element that is not the directory itself is refused by both, and
// one missing is made by Make alone.
func TestADirIsCheckedAndMadeByTheStep(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	symlink(t, outside, filepath.Join(base, dir.String()))
	linked := files.DirAt(base, dir).Sub(a)
	notTheDirectory(t, "Check", linked.Check())
	notTheDirectory(t, "Make", files.NewDurability().Make(linked))
	empty(t, outside)
	missing := files.DirAt(t.TempDir(), a).Sub(dir)
	if err := missing.Check(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Check of a missing Dir failed with %v; want fs.ErrNotExist", err)
	}
	if _, err := os.Lstat(missing.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Check made %s: %v; want nothing made", missing.Path(), err)
	}
	if err := files.NewDurability().Make(missing); err != nil {
		t.Fatal(err)
	}
	if err := missing.Check(); err != nil {
		t.Errorf("Check of the Dir Make made failed with %v", err)
	}
}

// A Dir's parent is it without its last element; a Dir of one element has
// none, and asking for it is a bug.
func TestADirsParentIsItWithoutItsLastElement(t *testing.T) {
	base := t.TempDir()
	if got, want := files.DirAt(base, dir).Sub(a).Parent().Path(), filepath.Join(base, dir.String()); got != want {
		t.Errorf("the parent of dir/a is %s; want %s", got, want)
	}
	panics.With(t, files.ErrNoParent, func() { files.DirAt(base, dir).Parent() })
}

// A file is made in the port's mode, whatever the umask, and its
// directories are made inside their parents.
func TestAFileIsMadeInItsPortsMode(t *testing.T) {
	for _, mode := range []fs.FileMode{0o644, 0o600} {
		d := files.DirAt(t.TempDir(), files.MustName("lit")).Sub(dir)
		filestest.Create(t, files.AtMode(d, mode), a, []byte("x"))
		info, err := os.Stat(d.PathOf(a))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("a file of a %v port is %v; want %v", mode, info.Mode().Perm(), mode)
		}
	}
}
