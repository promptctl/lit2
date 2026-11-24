package store_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memory(*testing.T) storetest.Clone {
	repo, logs := git.NewMemory(), files.NewMemory()
	return storetest.Clone{Ports: store.MemoryPorts(logs, repo, repo, storetest.Entropy(1)), Logs: logs, Objects: repo, Refs: repo}
}

// gitdir is a fresh clone's common dir on disk, made by git init, and its
// store's ports as the composition root builds them.
func gitdir(t *testing.T) storetest.Clone {
	p := store.DiskPorts(gittest.Runner(t), gittest.Clone(t), storetest.Entropy(2))
	objects, refs := store.RepositoryOf(p)
	return storetest.Clone{Ports: p, Logs: store.FilesOf(p), Objects: objects, Refs: refs}
}

// fetchedBy is the store of a fresh clone of fresh's that fetched c's lit
// repository: c's ports over the fresh clone's logs.
func fetchedBy(fresh func(*testing.T) storetest.Clone) func(*testing.T, storetest.Clone) storetest.Clone {
	return func(t *testing.T, c storetest.Clone) storetest.Clone {
		logs := fresh(t).Logs
		return storetest.Clone{Ports: store.WithFiles(c.Ports, logs), Logs: logs, Objects: c.Objects, Refs: c.Refs}
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
