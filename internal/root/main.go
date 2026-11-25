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
	return Run(Process{
		Clock:         systemClock{},
		Entropy:       rand.Reader,
		Agent:         events.AgentFrom(os.Getenv("LIT_AGENT")),
		Locate:        here(g),
		Stores:        diskStores(g, rand.Reader, u),
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

// here locates the checkout of the working directory through g. A working
// directory that has no path is ".", the one name it still has.
func here(g *git.Git) func() command.Located {
	return func() command.Located {
		dir, err := os.Getwd()
		if err != nil {
			return command.Unlocatable{Dir: ".", Err: fmt.Errorf("reading the working directory: %w", err)}
		}
		return locate(g, dir)
	}
}

// locate is the checkout g finds from dir, none, or why the search failed.
func locate(g *git.Git, dir string) command.Located {
	c, err := g.Locate(dir)
	if errors.As(err, new(git.NotFound)) {
		return command.NoCheckout{Dir: dir}
	}
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: err}
	}
	return command.Found{Checkout: c}
}

// diskStores is the ports of a checkout's clone's store on disk, lit's
// repository written by g, its syncs kept by u.
func diskStores(g *git.Git, entropy io.Reader, u *files.Durability) func(git.Checkout) store.Ports {
	return func(c git.Checkout) store.Ports { return store.DiskPorts(g, c, entropy, u) }
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
