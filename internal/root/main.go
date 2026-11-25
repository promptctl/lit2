package root

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
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
	// [LAW:no-silent-failure] a build with no version cannot say which
	// workspace versions it reads, so it does not run. Nothing is recorded,
	// since an invocation records the version it ran as, so it exits with
	// status 2, outside every recorded class, which D9 reads as unrecorded.
	reader, err := config.ReadReaderVersion(buildStamp())
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "lit2: "+err.Error()+"\n")
		return 2
	}
	g := git.Find(os.Environ())
	// [LAW:single-enforcer] one keeper of failed syncs for the process,
	// handed to every disk port, so a failure one port saw is answered to
	// every other over that directory
	u := files.NewDurability()
	place, open := here(g, u), diskStore(g, rand.Reader, u)
	return Run(Process{
		Clock:         systemClock{},
		Entropy:       rand.Reader,
		Agent:         events.AgentFrom(os.Getenv("LIT_AGENT")),
		Place:         place,
		Store:         opening(place, open),
		Open:          open,
		WorkTree:      workTree(u),
		Code:          g.CodeAt,
		Git:           g.Spawns,
		Fold:          foldEnv(runtime.GOMAXPROCS(0)),
		Reader:        reader,
		Commands:      commandLog(),
		IdentityFiles: identityFiles(u),
		Pin:           pinFiles(u),
		In:            os.Stdin,
		Out:           os.Stdout,
		Err:           os.Stderr,
	}, args)
}

// workTree is the files of c's .lit directory on disk, the real
// directory at that name in the work tree: a .lit that is a symlink,
// wherever it points, is refused, so nothing a work tree commits writes
// into the store or anywhere else. Its syncs are kept by u.
func workTree(u *files.Durability) func(git.Checkout) files.Files {
	return func(c git.Checkout) files.Files { return files.At(litDir(c), u) }
}

// litDir is c's .lit directory, in its work tree.
func litDir(c git.Checkout) files.Dir { return files.DirAt(c.WorkTree, verbs.LitDir()) }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// here is the place of the working directory, its identity read through
// ports whose syncs u keeps.
func here(g *git.Git, u *files.Durability) command.Place {
	dir, err := os.Getwd()
	if err != nil {
		return command.Unlocatable{Err: fmt.Errorf("reading the working directory: %w", err)}
	}
	return place(g, dir, u)
}

// place is the checkout git locates at dir, and its writer.
func place(g *git.Git, dir string, u *files.Durability) command.Place {
	c, err := g.Locate(dir)
	var none git.NotFound
	if errors.As(err, &none) {
		return command.NoCheckout{Dir: dir}
	}
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: err}
	}
	return command.InCheckout{Checkout: c, Identity: readIdentity(identityFiles(u)(c))}
}

// opening opens the store of a minted checkout through open, the one place
// a run's store is opened. Any other place has no store and says why.
func opening(p command.Place, open func(git.Checkout, command.Minted) command.Opening) command.Opening {
	switch p := p.(type) {
	case command.InCheckout:
		switch id := p.Identity.(type) {
		case command.Minted:
			return open(p.Checkout, id)
		case command.Unminted:
			return command.Unopened{Why: fmt.Errorf("this checkout has no identity, %s missing: lit2 admin init mints it", strings.Join(id.Paths(p.Checkout), " and "))}
		case command.Unreadable:
			return command.Unopened{Why: fmt.Errorf("reading %s: %w", command.PathOf(id.File, p.Checkout), id.Err)}
		}
		panic(fmt.Sprintf("root: %T is not an Identity", p.Identity))
	case command.NoCheckout:
		return command.Unopened{Why: fmt.Errorf("no git checkout at or above %s", p.Dir)}
	case command.Unlocatable:
		return command.Unopened{Why: p.Err}
	}
	panic(fmt.Sprintf("root: %T is not a Place", p))
}

// diskStore opens a minted checkout's store over its clone's common dir on
// disk, lit's repository written by g, its syncs kept by u.
func diskStore(g *git.Git, entropy io.Reader, u *files.Durability) func(git.Checkout, command.Minted) command.Opening {
	return func(c git.Checkout, id command.Minted) command.Opening {
		return command.Opened{Store: store.Open(id.Checkout, store.DiskPorts(g, c, entropy, u), store.DefaultSegmentBound)}
	}
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

// buildStamp is the main module's version as Go stamped it. A build with no
// version stamp reads "(devel)", or nothing when there is no build info at all;
// config.ReadReaderVersion refuses both.
func buildStamp() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}
