package command

import (
	"errors"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memoryStore() *store.Store {
	return store.Open(store.CheckoutID{9}, store.MemoryRepositoryPorts(filestest.Memory(), storetest.Entropy(1)), store.DefaultSegmentBound)
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
		{Path: "/w/.git/lit/id", Placing: files.Placed{Made: files.Created, Reap: files.Reap{Reaped: []string{".tmp-id-0"}}}},
		{Path: "/w/.git/lit/key", Placing: files.PlacedNotDurable{Placed: files.Placed{Made: files.Existing, Reap: files.Reap{Left: []files.Leftover{{Name: ".tmp-key-1", Why: errors.New("busy")}}}}, Why: errors.New("fsync failed")}},
		{Path: "/w/.lit/founding", Placing: files.NotPlaced{Why: errors.New("a directory"), Left: []files.Leftover{{Name: ".tmp-founding-0", Why: errors.New("busy")}}}},
		{Path: "/w/.git/lit/pin", Placing: files.Placed{Made: files.Created, Reap: files.Reap{Unlisted: errors.New("the directory would not read")}}},
	}})
	list, _ := block["create"].([]any)
	if len(list) != 4 {
		t.Fatalf("the block holds %v; want four creates", block["create"])
	}
	id, key, founding, pin := list[0].(map[string]any), list[1].(map[string]any), list[2].(map[string]any), list[3].(map[string]any)
	if id["path"] != "/w/.git/lit/id" || id["outcome"] != "placed" || id["made"] != "created" || id["why"] != nil ||
		len(id["temps_reaped"].([]any)) != 1 || len(id["temps_left"].([]any)) != 0 || id["unlisted"] != nil {
		t.Errorf("a durable create is %v; want placed, created, its one temp reaped and none left", id)
	}
	left, _ := key["temps_left"].([]any)
	if key["outcome"] != "not_durable" || key["made"] != "existing" || key["why"] != "fsync failed" || len(left) != 1 ||
		left[0].(map[string]any)["name"] != ".tmp-key-1" || left[0].(map[string]any)["why"] != "busy" {
		t.Errorf("a create whose sync failed is %v; want not_durable, existing, the sync's error, its temp left and why", key)
	}
	ownLeft, _ := founding["temps_left"].([]any)
	if founding["outcome"] != "not_placed" || founding["why"] != "a directory" || founding["made"] != nil || len(ownLeft) != 1 ||
		ownLeft[0].(map[string]any)["name"] != ".tmp-founding-0" {
		t.Errorf("a create that placed nothing is %v; want not_placed and why, made by none, its own temp left", founding)
	}
	if pin["outcome"] != "placed" || pin["unlisted"] != "the directory would not read" {
		t.Errorf("a create whose directory would not list is %v; want placed, and why the reap could not list it", pin)
	}
}

func TestAFreshStoresHeadsAreZeros(t *testing.T) {
	s := memoryStore()
	h := must[store.Heads](t)(s.Heads())
	block := storeBlockOf(t, StoreFacts{Heads: []store.Heads{h}})
	heads := first(t, block, "heads")
	if heads["refs_read"] != 0.0 || heads["logs_read"] != 0.0 || heads["streams"] != 0.0 {
		t.Errorf("a fresh store's heads are %v; want zero refs, logs and streams", heads)
	}
}

