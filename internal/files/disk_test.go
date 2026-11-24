package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/files"
)

// The rows only the disk faces: a symlink where the directory or a file
// of it should be. Each asserts with os, an oracle beside the port, that
// nothing was read or written at the link's target.

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

// overLinkedName is the files of base/dir, where dir/a links to a file
// outside base holding "outside", whose path it returns.
func overLinkedName(t *testing.T) (files.Disk, string) {
	base, outside := t.TempDir(), filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, dir.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(base, dir.String(), a.String()))
	return files.At(files.DirAt(base, dir)), outside
}

func TestANameThatLinksOutOfItsDirectoryIsRefused(t *testing.T) {
	t.Run("create-once over the link is NotAFile, not Existing", func(t *testing.T) {
		f, outside := overLinkedName(t)
		made, err := f.CreateOnce(a, []byte("inside"))
		if !errors.As(err, new(files.NotAFile)) {
			t.Errorf("CreateOnce over a symlink = %v, %v; want NotAFile", made, err)
		}
		holds(t, outside, "outside")
	})
	t.Run("an append through the link is refused", func(t *testing.T) {
		f, outside := overLinkedName(t)
		if end, err := f.Append(a, []byte("inside")); err == nil || errors.As(err, new(files.NotFound)) {
			t.Errorf("Append through a symlink out = %d, %v; want it refused", end, err)
		}
		holds(t, outside, "outside")
	})
	t.Run("a read through the link is refused", func(t *testing.T) {
		f, _ := overLinkedName(t)
		if b, err := f.ReadAt(a, 0, 100); err == nil || errors.As(err, new(files.NotFound)) {
			t.Errorf("ReadAt through a symlink out = %q, %v; want it refused, reading nothing", b, err)
		}
	})
	t.Run("create-once over a link to nothing is NotAFile, and makes nothing at its target", func(t *testing.T) {
		base, outside := t.TempDir(), filepath.Join(t.TempDir(), "nothing")
		if err := os.Mkdir(filepath.Join(base, dir.String()), 0o755); err != nil {
			t.Fatal(err)
		}
		symlink(t, outside, filepath.Join(base, dir.String(), a.String()))
		if made, err := files.At(files.DirAt(base, dir)).CreateOnce(a, []byte("inside")); !errors.As(err, new(files.NotAFile)) {
			t.Errorf("CreateOnce over a dangling symlink = %v, %v; want NotAFile", made, err)
		}
		empty(t, filepath.Dir(outside))
	})
}

func TestADirectoryThatIsALinkIsRefused(t *testing.T) {
	t.Run("a link to a directory inside its base is NotTheDirectory", func(t *testing.T) {
		base := t.TempDir()
		real := filepath.Join(base, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		// relative, as a link committed to a work tree is; os.Root follows
		// one that stays inside its root
		symlink(t, "real", filepath.Join(base, dir.String()))
		f := files.At(files.DirAt(base, dir))
		if made, err := f.CreateOnce(a, []byte("x")); !errors.As(err, new(files.NotTheDirectory)) {
			t.Errorf("CreateOnce in a linked directory = %v, %v; want NotTheDirectory", made, err)
		}
		if ns, err := f.List(); !errors.As(err, new(files.NotTheDirectory)) {
			t.Errorf("List of a linked directory = %v, %v; want NotTheDirectory", ns, err)
		}
		empty(t, real)
	})
	t.Run("a link to a directory outside its base is refused", func(t *testing.T) {
		base, outside := t.TempDir(), t.TempDir()
		symlink(t, outside, filepath.Join(base, dir.String()))
		f := files.At(files.DirAt(base, dir))
		if made, err := f.CreateOnce(a, []byte("x")); err == nil {
			t.Errorf("CreateOnce in a directory linked out = %v; want it refused", made)
		}
		if ns, err := f.List(); err == nil {
			t.Errorf("List of a directory linked out = %v; want it refused", ns)
		}
		empty(t, outside)
	})
}
