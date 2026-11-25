package command

import (
	"errors"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memoryStore() *store.Store {
	repo := git.NewMemory()
	return store.Open(store.CheckoutID{9}, store.MemoryPorts(files.NewMemory(), repo, repo, storetest.Entropy(1)), store.DefaultSegmentBound)
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

// TestACreateSaysWhatItPlacedAndHowDurably: the block names each file by
// path, created or found, durable or with the sync that failed, or not
// placed and why, with the temps each reaped and left.
func TestACreateSaysWhatItPlacedAndHowDurably(t *testing.T) {
	block := storeBlockOf(t, StoreFacts{Creates: []Creation{
		{Path: "/w/.git/lit/id", Placing: files.Placed{Made: files.Created, Swept: files.Swept{Reaped: []string{".tmp-id-0"}, Left: []files.Leftover{}}}},
		{Path: "/w/.git/lit/key", Placing: files.PlacedNotDurable{Made: files.Existing, Swept: files.Swept{Reaped: []string{}, Left: []files.Leftover{{Name: ".tmp-key-1", Why: errors.New("busy")}}}, Why: errors.New("fsync failed")}},
		{Path: "/w/.lit/founding", Placing: files.NotPlaced{Why: errors.New("a directory")}},
	}})
	list, _ := block["create"].([]any)
	if len(list) != 3 {
		t.Fatalf("the block holds %v; want three creates", block["create"])
	}
	id, key, founding := list[0].(map[string]any), list[1].(map[string]any), list[2].(map[string]any)
	if id["path"] != "/w/.git/lit/id" || id["outcome"] != "placed" || id["made"] != "created" || id["why"] != nil ||
		len(id["temps_reaped"].([]any)) != 1 || len(id["temps_left"].([]any)) != 0 {
		t.Errorf("a durable create is %v; want placed, created, its one temp reaped and none left", id)
	}
	left, _ := key["temps_left"].([]any)
	if key["outcome"] != "not_durable" || key["made"] != "existing" || key["why"] != "fsync failed" || len(left) != 1 ||
		left[0].(map[string]any)["name"] != ".tmp-key-1" || left[0].(map[string]any)["why"] != "busy" {
		t.Errorf("a create whose sync failed is %v; want not_durable, existing, the sync's error, its temp left and why", key)
	}
	if founding["outcome"] != "not_placed" || founding["why"] != "a directory" || founding["made"] != nil {
		t.Errorf("a create that placed nothing is %v; want not_placed and why, made by none", founding)
	}
}

func TestAFreshStoresHeadsAreZeros(t *testing.T) {
	s := memoryStore()
	h := must[store.Heads](t)(s.Heads())
	block := storeBlockOf(t, StoreFacts{Heads: []store.Heads{h}})
	heads := first(t, block, "heads")
	if temps, ok := heads["temps"].([]any); heads["refs_read"] != 0.0 || heads["logs_read"] != 0.0 || heads["streams"] != 0.0 || !ok || len(temps) != 0 {
		t.Errorf("a fresh store's heads are %v; want zero refs, logs and streams, and no temp, present", heads)
	}
}

// The heads line says the logs directory's temps, which no create of their
// name has reaped.
func TestTheHeadsSayTheLogsTemps(t *testing.T) {
	heads := first(t, storeBlockOf(t, StoreFacts{Heads: []store.Heads{{Temps: []string{".tmp-0d-0"}}}}), "heads")
	if temps, _ := heads["temps"].([]any); len(temps) != 1 || temps[0] != ".tmp-0d-0" {
		t.Errorf("the heads line is %v; want the temp .tmp-0d-0 said", heads)
	}
}

func TestAWriteThenAPublishThenAnIdlePublishSayWhatTheyDid(t *testing.T) {
	s := memoryStore()
	appended := s.Append(must[store.Batch](t)(store.NewBatch(store.Record("event")))).(store.Appended)
	h := must[store.Heads](t)(s.Heads())
	scan := must[store.Scan](t)(s.Read(store.Range{Stream: h.Streams[0], From: 0}))
	published := must[store.Published](t)(s.Publish(must[store.Sealing](t)(s.SealOwn()).(store.Publication)))
	idle := must[store.Published](t)(s.Publish(must[store.Sealing](t)(s.SealOwn()).(store.Publication)))
	nothing := store.NothingToPublish{Ref: h.Streams[0].Ref, Log: h.Streams[0].Until.Log}
	resealed := store.Published{Commit: published.Commit, Move: store.RefCreated{}, New: []git.ObjectID{},
		Prior: store.Resealed{Why: errors.New("its publication does not read")}}
	block := storeBlockOf(t, StoreFacts{
		Heads: []store.Heads{h}, Reads: []store.Scanned{{Range: store.Range{Stream: h.Streams[0], From: 0}, Scan: scan}},
		Appends: []store.Appending{appended, store.AppendedNotDurable{Appended: appended, Why: errors.New("fsync failed")},
			store.NotAppended{Why: errors.New("no space")}},
		Publishes: []store.Publishing{published, idle, resealed, nothing},
	})
	appends, _ := block["append"].([]any)
	if len(appends) != 3 {
		t.Fatalf("the block holds %v; want three appends", block["append"])
	}
	durable, landed, none := appends[0].(map[string]any), appends[1].(map[string]any), appends[2].(map[string]any)
	if durable["outcome"] != "appended" || durable["records"] != 1.0 || durable["log_created"] != true || durable["log_until"] != float64(appended.Until.At) || durable["why"] != nil {
		t.Errorf("the append is %v; want appended, one record, the log created, its end", durable)
	}
	if landed["outcome"] != "not_durable" || landed["records"] != 1.0 || landed["log_until"] != float64(appended.Until.At) || landed["why"] != "fsync failed" {
		t.Errorf("an append whose sync failed is %v; want not_durable, the frame's record and end, and the sync's error", landed)
	}
	if none["outcome"] != "not_appended" || none["why"] != "no space" || none["records"] != nil {
		t.Errorf("an append that wrote nothing is %v; want not_appended and why, and no record counted", none)
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
	appended := store.Appended{Records: 2, Bytes: 40, Log: files.Existing}
	e := Record(&FixedClock{At: t0}, noGit, inv, func(_ time.Time, r Recorder) cli.Outcome {
		r.Stored.Appends = append(r.Stored.Appends, appended)
		panic("after the append")
	})
	if len(e.Store.Appends) != 1 || e.Store.Appends[0].(store.Appended).Bytes != appended.Bytes || cli.ExitCode(e.Outcome) != 70 {
		t.Errorf("a panic after an append recorded %+v, exit %d; want the append kept, exit 70", e.Store, cli.ExitCode(e.Outcome))
	}
}

func TestTheOpeningSaysOpenedOrWhyNot(t *testing.T) {
	opened := storeBlockOf(t, StoreFacts{Opening: Opened{Store: memoryStore()}})
	if o, _ := opened["open"].(map[string]any); o["state"] != "opened" || o["why"] != nil {
		t.Errorf("an opened store's line is %v; want opened", o)
	}
	unopened := storeBlockOf(t, StoreFacts{Opening: Unopened{Why: errors.New("this checkout has no id")}})
	if o, _ := unopened["open"].(map[string]any); o["state"] != "unopened" || o["why"] != "this checkout has no id" {
		t.Errorf("an unopened store's line is %v; want unopened, and why", o)
	}
}

// The checkout's object format is on the place line, beside its paths,
// and no temp when none is left.
func TestThePlaceSaysTheCheckoutsObjectFormat(t *testing.T) {
	place, _ := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "get"}, Started: t0, Outcome: cli.Done{}})["place"].(map[string]any)
	c, _ := place["checkout"].(map[string]any)
	if temps, ok := c["temps"].([]any); c["object_format"] != "sha256" || c["git_dir"] != "/w/.git" || !ok || len(temps) != 0 {
		t.Errorf("the place line is %v; want the checkout's paths and its format, sha256, and no temp", place)
	}
}

