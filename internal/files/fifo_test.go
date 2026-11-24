//go:build unix

package files_test

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/files"
)

// A FIFO at a name is NotAFile, and a read or a size of it returns at
// once: nothing waits on a writer that never comes.
func TestAFIFOAtANameIsRefusedAtOnce(t *testing.T) {
	base := t.TempDir()
	mkdir(t, filepath.Join(base, dir.String()))
	if err := syscall.Mkfifo(filepath.Join(base, dir.String(), a.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	f := files.At(files.DirAt(base, dir))
	done := make(chan [2]error, 1)
	go func() {
		_, readErr := f.ReadAt(a, 0, 100)
		_, sizeErr := f.Size(a)
		done <- [2]error{readErr, sizeErr}
	}()
	select {
	case errs := <-done:
		notAFile(t, "ReadAt of a FIFO", errs[0])
		notAFile(t, "Size of a FIFO", errs[1])
	case <-time.After(10 * time.Second):
		t.Fatal("a read of a FIFO at a name had not returned after 10s; want it refused at once")
	}
}
