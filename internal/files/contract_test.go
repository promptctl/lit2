package files_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/files/filestest"
)

// The Files port contract's run, over the disk adapter and its twin.
func TestFilesContract(t *testing.T) { filestest.Files(t) }
