package files_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/files"
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
// want, and returns no bytes.
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
	_, err = f.Size(a)
	want(t, "Size", err)
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
		_, err := f.List()
		notTheDirectory(t, "List", err)
		empty(t, filepath.Join(base, "real"))
	})
	t.Run("a link to a directory outside its base", func(t *testing.T) {
		outside := t.TempDir()
		_, f := linkedDir(t, func(t *testing.T, _, at string) { symlink(t, outside, at) })
		refusesEveryCall(t, f, notTheDirectory)
		_, err := f.List()
		notTheDirectory(t, "List", err)
		empty(t, outside)
	})
	t.Run("a dangling link", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "nothing")
		_, f := linkedDir(t, func(t *testing.T, _, at string) { symlink(t, outside, at) })
		refusesEveryCall(t, f, notTheDirectory)
		_, err := f.List()
		notTheDirectory(t, "List", err)
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

func TestADirNoConstructorMadeIsRefused(t *testing.T) {
	for name, d := range map[string]files.Dir{"the zero Dir": {}, "an empty base": files.DirAt("", dir)} {
		f := files.At(d)
		if _, err := f.CreateOnce(a, []byte("x")); !errors.Is(err, files.ErrNoDir) {
			t.Errorf("%s: CreateOnce failed with %v; want ErrNoDir", name, err)
		}
		if ns, err := f.List(); !errors.Is(err, files.ErrNoDir) {
			t.Errorf("%s: List = %v, %v; want ErrNoDir", name, ns, err)
		}
	}
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
