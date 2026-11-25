package store_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// cloneOf is the store over p as the suite reaches it, everything in it
// read from p, so it can never disagree with the ports.
func cloneOf(p store.Ports) storetest.Clone {
	objects, refs := store.RepositoryOf(p)
	return storetest.Clone{Ports: p, Logs: store.FilesOf(p), Objects: objects, Refs: refs}
}

func memory(*testing.T) storetest.Clone {
	repo := git.NewMemory()
	return cloneOf(store.MemoryPorts(filestest.Memory(), repo, repo, storetest.Entropy(1)))
}

// gitdir is a fresh clone's common dir on disk, made by git init, and its
// store's ports as the composition root builds them.
func gitdir(t *testing.T) storetest.Clone {
	return cloneOf(store.DiskPorts(gittest.Runner(t), gittest.Clone(t), storetest.Entropy(2), files.NewDurability()))
}

// fetchedBy is the store of a fresh clone of fresh's that fetched c's lit
// repository: c's ports over the fresh clone's logs.
func fetchedBy(fresh func(*testing.T) storetest.Clone) func(*testing.T, storetest.Clone) storetest.Clone {
	return func(t *testing.T, c storetest.Clone) storetest.Clone {
		return cloneOf(store.WithFiles(c.Ports, fresh(t).Logs))
	}
}

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		storetest.Store(t, storetest.Substrate{Fresh: memory, Fetched: fetchedBy(memory)})
	})
	t.Run("gitdir", func(t *testing.T) {
		storetest.Store(t, storetest.Substrate{Fresh: gitdir, Fetched: fetchedBy(gitdir)})
	})
}
