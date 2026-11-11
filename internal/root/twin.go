package root

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Twin is lit2 in process: Run over a fixed clock, seeded entropy and a
// fixed writer, with its streams and its command log kept for a test to
// read.
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

// NewTwin is a twin whose checkout is minted, so its writer is known.
func NewTwin() Twin {
	out, errOut, commands := &bytes.Buffer{}, &bytes.Buffer{}, &command.Memory{}
	return Twin{
		Process: Process{
			Clock:    &command.FixedClock{At: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)},
			Entropy:  rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 't', 'w', 'i', 'n'}),
			Agent:    events.Unlabeled{},
			Place:    command.InCheckout{Checkout: twinCheckout, Writer: command.Minted{ID: twinID}},
			Version:  "twin",
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
