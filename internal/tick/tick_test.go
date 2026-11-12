package tick

import (
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

func TestAViewDerivesFromTheStateItIsOf(t *testing.T) {
	env, err := fold.NewEnv(fold.NewRegistry(fold.FirstFacts()...), events.NewSuites(events.Ed25519()), 1)
	if err != nil {
		t.Fatal(err)
	}
	s := fold.Genesis(env, nil).State
	v := Of(s, config.Schema{}, time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC))
	if !v.State().Equal(s) {
		t.Error("a view's state is not the state it is of")
	}
}
