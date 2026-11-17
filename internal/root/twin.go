package root

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// Twin is lit2 in process: Run over a fixed clock, seeded entropy, a
// fixed writer and a store over the in-memory twins, with its streams, its
// command log, its work tree and its code repository kept for a test to
// read and write.
type Twin struct {
	Process  Process
	Out, Err *bytes.Buffer
	Commands *command.Memory
	Tree     *store.MemoryFiles
	Code     *git.CodeMemory
}

// The twin's checkout and the id it is minted with.
var (
	twinCheckout = git.Checkout{WorkTree: "/twin", GitDir: "/twin/.git", CommonDir: "/twin/.git"}
	twinID       = store.CheckoutID{0x7a, 0x1e, 0x0f, 0x7b, 0x11, 0x2d, 0x4c, 0x88, 0x9e, 0x01, 0x5a, 0x6b, 0x3c, 0x2f, 0x40, 0x99}
)

// twinKey is the twin's principal's key, drawn from a source of its own,
// so minting it moves no trace id either.
func twinKey() events.Key {
	key, err := events.NewKey(rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 'k', 'e', 'y'}))
	if err != nil {
		panic(fmt.Sprintf("root: ChaCha8 failed a read: %v", err))
	}
	return key
}

// twinReader is the release the twin runs as.
func twinReader() config.ReaderVersion {
	r, err := config.ReadReaderVersion("v0.4.0")
	if err != nil {
		panic(err)
	}
	return r
}

// NewTwin is a twin as admin init leaves a checkout: minted, so its
// writer is known, with the default profile in its work tree, committed,
// and applied as version one. Its streams and command log hold nothing of
// the init.
func NewTwin() Twin {
	t := NewFreshTwin()
	t.Code.Commit(map[string][]byte{verbs.WorkspaceFile: assets.DefaultWorkspace.Bytes()})
	if status := Run(t.Process, []string{"admin", "init"}); status != 0 {
		panic(fmt.Sprintf("root: the twin's admin init exited %d: %s", status, t.Err))
	}
	t.Out.Reset()
	t.Err.Reset()
	t.Commands = &command.Memory{}
	t.Process.Commands = t.Commands
	return t
}

// NewFreshTwin is a twin whose checkout is minted and whose store, work
// tree and code repository hold nothing: a checkout before admin init
// applies version one.
func NewFreshTwin() Twin {
	out, errOut, commands := &bytes.Buffer{}, &bytes.Buffer{}, &command.Memory{}
	// the store draws from its own source, so a log it creates moves no
	// trace id the twin mints
	entropy := rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 't', 'w', 'i', 'n'})
	stored := rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 's', 't', 'o', 'r', 'e'})
	repo := git.NewMemory()
	ports := store.Ports{Files: store.NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: stored}
	opened := command.Opened{Store: store.Open(twinID, ports, store.DefaultSegmentBound), Format: git.SHA1}
	tree, code := store.NewMemoryFiles(), &git.CodeMemory{}
	// the twin is minted: its identity files hold its id and key, so init
	// finds them there
	shared, private := store.NewMemoryFiles(), store.NewMemoryFiles()
	if _, err := private.CreateOnce(command.NameOf(command.IDFile{}), twinID.FileText()); err != nil {
		panic(err)
	}
	if _, err := shared.CreateOnce(command.NameOf(command.PrincipalFile{}), twinKey().FileText()); err != nil {
		panic(err)
	}
	return Twin{
		Process: Process{
			Clock:         &command.FixedClock{At: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)},
			Entropy:       entropy,
			Agent:         events.Unlabeled{},
			Place:         command.InCheckout{Checkout: twinCheckout, Identity: command.Minted{Checkout: twinID, Key: twinKey()}},
			Store:         opened,
			Open:          func(git.Checkout, command.Minted) command.Opening { return opened },
			WorkTree:      func(git.Checkout) store.Files { return tree },
			Code:          func(git.Checkout, git.ObjectFormat) git.Code { return code },
			Fold:          foldEnv(2),
			Reader:        twinReader(),
			Commands:      commands,
			IdentityFiles: func(git.Checkout) (store.Files, store.Files) { return shared, private },
			In:            strings.NewReader(""),
			Out:           out,
			Err:           errOut,
		},
		Out:      out,
		Err:      errOut,
		Commands: commands,
		Tree:     tree,
		Code:     code,
	}
}