// The store block says each directory scanned, with the temps no create
// of their name has reaped, none or some, and each not scanned, and why;
// and no directory when no scan was meant.
func TestTheStoreBlockSaysEachDirectoryScannedOrWhyNot(t *testing.T) {
	block := storeBlockOf(t, StoreFacts{Temps: []Leftovers{
		TempsScanned{Dir: "/w/.git/lit/store/log", Temps: []string{".tmp-0d-0"}},
		TempsScanned{Dir: "/w/.git/lit"},
		TempsNotScanned{Dir: "/w/.lit", Why: errors.New("a symlink")},
	}})
	temps, _ := block["temps"].([]any)
	if len(temps) != 3 {
		t.Fatalf("the store block is %v; want three directories", block)
	}
	some, empty, unread := temps[0].(map[string]any), temps[1].(map[string]any), temps[2].(map[string]any)
	if got, _ := some["temps"].([]any); some["dir"] != "/w/.git/lit/store/log" || some["scan"] != "scanned" || len(got) != 1 || got[0] != ".tmp-0d-0" {
		t.Errorf("the logs' temps are %v; want the directory scanned and .tmp-0d-0", some)
	}
	if got, ok := empty["temps"].([]any); empty["dir"] != "/w/.git/lit" || empty["scan"] != "scanned" || !ok || len(got) != 0 {
		t.Errorf("a directory with no temp is %v; want it scanned, its temps an empty list", empty)
	}
	if unread["dir"] != "/w/.lit" || unread["scan"] != "not_scanned" || unread["error"] != "a symlink" || unread["temps"] != nil {
		t.Errorf("the unread temps are %v; want the directory not scanned and why", unread)
	}
	if none := storeBlockOf(t, StoreFacts{Opening: Opened{Store: memoryStore()}}); none["temps"] != nil {
		t.Errorf("a store block with no directory scanned says %v", none["temps"])
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
		Appends: []store.Appending{appended, store.AppendedNotDurable{Written: appended.Written, Log: appended.Log, Why: errors.New("fsync failed")},
			store.NotAppended{Log: store.NoLog{Left: []files.Leftover{{Name: ".tmp-09-0", Why: errors.New("busy")}}}, Why: errors.New("no space")},
			store.Appended{Written: store.Written{Records: 1}, Log: store.LogCreated{Placed: files.Placed{Made: files.Existing, Reap: files.Reap{Reaped: []string{".tmp-09-1"}}}}}},
		Publishes: []store.Publishing{published, idle, resealed, nothing},
	})
	appends, _ := block["append"].([]any)
	if len(appends) != 4 {
		t.Fatalf("the block holds %v; want four appends", block["append"])
	}
	durable, landed, none, lost := appends[0].(map[string]any), appends[1].(map[string]any), appends[2].(map[string]any), appends[3].(map[string]any)
	if durable["outcome"] != "appended" || durable["records"] != 1.0 || durable["log"] != "created" || durable["log_until"] != float64(appended.Until.At) || durable["why"] != nil {
		t.Errorf("the append is %v; want appended, one record, the log created, its end", durable)
	}
	if left, _ := none["temps_left"].([]any); none["log"] != "none" || len(left) != 1 || left[0].(map[string]any)["name"] != ".tmp-09-0" {
		t.Errorf("an append that placed no log is %v; want no log, and its create's temp left", none)
	}
	if reaped, _ := lost["temps_reaped"].([]any); lost["log"] != "existing" || len(reaped) != 1 || reaped[0] != ".tmp-09-1" {
		t.Errorf("an append whose create lost is %v; want the log existing, a racing create's, and the temp reaped", lost)
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

// An append says the sync of its log's name: synced, not_durable and why,
// whatever the frame did after, or none when it placed no log.
func TestAnAppendSaysTheSyncOfItsLogsName(t *testing.T) {
	block := storeBlockOf(t, StoreFacts{Appends: []store.Appending{
		store.Appended{Written: store.Written{Records: 1}, Log: store.LogLookedUp{}},
		store.NotAppended{Log: store.LogNotDurable{Log: store.LogCreated{Placed: files.Placed{Made: files.Created}}, Why: errors.New("EIO")}, Why: errors.New("the header did not read")},
		store.NotAppended{Log: store.NoLog{}, Why: errors.New("no space")},
	}})
	appends, _ := block["append"].([]any)
	if len(appends) != 3 {
		t.Fatalf("the block holds %v; want three appends", block["append"])
	}
	synced, unsynced, none := appends[0].(map[string]any), appends[1].(map[string]any), appends[2].(map[string]any)
	if synced["log"] != "looked_up" || synced["log_sync"] != "synced" || synced["log_sync_why"] != nil {
		t.Errorf("an append whose log's name synced is %v; want looked_up, synced", synced)
	}
	if unsynced["outcome"] != "not_appended" || unsynced["why"] != "the header did not read" || unsynced["log"] != "created" ||
		unsynced["log_sync"] != "not_durable" || unsynced["log_sync_why"] != "EIO" {
		t.Errorf("an append that landed nothing after its log's sync failed is %v; want not_appended, the log created, not_durable by EIO", unsynced)
	}
	if none["log"] != "none" || none["log_sync"] != "none" {
		t.Errorf("an append that placed no log is %v; want no log and no sync", none)
	}
}

func TestStoreFactsSurviveAPanickingBody(t *testing.T) {
	appended := store.Appended{Written: store.Written{Records: 2, Bytes: 40}, Log: store.LogLookedUp{}}
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

// The checkout's object format is on the place line, beside its paths.
func TestThePlaceSaysTheCheckoutsObjectFormat(t *testing.T) {
	place, _ := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "get"}, Started: t0, Outcome: cli.Done{}})["place"].(map[string]any)
	c, _ := place["checkout"].(map[string]any)
	if c["object_format"] != "sha256" || c["git_dir"] != "/w/.git" {
		t.Errorf("the place line is %v; want the checkout's paths and its format, sha256", place)
	}
}
