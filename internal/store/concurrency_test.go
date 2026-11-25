package store_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// gate holds racers at one log's lookup: each racer's logs are an arm of
// it, and an arm's first read of the log while it is absent is that
// racer's arrival, held until every racer has arrived, so every racer is
// past its lookup before any creates the log
// [LAW:no-ambient-temporal-coupling]: the race of first appends is forced,
// never left to the scheduler. A wait that outlasts meetWithin fails the
// read, saying how many arrived.
type gate struct {
	log      files.Name
	racers   int
	deadline time.Time
	met      chan struct{}
	mu       sync.Mutex
	arrived  int
}

// meetWithin is how long racers have to meet, well inside any test
// binary's timeout, so a racer that never arrives is said by the gate.
const meetWithin = 10 * time.Second

func newGate(log files.Name, racers int) *gate {
	return &gate{log: log, racers: racers, deadline: time.Now().Add(meetWithin), met: make(chan struct{})}
}

// arm is one racer's logs, logs held at the gate.
func (g *gate) arm(logs files.Files) *arm { return &arm{Files: logs, g: g} }

type arm struct {
	files.Files
	g       *gate
	arrived bool
}

func (a *arm) ReadAt(n files.Name, off, size uint64) ([]byte, error) {
	b, err := a.Files.ReadAt(n, off, size)
	if n != a.g.log || !errors.As(err, new(files.NotFound)) {
		return b, err
	}
	if !a.arrived {
		a.arrived = true
		a.g.arrive()
	}
	select {
	case <-a.g.met:
		return b, err
	case <-time.After(time.Until(a.g.deadline)):
		return nil, fmt.Errorf("the racers did not meet within %v: %d of %d arrived", meetWithin, a.g.count(), a.g.racers)
	}
}

func (g *gate) arrive() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.arrived++
	if g.arrived == g.racers {
		close(g.met)
	}
}

func (g *gate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.arrived
}

// Appenders racing to one log's first append, every one held until all
// have read the log absent, make one log: one create's header is the
// log's, every other create finds it Existing, and every record reads
// back once.
func TestRacingFirstAppendsMakeOneLog(t *testing.T) {
	firsts := storetest.Numbered(t, 8, "first") // one per racer
	p := fresh()
	g := newGate(store.LogName(ida), len(firsts))
	appended := make([]store.Appending, len(firsts))
	var wg sync.WaitGroup
	for i, bt := range firsts {
		ports := store.WithFiles(p.ports, g.arm(p.logs))
		wg.Go(func() { appended[i] = store.Open(ida, ports, store.DefaultSegmentBound).Append(bt) })
	}
	wg.Wait()
	select {
	case <-g.met:
	default:
		t.Fatalf("the racers never met at the log's lookup: %d of %d arrived", g.count(), g.racers)
	}
	made := map[files.Made]int{}
	for i, a := range appended {
		got, ok := a.(store.Appended)
		if !ok {
			t.Fatalf("racer %d's append = %#v; want Appended", i, a)
		}
		c, ok := got.Log.(store.LogCreated)
		if !ok {
			t.Fatalf("racer %d's log is %#v; want LogCreated, each racer having read it absent", i, got.Log)
		}
		made[c.Made]++
	}
	if made[files.Created] != 1 || made[files.Existing] != len(firsts)-1 {
		t.Errorf("the racers' creates made %v; want one Created and %d Existing", made, len(firsts)-1)
	}
	got, want := storetest.Read(storetest.Whole(t, store.Open(ida, p.ports, store.DefaultSegmentBound), store.WriterRef{Checkout: ida})), storetest.All(firsts)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the log reads %v; want each racer's record once, %v", got, want)
	}
}

// TestTheTwinsUnderLoad is a load, not a race proof: appenders at once,
// and a long log published, at the volumes the store's suite once ran
// over git, here over the twins. The scheduler interleaves the appenders
// as it will; TestRacingFirstAppendsMakeOneLog forces the race there is.
func TestTheTwinsUnderLoad(t *testing.T) {
	t.Run("200 appends from 8 appenders at once lose no record", func(t *testing.T) {
		p := fresh()
		var want []string
		var wg sync.WaitGroup
		for g := range 8 {
			bs := storetest.Numbered(t, 25, fmt.Sprintf("g%d", g))
			want = append(want, storetest.All(bs)...)
			wg.Go(func() {
				s := store.Open(ida, p.ports, store.DefaultSegmentBound)
				for _, bt := range bs {
					if got, ok := s.Append(bt).(store.Appended); !ok {
						t.Errorf("Append = %#v; want Appended", got)
						return
					}
				}
			})
		}
		wg.Wait()
		scan := storetest.Whole(t, store.Open(ida, p.ports, store.DefaultSegmentBound), store.WriterRef{Checkout: ida})
		got := storetest.Read(scan)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || len(scan.Torn) != 0 {
			t.Errorf("%d concurrent appends read back %d records and %d torn spans; want each record once, none torn", len(want), len(got), len(scan.Torn))
		}
	})
	t.Run("a log of 1000 appends publishes as one commit whose stream reads back the log", func(t *testing.T) {
		p := fresh()
		s := store.Open(ida, p.ports, store.DefaultSegmentBound)
		bs := storetest.Numbered(t, 1000, "event")
		storetest.AppendAll(t, s, bs...)
		out := storetest.PublishOwn(t, s)
		if out.Move != (store.RefCreated{}) || out.Prior != (store.NoPrior{}) || out.Kept != 0 || out.Sealed == 0 || len(out.New) != out.Sealed+2 {
			t.Errorf("the publish was %+v; want a created ref, no prior, and only its segments, one tree and one commit new", out)
		}
		reader := store.Open(idb, fetched(p).ports, store.DefaultSegmentBound)
		if got := storetest.Read(storetest.Whole(t, reader, store.WriterRef{Checkout: ida})); !slices.Equal(got, storetest.All(bs)) {
			t.Errorf("the publication reads back %d records; want the log's %d", len(got), len(bs))
		}
	})
}
