package command

import (
	"errors"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memoryStore() *store.Store {
	repo := git.NewMemory()
	return store.Open(store.CheckoutID{9}, store.Ports{Files: store.NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: storetest.Entropy(1)}, store.DefaultSegmentBound)
}

func storeBlockOf(t *testing.T, f StoreFacts) map[string]any {
	t.Helper()
	m := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "get"}, Started: t0, Store: f, Outcome: cli.Done{}})
	block, ok := m["store"].(map[string]any)
	if !ok {
		t.Fatalf("the line has no store block: %v", m)
	}
	return block
}

func first(t *testing.T, block map[string]any, step string) map[string]any {
	t.Helper()
	list, _ := block[step].([]any)
	if len(list) == 0 {
		t.Fatalf("the store block has no %s: %v", step, block)
	}
	obj, _ := list[0].(map[string]any)
	return obj
}

// must is v, failing t on err.
func must[T any](t *testing.T) func(v T, err error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func TestAnEventThatNeverUsedItsStoreHasNoStoreBlock(t *testing.T) {
	if m := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}}); m["store"] != nil {
		t.Errorf("an event with no store step carries %v", m["store"])
	}
}

// TestAnInitSaysWhichIdentityFilesItCreated: the block names each file
// created or found, both readings visible.
func TestAnInitSaysWhichIdentityFilesItCreated(t *testing.T) {
	in := first(t, storeBlockOf(t, StoreFacts{Inits: []Fresh{{Checkout: store.Created, Key: store.Existing}}}), "init")
	if in["checkout_created"] != true || in["key_created"] != false {
		t.Errorf("the init line is %v; want the checkout created and the key found", in)
	}
}

func TestAFreshStoresHeadsAreZeros(t *testing.T) {
	s := memoryStore()
	h := must[store.Heads](t)(s.Heads())
	block := storeBlockOf(t, StoreFacts{Heads: []store.Heads{h}})
	heads := first(t, block, "heads")
	if heads["refs_read"] != 0.0 || heads["logs_read"] != 0.0 || heads["streams"] != 0.0 {
		t.Errorf("a fresh store's heads are %v; want zero refs, logs and streams, present", heads)
	}
}

func TestAWriteThenAPublishThenAnIdlePublishSayWhatTheyDid(t *testing.T) {
	s := memoryStore()
	appended := must[store.Appended](t)(s.Append(must[store.Batch](t)(store.NewBatch(store.Record("event")))))
	h := must[store.Heads](t)(s.Heads())
	scan := must[store.Scan](t)(s.Read(store.Range{Stream: h.Streams[0], From: 0}))
	published := must[store.Published](t)(s.Publish(must[store.Sealing](t)(s.SealOwn()).(store.Publication)))
	idle := must[store.Published](t)(s.Publish(must[store.Sealing](t)(s.SealOwn()).(store.Publication)))
	nothing := store.NothingToPublish{Ref: h.Streams[0].Ref, Log: h.Streams[0].Until.Log}
	resealed := store.Published{Commit: published.Commit, Move: store.RefCreated{}, New: []git.ObjectID{},
		Prior: store.Resealed{Why: errors.New("its publication does not read")}}
	block := storeBlockOf(t, StoreFacts{
		Heads: []store.Heads{h}, Reads: []Read{{Range: store.Range{Stream: h.Streams[0], From: 0}, Scan: scan}},
		Appends:   []store.Appended{appended},
		Publishes: []store.Publishing{published, idle, resealed, nothing},
	})
	if a := first(t, block, "append"); a["records"] != 1.0 || a["log_created"] != true || a["log_until"] != float64(appended.Until.At) {
		t.Errorf("the append is %v; want one record, the log created, its end", a)
	}
	if r, _ := block["read"].(map[string]any); r["frames_read"] != 1.0 || r["bytes_read"] != float64(appended.Until.At) || r["incomplete_tail_bytes"] != 0.0 {
		t.Errorf("the read is %v; want one frame, every byte, no tail", r)
	}
	list, _ := block["publish"].([]any)
	if len(list) != 4 {
		t.Fatalf("the block holds %d publishes; want 4", len(list))
	}
	made, again, anew, none := list[0].(map[string]any), list[1].(map[string]any), list[2].(map[string]any), list[3].(map[string]any)
	if none["outcome"] != "nothing_to_publish" || none["ref_move"] != "untouched" || none["objects_new"] != 0.0 || none["commit"] != nil {
		t.Errorf("a publish with nothing to publish is %v; want it said, the ref untouched, nothing new, no commit", none)
	}
	if made["outcome"] != "published" || made["ref_move"] != "created" || made["objects_new"] != 3.0 || made["commit"] != published.Commit.String() || made["prior"] != "none" {
		t.Errorf("the first publish is %v; want a created ref, a segment, a tree and a commit new, no prior", made)
	}
	if again["ref_move"] != "unchanged" || again["objects_new"] != 0.0 || again["bytes_new"] != 0.0 ||
		again["prior"] != "kept" || again["prior_commit"] != published.Commit.String() {
		t.Errorf("an idle publish is %v; want the ref unchanged, nothing new, the first publish kept", again)
	}
	if anew["prior"] != "resealed" || anew["prior_why"] != "its publication does not read" {
		t.Errorf("a resealed publish is %v; want resealed and its reason", anew)
	}
}

func TestStoreFactsSurviveAPanickingBody(t *testing.T) {
	appended := store.Appended{Records: 2, Bytes: 40, Log: store.Existing}
	e := Record(&FixedClock{At: t0}, inv, func(_ time.Time, _ func(cli.Summary), stored *StoreFacts, _ func(VerbsFacts)) cli.Outcome {
		stored.Appends = append(stored.Appends, appended)
		panic("after the append")
	})
	if len(e.Store.Appends) != 1 || e.Store.Appends[0] != appended || cli.ExitCode(e.Outcome) != 70 {
		t.Errorf("a panic after an append recorded %+v, exit %d; want the append kept, exit 70", e.Store, cli.ExitCode(e.Outcome))
	}
}

func TestTheOpeningSaysOpenedAndItsFormatOrWhyNot(t *testing.T) {
	opened := storeBlockOf(t, StoreFacts{Opening: Opened{Store: memoryStore(), Format: git.SHA256}})
	if o, _ := opened["open"].(map[string]any); o["state"] != "opened" || o["object_format"] != "sha256" || o["why"] != nil {
		t.Errorf("an opened store's line is %v; want opened, sha256", o)
	}
	unopened := storeBlockOf(t, StoreFacts{Opening: Unopened{Why: errors.New("this checkout has no id")}})
	if o, _ := unopened["open"].(map[string]any); o["state"] != "unopened" || o["why"] != "this checkout has no id" || o["object_format"] != nil {
		t.Errorf("an unopened store's line is %v; want unopened, and why", o)
	}
}
