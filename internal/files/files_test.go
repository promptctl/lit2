package files_test

import (
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/panics"
)

func TestANameIsOneElementOfItsDirectory(t *testing.T) {
	if n, err := files.NameOf("founding"); err != nil || n.String() != "founding" {
		t.Fatalf("NameOf(founding) = %v, %v; want the name", n, err)
	}
	// each differs from the control in the one respect refused
	for _, s := range []string{"", ".", "..", "a/b", "../founding", `a\b`, "a\x00b", ".tmp-a"} {
		if n, err := files.NameOf(s); !errors.Is(err, files.ErrNotAName) {
			t.Errorf("NameOf(%q) = %v, %v; want ErrNotAName", s, n, err)
		}
	}
}

func TestFilesContract(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		filestest.Files(t, func(t *testing.T) files.Files {
			return files.At(files.DirAt(t.TempDir(), files.MustName("dir")), files.NewDurability())
		})
	})
	t.Run("memory", func(t *testing.T) {
		filestest.Files(t, func(*testing.T) files.Files { return filestest.Memory() })
	})
}

func TestAFixedNameThatIsNoNamePanicsWithErrNotAName(t *testing.T) {
	panics.With(t, files.ErrNotAName, func() { files.MustName("a/b") })
}
