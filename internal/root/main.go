package root

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Main is production: it reads the world into a Process, once, and runs the
// invocation. Every read of the process's world is here
// `[LAW:effects-at-boundaries]`. It takes SIGPIPE, which would kill the
// process at its first write to a stdout pipe whose reader is gone, so that
// write returns EPIPE and the invocation keeps its record. A handler, unlike
// an ignore, is not inherited across exec, so git run by sync gets SIGPIPE
// as usual. Nothing reads the channel: delivery to a full one is dropped.
func Main(args []string) int {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	place := here()
	return Run(Process{
		Clock:    systemClock{},
		Entropy:  rand.Reader,
		Agent:    events.AgentFrom(os.Getenv("LIT_AGENT")),
		Place:    place,
		Store:    opening(place, rand.Reader),
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

// place locates the checkout at dir and reads its writer. It walks up from
// the physical directory, as git does: Getwd gives the logical $PWD, from
// which a cwd reached through a symlink misses its checkout.
func place(dir string) command.Place {
	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: fmt.Errorf("resolving the working directory: %w", err)}
	}
	c, err := git.Locate(physical)
	var none git.NotFound
	if errors.As(err, &none) {
		return command.NoCheckout{Dir: dir}
	}
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: err}
	}
	return command.InCheckout{Checkout: c, Identity: readIdentity(identityFiles(c))}
}

// opening opens the store of a minted checkout, the one place a store is
// opened: over its clone's common dir on disk, beside the code
// repository's object format, read once. Any other place has no store and
// says why.
func opening(p command.Place, entropy io.Reader) command.Opening {
	switch p := p.(type) {
	case command.InCheckout:
		return openingIn(p, entropy)
	case command.NoCheckout:
		return command.Unopened{Why: fmt.Errorf("no git checkout at or above %s", p.Dir)}
	case command.Unlocatable:
		return command.Unopened{Why: p.Err}
	}
	panic(fmt.Sprintf("root: %T is not a Place", p))
}

func openingIn(in command.InCheckout, entropy io.Reader) command.Opening {
	switch id := in.Identity.(type) {
	case command.Minted:
		format, err := git.ReadObjectFormat(in.Checkout.CommonDir)
		if err != nil {
			return command.Unopened{Why: fmt.Errorf("reading the object format: %w", err)}
		}
		return command.Opened{Store: store.Open(id.Checkout, store.DiskPorts(in.Checkout, entropy), store.DefaultSegmentBound), Format: format}
	case command.Unminted:
		var paths []string
		for _, f := range id.Files() {
			paths = append(paths, command.PathOf(f, in.Checkout))
		}
		return command.Unopened{Why: fmt.Errorf("this checkout has no identity, %s missing: lit2 admin init mints it", strings.Join(paths, " and "))}
	case command.Unreadable:
		return command.Unopened{Why: fmt.Errorf("reading %s: %w", command.PathOf(id.File, in.Checkout), id.Err)}
	}
	panic(fmt.Sprintf("root: %T is not an Identity", in.Identity))
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
