package files_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/panics"
)

var (
	dir = files.MustName("dir")
	a   = files.MustName("a")
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

func TestTheZeroDirIsRefused(t *testing.T) {
	f := files.At(files.Dir{}, files.NewDurability())
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

// A Dir's parent is it without its last element; a Dir of one element has
// none, and asking for it is a bug.
func TestADirsParentIsItWithoutItsLastElement(t *testing.T) {
	base := "/base"
	if got, want := files.DirAt(base, dir).Sub(a).Parent().Path(), filepath.Join(base, dir.String()); got != want {
		t.Errorf("the parent of dir/a is %s; want %s", got, want)
	}
	panics.With(t, files.ErrNoParent, func() { files.DirAt(base, dir).Parent() })
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
