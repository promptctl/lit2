package root

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Main is production: it reads the world into a Process, once, and runs the
// invocation. Every read of the process's world is here
// `[LAW:effects-at-boundaries]`.
func Main(args []string) int {
	return Run(Process{
		Clock:    systemClock{},
		Entropy:  rand.Reader,
		Agent:    events.AgentFrom(os.Getenv("LIT_AGENT")),
		Place:    here(),
		Version:  version(),
		Commands: commandLog(),
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
	}, args)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// here is the place of the working directory.
func here() command.Place {
	dir, err := os.Getwd()
	if err != nil {
		return command.Unlocatable{Err: fmt.Errorf("reading the working directory: %w", err)}
	}
	return place(dir)
}

// place locates the checkout at dir and reads its writer.
func place(dir string) command.Place {
	c, err := git.Locate(dir)
	var none git.NotFound
	if errors.As(err, &none) {
		return command.NoCheckout{Dir: dir}
	}
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: err}
	}
	return command.InCheckout{Checkout: c, Writer: writer(filepath.Join(c.PrivateDir(), "id"))}
}

// writer reads the checkout id file at path (D11): 32 lowercase hex digits
// and a newline. No file is Unminted; a file that is not an id is
// Unreadable, a different fact.
func writer(path string) command.Writer {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return command.Unminted{Path: path}
	}
	if err != nil {
		return command.Unreadable{Path: path, Err: err}
	}
	id, err := store.ParseCheckoutID(strings.TrimSuffix(string(b), "\n"))
	if err != nil {
		return command.Unreadable{Path: path, Err: err}
	}
	return command.Minted{ID: id}
}

// commandLog is this machine's command log, $XDG_STATE_HOME/lit/commands,
// ~/.local/state when XDG_STATE_HOME is unset (D12). The XDG spec makes a
// relative value invalid, to be ignored, so one counts as unset.
func commandLog() command.Sink {
	state := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil {
			return command.Unwritable{Err: fmt.Errorf("no directory for the command log: XDG_STATE_HOME is unset or relative and %w", err)}
		}
		state = filepath.Join(home, ".local", "state")
	}
	return command.NewLog(filepath.Join(state, "lit", "commands"))
}

// version is the main module's version as the build stamped it, with the
// commit it was built from and whether the tree was modified, since a build
// from a checkout with no tag is versioned (devel).
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(no build info)"
	}
	v := info.Main.Version
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" || s.Key == "vcs.modified" {
			v += " " + s.Key + "=" + s.Value
		}
	}
	return v
}
