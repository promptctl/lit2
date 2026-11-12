package tick

import (
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

func TestAViewDerivesFromTheStateItIsOf(t *testing.T) {
	env := fold.Env{Registry: fold.NewRegistry(fold.FirstFacts()...), Suites: events.NewSuites(events.Ed25519()), Workers: 1}
	s := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, nil).State
	v := Of(s, config.Schema{}, time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC))
	if !v.State().Equal(s) {
		t.Error("a view's state is not the state it is of")
	}
}
