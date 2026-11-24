package files_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
)

func TestANameIsOneElementOfItsDirectory(t *testing.T) {
	if n, err := files.NameOf("founding"); err != nil || n.String() != "founding" {
		t.Fatalf("NameOf(founding) = %v, %v; want the name", n, err)
	}
	// each differs from the control in the one respect refused
	for _, s := range []string{"", ".", "..", "a/b", "../founding", `a\b`, "a\x00b"} {
		if n, err := files.NameOf(s); !errors.Is(err, files.ErrNotAName) {
			t.Errorf("NameOf(%q) = %v, %v; want ErrNotAName", s, n, err)
		}
	}
}

func TestFilesContract(t *testing.T) {
	dir := files.MustName("dir")
	t.Run("disk", func(t *testing.T) {
		filestest.Files(t, 0o644, func(t *testing.T) filestest.ModedFiles {
			return files.At(files.DirAt(filepath.Join(t.TempDir(), "base"), dir))
		})
	})
	t.Run("disk, a secret's mode", func(t *testing.T) {
		filestest.Files(t, 0o600, func(t *testing.T) filestest.ModedFiles {
			return files.AtMode(files.DirAt(filepath.Join(t.TempDir(), "base"), dir), 0o600)
		})
	})
	t.Run("memory", func(t *testing.T) {
		filestest.Files(t, 0o644, func(*testing.T) filestest.ModedFiles { return files.NewMemory() })
	})
	t.Run("memory, a secret's mode", func(t *testing.T) {
		filestest.Files(t, 0o600, func(*testing.T) filestest.ModedFiles { return files.NewMemoryMode(0o600) })
	})
}