// The place line says each of the checkout's files with temps beside it,
// and each whose temps did not read, and why.
func TestThePlaceSaysTheCheckoutsLeftoverTemps(t *testing.T) {
	in := inv
	checkout := in.Place.(InCheckout)
	checkout.Temps = []Leftovers{TempsBeside{Path: "/w/.git/lit/key", Temps: []string{".tmp-key-0"}}, TempsUnread{Path: "/w/.lit/founding", Why: errors.New("a symlink")}}
	in.Place = checkout
	place, _ := decode(t, Event{Invocation: in, Command: cli.Summary{Name: "get"}, Started: t0, Outcome: cli.Done{}})["place"].(map[string]any)
	c, _ := place["checkout"].(map[string]any)
	temps, _ := c["temps"].([]any)
	if len(temps) != 2 {
		t.Fatalf("the place line is %v; want two files' temps", place)
	}
	beside, unread := temps[0].(map[string]any), temps[1].(map[string]any)
	if got, _ := beside["temps"].([]any); beside["path"] != "/w/.git/lit/key" || len(got) != 1 || got[0] != ".tmp-key-0" {
		t.Errorf("the key's temps are %v; want its path and .tmp-key-0", beside)
	}
	if unread["path"] != "/w/.lit/founding" || unread["error"] != "a symlink" {
		t.Errorf("the founding file's temps are %v; want its path and why they did not read", unread)
	}
}
