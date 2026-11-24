package store_test

import (
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memory(*testing.T) store.Ports {
	repo := git.NewMemory()
	return store.Ports{Files: store.NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: storetest.Entropy(1)}
}

// gitdir is a fresh clone's common dir on disk, made by git init, and its
// store's ports as the composition root builds them.
func gitdir(t *testing.T) store.Ports {
	return store.DiskPorts(gittest.Runner(t), gittest.Clone(t), storetest.Entropy(2))
}

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) { storetest.Store(t, memory) })
	t.Run("gitdir", func(t *testing.T) { storetest.Store(t, gitdir) })
}

func TestFilesContract(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		storetest.Files(t, 0o644, func(t *testing.T) storetest.ModedFiles { return store.FilesAt(filepath.Join(t.TempDir(), "store")) })
	})
	t.Run("disk, a secret's mode", func(t *testing.T) {
		storetest.Files(t, 0o600, func(t *testing.T) storetest.ModedFiles {
			return store.FilesAtMode(filepath.Join(t.TempDir(), "lit"), 0o600)
		})
	})
	t.Run("memory", func(t *testing.T) {
		storetest.Files(t, 0o644, func(*testing.T) storetest.ModedFiles { return store.NewMemoryFiles() })
	})
	t.Run("memory, a secret's mode", func(t *testing.T) {
		storetest.Files(t, 0o600, func(*testing.T) storetest.ModedFiles { return store.NewMemoryFilesMode(0o600) })
	})
}
