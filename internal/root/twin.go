package root

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Twin is lit2 in process: Run over a fixed clock, seeded entropy, a
// fixed writer and a store over the in-memory twins, with its streams and
// its command log kept for a test to read.
type Twin struct {
	Process  Process
	Out, Err *bytes.Buffer
	Commands *command.Memory
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

// NewTwin is a twin whose checkout is minted, so its writer is known.
func NewTwin() Twin {
	out, errOut, commands := &bytes.Buffer{}, &bytes.Buffer{}, &command.Memory{}
	// the store draws from its own source, so a log it creates moves no
	// trace id the twin mints
	entropy := rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 't', 'w', 'i', 'n'})
	stored := rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 's', 't', 'o', 'r', 'e'})
	repo := git.NewMemory()
	ports := store.Ports{Files: store.NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: stored}
	return Twin{
		Process: Process{
			Clock:    &command.FixedClock{At: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)},
			Entropy:  entropy,
			Agent:    events.Unlabeled{},
			Place:    command.InCheckout{Checkout: twinCheckout, Identity: command.Minted{Checkout: twinID, Key: twinKey()}},
			Store:    command.Opened{Store: store.Open(twinID, ports, store.DefaultSegmentBound), Format: git.SHA1},
			Fold:     foldEnv(2),
			Reader:   twinReader(),
			Commands: commands,
			In:       strings.NewReader(""),
			Out:      out,
			Err:      errOut,
		},
		Out:      out,
		Err:      errOut,
		Commands: commands,
	}
}
