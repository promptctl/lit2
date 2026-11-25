//go:build integration && unix

package files_test

import (
	"errors"
	"io/fs"
	"os"
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
	f := files.At(files.DirAt(base, dir), files.NewDurability())
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

// A file swapped for a FIFO after its look is refused, and its open
// returns at once: O_NONBLOCK keeps it from waiting on a writer that never
// comes.
func TestAFileSwappedForAFIFOIsRefusedAtOnce(t *testing.T) {
	base := t.TempDir()
	at := filepath.Join(base, dir.String(), a.String())
	mkdir(t, filepath.Dir(at))
	write(t, at, "a")
	f := files.At(files.Gated(files.DirAt(base, dir), func(p string) {
		if p != at {
			return
		}
		if err := os.Remove(at); err != nil {
			t.Error(err)
		}
		if err := syscall.Mkfifo(at, 0o644); err != nil {
			t.Error(err)
		}
	}), files.NewDurability())
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadAt(a, 0, 100)
		done <- err
	}()
	select {
	case err := <-done:
		var refused files.NotAFile
		if !errors.As(err, &refused) || refused.Mode.Type() != fs.ModeNamedPipe {
			t.Errorf("ReadAt of a file swapped for a FIFO failed with %v; want NotAFile, a FIFO", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a read of a file swapped for a FIFO had not returned after 10s; want it refused at once")
	}
}
