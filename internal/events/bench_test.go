package events

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/store"
)

// corpus is scenario 13's store: 10,000 items and 200,000 events from ten
// writers, the items' creates among acts on items and their elements, each
// with prose, as one log's records.
var corpus = sync.OnceValue(func() []store.Record {
	const items, events, writers = 10_000, 200_000, 10
	r, salt := rand.New(rand.NewPCG(13, 13)), salts(13)
	keys := make([]Key, writers)
	for w := range keys {
		keys[w] = keyOf(byte(w))
	}
	var f Frontier
	var ids []ItemID
	var b NamesBuilder
	top := Children{Parent: Top{Prefix: prefix}}
	rs := make([]store.Record, 0, events)
	for len(rs) < events {
		w := r.IntN(writers)
		var d Draft
		create := len(ids) == 0 || r.IntN(events-len(rs)) < items-len(ids)
		switch {
		case create:
			d = Create{In: top, Payload: made{Title: text(words(r, 3+r.IntN(8)))}}
		default:
			item := ids[r.IntN(len(ids))]
			about := About(Item{ID: item})
			if r.IntN(4) == 0 {
				about = Element{Of: item, Name: Segment{s: "abc"}}
			}
			var tags []string
			for range r.IntN(3) {
				tags = append(tags, english[r.IntN(len(english))])
			}
			d = Act{About: about, Payload: note{Text: text(words(r, r.IntN(60))), Tags: tags}}
		}
		m, err := Mint(Context{Writer: writer(byte(w)), Agent: Unlabeled{}, ConfigRef: config, Policy: 1, Frontier: f, Names: b.Names(), Now: t0},
			NewDrafts(d), keys[w].Signer(), salt)
		if err != nil {
			panic(err)
		}
		e := m.Events[0]
		f = f.Observe(e.Header())
		if create {
			it := e.Header().About().(Item)
			ids = append(ids, it.ID)
			b.Add(top, it.ID.Segments()[0])
		}
		rs = append(rs, m.Batch.Records()...)
	}
	return rs
})

// BenchmarkCorpus measures one replay's codec cost over scenario 13's
// corpus, per event: Decode is the whole of reading a record (its parse,
// its id's hash and its payload's commitment check, the payload's decode);
// Hash is the two hashes alone, the id and the payload's; Verify is the
// signature check Decode leaves to the fold. bytes/event is the mean stored
// record.
func BenchmarkCorpus(b *testing.B) {
	rs := corpus()
	var size int
	for _, r := range rs {
		size += len(r)
	}
	es := make([]Event, len(rs))
	parts := make([]record, len(rs))
	for i, r := range rs {
		es[i] = decoded(b, here, r)
		rec, err := readRecord(r)
		if err != nil {
			b.Fatal(err)
		}
		parts[i] = rec
	}
	report := func(b *testing.B) {
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(rs)), "ns/event")
		b.ReportMetric(float64(size)/float64(len(rs)), "bytes/event")
	}
	b.Run("Decode", func(b *testing.B) {
		for b.Loop() {
			for _, r := range rs {
				if _, ok := Decode(here, r).(Event); !ok {
					b.Fatal("a corpus record is malformed")
				}
			}
		}
		report(b)
	})
	b.Run("Hash", func(b *testing.B) {
		bodies := make([]Readable, len(es))
		payloads := make([][]byte, len(es))
		for i, e := range es {
			bodies[i] = e.Body().(Readable)
			payloads[i] = payloadWire(bodies[i].Payload)
		}
		for b.Loop() {
			for i, p := range parts {
				if eventID(p.header, p.sigRaw) != es[i].ID() || payloadHash(bodies[i].Salt, payloads[i]) != es[i].Header().PayloadHash() {
					b.Fatal("a corpus record hashes apart from its decode")
				}
			}
		}
		report(b)
	})
	b.Run("Verify", func(b *testing.B) {
		for b.Loop() {
			for _, e := range es {
				if suites.Check(e.Header().Attribution().Principal, e.SignedBytes(), e.Signature()) != Valid {
					b.Fatal("a corpus signature does not verify")
				}
			}
		}
		report(b)
	})
}
