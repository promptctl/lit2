package git_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

func TestCodeContract(t *testing.T) {
	t.Parallel()
	t.Run("repository", func(t *testing.T) {
		gittest.Code(t, func(t *testing.T) gittest.Commits { return gittest.NewRepo(t) })
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Code(t, func(*testing.T) gittest.Commits { return gittest.Memory{CodeMemory: &git.CodeMemory{}} })
	})
}
