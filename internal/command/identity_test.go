package command

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

func identityOf(t *testing.T, id Identity) map[string]any {
	t.Helper()
	m := decode(t, Event{Invocation: Invocation{Place: InCheckout{Checkout: checkout, Identity: id}, Agent: events.Unlabeled{}},
		Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}})
	place, _ := m["place"].(map[string]any)
	in, _ := place["checkout"].(map[string]any)
	got, _ := in["identity"].(map[string]any)
	return got
}

func TestTheLineNamesTheWriterAndItsPrincipalNeverItsKey(t *testing.T) {
	got := identityOf(t, Minted{Checkout: store.CheckoutID{9}, Key: key})
	if got["checkout"] != (store.CheckoutID{9}).String() || got["principal"] != key.Principal().String() {
		t.Errorf("a minted identity is %v; want the checkout id and the principal's did:key", got)
	}
	b, err := json.Marshal(decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}}))
	if err != nil {
		t.Fatal(err)
	}
	if seed := strings.TrimSuffix(strings.TrimPrefix(string(key.FileText()), "ed25519 "), "\n"); strings.Contains(string(b), seed) {
		t.Errorf("the line %s holds the key's seed", b)
	}
}

func TestTheLineNamesEachMissingOrUnreadableFilesPath(t *testing.T) {
	id, principal := PathOf(IDFile{}, checkout), PathOf(PrincipalFile{}, checkout)
	if id != "/w/.git/lit/checkout/id" || principal != "/w/.git/lit/principal" {
		t.Errorf("the identity files are at %s and %s; want the private dir's id and the common dir's principal (D11)", id, principal)
	}
	for _, tc := range []struct {
		missing Missing
		want    []any
	}{
		{Both{}, []any{id, principal}},
		{Only{File: IDFile{}}, []any{id}},
		{Only{File: PrincipalFile{}}, []any{principal}},
	} {
		got, _ := identityOf(t, Unminted{Missing: tc.missing})["unminted"].([]any)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%#v is unminted at %v; want %v", tc.missing, got, tc.want)
		}
	}
	unreadable, _ := identityOf(t, Unreadable{File: PrincipalFile{}, Err: errors.New("the key file holds 3 bytes")})["unreadable"].(map[string]any)
	if unreadable["path"] != principal || unreadable["error"] != "the key file holds 3 bytes" {
		t.Errorf("an unreadable key file is %v; want its path and why", unreadable)
	}
}
