package root

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
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
	Tree     *files.Memory
	Code     *git.CodeMemory
}

// The twin's checkout and the id it is minted with.
var (
	twinCheckout = git.Checkout{WorkTree: "/twin", GitDir: "/twin/.git", CommonDir: "/twin/.git", Format: git.SHA1}
	twinID       = store.CheckoutID{0x7a, 0x1e, 0x0f, 0x7b, 0x11, 0x2d, 0x4c, 0x88, 0x9e, 0x01, 0x5a, 0x6b, 0x3c, 0x2f, 0x40, 0x99}
)

// The second twin's checkout, beside the first over one store, and its id.
var (
	besideCheckout = git.Checkout{WorkTree: "/beside", GitDir: "/beside/.git", CommonDir: "/beside/.git", Format: git.SHA1}
	besideID       = store.CheckoutID{0xbe, 0x51, 0xde}
)

// twinKey is the twin's principal's key, drawn from a source of its own,
// so minting it moves no trace id either.
func twinKey() events.Key { return keyOf([32]byte{'l', 'i', 't', '2', ' ', 'k', 'e', 'y'}) }

// besideKey is the second twin's, another principal's.
func besideKey() events.Key { return keyOf([32]byte{'b', 'e', 's', 'i', 'd', 'e'}) }

func keyOf(seed [32]byte) events.Key {
	key, err := events.NewKey(rand.NewChaCha8(seed))
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
	t.Code.Commit(map[string][]byte{verbs.WorkspacePath(): assets.DefaultWorkspace.Bytes()})
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
func NewFreshTwin() Twin { return freshTwinOver(files.NewMemory(store.LogsDir(twinCheckout))) }

// freshTwinOver is NewFreshTwin whose store's logs are logs.
func freshTwinOver(logs files.Files) Twin {
	// the store draws from its own source, so a log it creates moves no
	// trace id the twin mints
	stored := rand.NewChaCha8([32]byte{'l', 'i', 't', '2', ' ', 's', 't', 'o', 'r', 'e'})
	ports := store.RepositoryInMemory(logs, stored)
	return twinOver(ports, twinCheckout, twinID, twinKey(), [32]byte{'l', 'i', 't', '2', ' ', 't', 'w', 'i', 'n'})
}

// NewTwinBeside is a second checkout over t's store, another principal's,
// minted and before its admin init: a second person's clone, every event
// either writes read by both, its work tree holding the FoundingFile t's
// holds, as a clone of the repository t committed it to does.
func NewTwinBeside(t Twin) Twin {
	beside := twinOver(t.Process.Stores(twinCheckout), besideCheckout, besideID, besideKey(), [32]byte{'b', 'e', 's', 'i', 'd', 'e', ' ', 't', 'w', 'i', 'n'})
	named, err := foundingFileIn(t.Tree)
	if err != nil {
		panic(err)
	}
	if f, founded := named.(fold.FoundedBy); founded {
		durably(beside.Tree.CreateOnce(foundingFile, f.Enrollment.FileText()))
	}
	return beside
}

// twinOver is a minted checkout c, id and key, over the store ports hold,
// its entropy drawn from seed and its work tree and code repository empty.
func twinOver(ports store.Ports, c git.Checkout, id store.CheckoutID, key events.Key, seed [32]byte) Twin {
	out, errOut, commands := &bytes.Buffer{}, &bytes.Buffer{}, &command.Memory{}
	entropy := rand.NewChaCha8(seed)
	tree, code := files.NewMemory(litDir(c)), &git.CodeMemory{}
	// the twin is minted: its identity files hold its id and key, so init
	// finds them there
	shared, private, pins := files.NewMemory(command.DirOf(command.PrincipalFile{}, c)), files.NewMemory(command.DirOf(command.IDFile{}, c)), files.NewMemory(pinDir(c))
	durably(private.CreateOnce(command.NameOf(command.IDFile{}), id.FileText()))
	durably(shared.CreateOnce(command.NameOf(command.PrincipalFile{}), key.FileText()))
	return Twin{
		Process: Process{
			Clock:         &command.FixedClock{At: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)},
			Entropy:       entropy,
			Agent:         events.Unlabeled{},
			Locate:        func() command.Located { return command.AtCheckout{Checkout: c} },
			Stores:        storesOf(c, ports),
			WorkTree:      func(git.Checkout) files.Files { return tree },
			Code:          func(git.Checkout) git.Code { return code },
			Git:           func() []git.Spawn { return []git.Spawn{} },
			Fold:          foldEnv(2),
			Reader:        twinReader(),
			Commands:      commands,
			IdentityFiles: func(git.Checkout) (files.Files, files.Files) { return shared, private },
			Pin:           func(git.Checkout) files.Files { return pins },
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

// storesOf is the store ports of c's clone, as Main's are of each
// checkout's, so a run that asks for another checkout's store fails as a
// wrong path through Main's would read the wrong clone.
func storesOf(c git.Checkout, ports store.Ports) func(git.Checkout) store.Ports {
	return func(at git.Checkout) store.Ports {
		if at != c {
			panic(fmt.Sprintf("root: the twin holds the store of %s, not %s", c.CommonDir, at.CommonDir))
		}
		return ports
	}
}

// Forge is a modified client's write: verb on ops compiled and minted as
// lit2 mints them and appended with no verdict asked of the fold, the
// event a test hands every replica for the fold to refuse
// `[LAW:single-enforcer]`.
func (t Twin) Forge(verb verbs.Verb, ops ...verbs.Operand) error {
	rec := command.Recorder{Initialized: func(command.Opens) {}, Stored: &command.StoreFacts{}, Configured: func(command.ConfigFacts) {}, Told: func(command.VerbsFacts) {}, Policy: &command.PolicyFacts{}}
	var forged error
	w := &writes{stored: rec.Stored}
	out := body{p: t.Process, now: t.Process.Clock.Now(), place: placeOf(t.Process), rec: rec, w: w}.verb(func(v inForce) cli.Outcome {
		c := verbs.Compile(v.world(v.folded.State), verb, ops, func() (string, error) { return "", nil })
		if no, refused := c.Refusal(); refused {
			forged = no.Err()
			return cli.Done{}
		}
		m, err := v.mint(c.Drafts, v.ref)
		if err == nil {
			err = v.append(m)
		}
		forged = err
		return cli.Done{}
	})
	if f, isFailed := w.end(out).(cli.Failed); isFailed {
		return f.Failure
	}
	return forged
}

// Store is the twin's store as a run opens it, from the writer its
// identity files hold, for a test to append to or read directly.
func (t Twin) Store() *store.Store { return t.writer().store }

// Principal is the principal the twin's identity files hold.
func (t Twin) Principal() events.PrincipalID { return t.writer().id.Key.Principal() }

// writer is the twin's writer and store as a run opens them, read from
// its place now, so a test that changed the place gets the store a run
// would open there.
func (t Twin) writer() writer {
	switch o := command.OpeningAt(placeOf(t.Process)).(type) {
	case command.Opens:
		return t.Process.writerOf(o)
	case command.NoStore:
		panic(fmt.Sprintf("root: the twin's place opens no store: %v", o.Why))
	}
	panic("root: an Opening that is neither Opens nor NoStore")
}

// durably panics unless p placed its file durably: the twin's setup is
// over twins whose writes are.
func durably(p files.Placing) {
	if _, ok := p.(files.Placed); !ok {
		panic(fmt.Sprintf("root: the twin's setup create is %#v; want Placed", p))
	}
}
