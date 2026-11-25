package git_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/git/gittest"
)

// The port contracts' runs: lit's repository's two ports and the code
// repository's, each over git on disk and its twin.

func TestObjectsContract(t *testing.T) { gittest.Objects(t) }

func TestRefsContract(t *testing.T) { gittest.Refs(t) }

func TestCodeContract(t *testing.T) { gittest.Code(t) }
