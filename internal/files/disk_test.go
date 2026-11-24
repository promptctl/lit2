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

// refusesEveryCall fails t unless every call on f refuses the name a, by
// want, and returns no bytes; a listing refuses the directory or lists a
// among the entries it refuses.
func refusesEveryCall(t *testing.T, f files.Files, want func(*testing.T, string, error)) {
	t.Helper()
	_, err := f.CreateOnce(a, []byte("inside"))
	want(t, "CreateOnce", err)
	_, err = f.Append(a, []byte("inside"))
	want(t, "Append", err)
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
		_, err := files.At(files.DirAt(base, dir)).CreateOnce(a, []byte("inside"))
		notAFile(t, "CreateOnce over a dangling symlink", err)
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
	if _, err := f.CreateOnce(a, []byte("x")); !errors.Is(err, files.ErrNoDir) {
		t.Errorf("CreateOnce failed with %v; want ErrNoDir", err)
	}
	if l, err := f.List(); !errors.Is(err, files.ErrNoDir) {
		t.Errorf("List = %v, %v; want ErrNoDir", l, err)
	}
	if err := (files.Dir{}).Make(); !errors.Is(err, files.ErrNoDir) {
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
// temporary file is neither.
func TestAListRefusesEveryEntryThatIsNoFile(t *testing.T) {
	base := t.TempDir()
	at := filepath.Join(base, dir.String())
	mkdir(t, at)
	write(t, filepath.Join(at, a.String()), "a")
	write(t, filepath.Join(at, ".tmp-a-0"), "temp")
	symlink(t, a.String(), filepath.Join(at, "link"))
	mkdir(t, filepath.Join(at, "sub"))
	write(t, filepath.Join(at, `back\slash`), "x")
	l, err := files.At(files.DirAt(base, dir)).List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Names, []files.Name{a}) {
		t.Errorf("List names %v; want a alone", l.Names)
	}
	want := map[string]func(error) bool{
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
		t.Errorf("List refused %v; want %d entries, the link, the directory and the backslash", l.Refused, len(want))
	}
	for _, r := range l.Refused {
		if is, ok := want[r.Name]; !ok || !is(r) {
			t.Errorf("List refused %s by %v; want each of %v refused by its path and why", r.Name, r.Why, slices.Collect(maps.Keys(want)))
		}
	}
}

// The step's checks of what it opened refuse an entry swapped after its
// look: each row swaps it in the gap, by the seam files' tests alone set.
func TestAnEntrySwappedAfterItsLookIsRefused(t *testing.T) {
	// swap runs do once, when the step reaches path
	swap := func(t *testing.T, path string, do func()) {
		files.BetweenLookAndOpen(t, func(p string) {
			if p == path {
				do()
			}
		})
	}
	t.Run("a directory swapped for a link to another", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		swap(t, at, func() {
			if err := os.Rename(at, filepath.Join(base, "old")); err != nil {
				t.Fatal(err)
			}
			mkdir(t, filepath.Join(base, "real"))
			symlink(t, "real", at)
		})
		_, err := files.At(files.DirAt(base, dir)).CreateOnce(a, []byte("x"))
		notTheDirectory(t, "CreateOnce through a directory swapped for a link", err)
		empty(t, filepath.Join(base, "real"))
	})
	t.Run("a file swapped for a link to another", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		write(t, filepath.Join(at, a.String()), "a")
		write(t, filepath.Join(at, "b"), "b")
		swap(t, filepath.Join(at, a.String()), func() {
			if err := os.Remove(filepath.Join(at, a.String())); err != nil {
				t.Fatal(err)
			}
			symlink(t, "b", filepath.Join(at, a.String()))
		})
		_, err := files.At(files.DirAt(base, dir)).Append(a, []byte("x"))
		notAFile(t, "Append to a file swapped for a link", err)
		holds(t, filepath.Join(at, "b"), "b")
	})
	t.Run("a file swapped for a directory", func(t *testing.T) {
		base := t.TempDir()
		at := filepath.Join(base, dir.String())
		mkdir(t, at)
		write(t, filepath.Join(at, a.String()), "a")
		swap(t, filepath.Join(at, a.String()), func() {
			if err := os.Remove(filepath.Join(at, a.String())); err != nil {
				t.Fatal(err)
			}
			mkdir(t, filepath.Join(at, a.String()))
		})
		b, err := files.At(files.DirAt(base, dir)).ReadAll(a)
		var refused files.NotAFile
		if !errors.As(err, &refused) || !refused.Mode.IsDir() {
			t.Errorf("ReadAll of a file swapped for a directory = %q, %v; want NotAFile, a directory", b, err)
		}
	})
}

// A file is made in the port's mode, whatever the umask, and its
// directories are made inside their parents.
func TestAFileIsMadeInItsPortsMode(t *testing.T) {
	for _, mode := range []fs.FileMode{0o644, 0o600} {
		base := filepath.Join(t.TempDir(), "base")
		d := files.DirAt(base, files.MustName("lit")).Sub(dir)
		if _, err := files.AtMode(d, mode).CreateOnce(a, []byte("x")); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(d.PathOf(a))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("a file of a %v port is %v; want %v", mode, info.Mode().Perm(), mode)
		}
	}
}
