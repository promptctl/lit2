package git_test

import (
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

// repositoryAt is lit's repository at path, it and the directory it is in
// checked from the directory above.
func repositoryAt(g *git.Git, path string) git.Repository {
	in := filepath.Dir(path)
	return g.RepositoryAt(files.DirAt(filepath.Dir(in), files.MustName(filepath.Base(in))).Sub(files.MustName(filepath.Base(path))), files.NewDurability())
}

func TestObjectsContract(t *testing.T) {
	t.Parallel()
	t.Run("repository", func(t *testing.T) {
		gittest.Objects(t, func(t *testing.T) git.Objects {
			return repositoryAt(gittest.Runner(t), filepath.Join(t.TempDir(), "repo"))
		})
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Objects(t, func(*testing.T) git.Objects { return git.NewMemory() })
	})
}

func TestRefsContract(t *testing.T) {
	t.Parallel()
	t.Run("repository", func(t *testing.T) {
		gittest.Refs(t, func(t *testing.T) gittest.Repository {
			return repositoryAt(gittest.Runner(t), filepath.Join(t.TempDir(), "repo"))
		})
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Refs(t, func(*testing.T) gittest.Repository { return git.NewMemory() })
	})
}
