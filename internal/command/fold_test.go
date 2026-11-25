package command

import (
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

func foldBlockOf(t *testing.T, f fold.FoldFacts) map[string]any {
	t.Helper()
	m := decode(t, Event{Invocation: asked(inv, "get"), Place: here, Store: StoreFacts{Folds: []fold.FoldFacts{f}}, Outcome: cli.Done{}})
	list, _ := m["fold"].([]any)
	if len(list) != 1 {
		t.Fatalf("the line's fold block is %v; want one fold", m["fold"])
	}
	return list[0].(map[string]any)
}

var nothing = func() fold.Env {
	env, err := fold.NewEnv(fold.NewRegistry(fold.FirstFacts()...), events.NewSuites(events.Ed25519()), 1)
	if err != nil {
		panic(err)
	}
	return env
}()

func TestAFoldOfNothingIsEveryCountZeroAndPresent(t *testing.T) {
	block := foldBlockOf(t, fold.Replay(nothing, fold.NoFounding{}, nil).Facts)
	for _, k := range []string{"records", "torn", "duplicates", "checked", "applied"} {
		if block[k] != 0.0 {
			t.Errorf("a fold of nothing has %s %v; want 0, present", k, block[k])
		}
	}
	zeros := map[string][]string{
		"decoded":   {"readable", "erased", "uncommitted", "malformed"},
		"verdicts":  {"valid", "invalid", "unknown_suite"},
		"counts":    {"items", "comments", "members", "prose_conflicts", "overruled", "mixed_forms", "collisions", "uncreated", "versions", "contending"},
		"carried":   {"bad_signature", "unknown_suite"},
		"unapplied": {"unread", "erased", "uncommitted", "misaddressed"},
	}
	for obj, keys := range zeros {
		m, ok := block[obj].(map[string]any)
		if !ok {
			t.Errorf("the fold block has no %s: %v", obj, block)
			continue
		}
		for _, k := range keys {
			if m[k] != 0.0 {
				t.Errorf("a fold of nothing has %s.%s %v; want 0, present", obj, k, m[k])
			}
		}
	}
	// unread is keyed by kind and reason, an open set: none read is an
	// empty object, present
	if unread, ok := block["decoded"].(map[string]any)["unread"].(map[string]any); !ok || len(unread) != 0 {
		t.Errorf("a fold of nothing has decoded.unread %v; want {}", block["decoded"])
	}
}

func TestAnEventThatNeverFoldedHasNoFoldBlock(t *testing.T) {
	if m := decode(t, Event{Invocation: asked(inv, "help"), Place: here, Outcome: cli.Done{}}); m["fold"] != nil {
		t.Errorf("an event that never reached step 3 carries %v", m["fold"])
	}
}
