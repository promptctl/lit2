package address_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

var schema = must(config.Load(config.NeverCommitted{}, running)).Schema()

func comment(of events.ItemID) events.Draft {
	return events.CreateElement{Of: of, Payload: events.CommentAdd{Body: events.SealPlain("note")}}
}

func made2(e events.Event) events.NewElement { return e.Header().About().(events.NewElement) }

// TestEveryPathFormNamesItsNode: the root, a home, an item by its full id
// or its segments, a field, a slashed field, a member and a comment each
// parse to what they name, rendered canonically.
func TestEveryPathFormNamesItsNode(t *testing.T) {
	var s fold.State
	s, ev := mint(t, s, s, 0xa, top(2), [16]byte{0x10})
	feature := made(ev)
	s, ec := mint(t, s, s, 0xa, comment(feature.ID), [16]byte{0x30})
	note := made2(ec)
	two := must(events.ParseLevel(2))
	seg := feature.Segment().String()
	for _, c := range []struct {
		text, canon string
		place       field.Place
	}{
		{"/", "/", field.Root{}},
		{"/children", "/children", field.Root{}},
		{"unfiled", "unfiled", field.Home{Level: two}},
		{"unfiled/children", "unfiled/children", field.Home{Level: two}},
		{"lit-" + seg, "lit-" + seg, field.Node{Level: two}},
		{feature.ID.String() + "/title", "lit-" + seg + "/title", field.Node{Level: two}},
		{seg + "/plan/verdict", "lit-" + seg + "/plan/verdict", field.Node{Level: two}},
		{seg + "/labels/ready", "lit-" + seg + "/labels/ready", field.Node{Level: two}},
		{seg + "/comments", "lit-" + seg + "/comments", field.Node{Level: two}},
		{seg + "/comments/" + note.Segment().String(), "lit-" + seg + "/comments/" + note.Segment().String(), field.Comment{}},
		{seg + "/comments/" + note.ID.String(), "lit-" + seg + "/comments/" + note.Segment().String(), field.Comment{}},
		{seg + "/comments/" + note.Segment().String() + "/body", "lit-" + seg + "/comments/" + note.Segment().String() + "/body", field.Comment{}},
		{seg + "/comments/" + note.ID.String() + "/deleted", "lit-" + seg + "/comments/" + note.Segment().String() + "/deleted", field.Comment{}},
		{seg + "/ready", "lit-" + seg + "/ready", field.Node{Level: two}},
	} {
		a, r := address.Parse(s, schema, c.text)
		if r != nil {
			t.Errorf("%s: %v", c.text, r)
			continue
		}
		if a.String() != c.canon || a.Place != c.place {
			t.Errorf("%s = %s at %#v; want %s at %#v", c.text, a, a.Place, c.canon, c.place)
		}
	}
	if a, _ := address.Parse(s, schema, seg+"/comments/"+note.Segment().String()); a.Node != (address.Element{Of: feature.ID, ID: note.ID, Rendered: "lit-" + seg + "/comments/" + note.Segment().String()}) {
		t.Errorf("a comment's path names %#v; want the comment, a node", a.Node)
	}
	if a, _ := address.Parse(s, schema, seg+"/ready"); !isCheck(a) {
		t.Errorf("%s/ready = %#v; want the check row", seg, a.Target)
	}
}

func isCheck(a address.Address) bool {
	f, ok := a.Target.(address.Field)
	if !ok {
		return false
	}
	_, ok = f.Row.(config.CheckRow)
	return ok
}

// TestAPathThatNamesNothingSaysWhy: each refusal carries its own reason,
// and an unknown field lists the rows the place has, which are the rows
// help prints there.
func TestAPathThatNamesNothingSaysWhy(t *testing.T) {
	var s fold.State
	s, ev := mint(t, s, s, 0xa, top(2), [16]byte{0x10})
	seg := made(ev).Segment().String()
	s, ec := mint(t, s, s, 0xa, comment(made(ev).ID), [16]byte{0x30})
	note := seg + "/comments/" + made2(ec).Segment().String()
	for text, reason := range map[string]string{
		"":                      "syntax",
		seg + "/":               "syntax",
		seg + "/title/":         "syntax",
		seg + "/labels/":        "syntax",
		"Not An Id":             "syntax",
		"zzz":                   "not_found",
		seg + "/nope":           "unknown_field",
		seg + "/status":         "not_on_level",
		"/title":                "not_on_level",
		seg + "/title/x":        "names_no_element",
		seg + "/children/x":     "names_no_element",
		seg + "/comments/zzz":   "not_found",
		seg + "/labels/Not A":   "syntax",
		seg + "/ready/x":        "names_no_element",
		seg + "/labels/ready/x": "names_no_element",
		"unfiled/description":   "not_on_level",
		note + "/":              "syntax",
		note + "/nope":          "unknown_field",
		note + "/title":         "not_on_level",
		note + "/body/x":        "names_no_element",
		"/pr/verdict":           "not_on_level",
		note + "/pr/verdict":    "not_on_level",
		seg + "/status/x":       "not_on_level",
		seg + "//title":         "syntax",
		note + "//body":         "syntax",
	} {
		_, r := address.Parse(s, schema, text)
		if r == nil || r.Reason() != reason {
			t.Errorf("%q refused with %v; want %s", text, r, reason)
		}
	}
	_, r := address.Parse(s, schema, seg+"/nope")
	var unknown address.UnknownField
	if !errors.As(r, &unknown) {
		t.Fatalf("nope = %v", r)
	}
	var want []field.Path
	for _, row := range schema.Table().Rows(field.Node{Level: must(events.ParseLevel(2))}) {
		want = append(want, row.Path())
	}
	if !slices.Equal(unknown.Known, want) {
		t.Errorf("an unknown field lists %v; want the place's rows %v", unknown.Known, want)
	}
}

// TestAnAmbiguousCommentListsBoth: two writers offline can fix one segment
// for two comments of one item, as for two items, and it names neither.
func TestAnAmbiguousCommentListsBoth(t *testing.T) {
	var s fold.State
	s, ev := mint(t, s, s, 0xa, top(2), [16]byte{0x10})
	feature := made(ev)
	view := s
	s, ea := mint(t, s, view, 0xa, comment(feature.ID), [16]byte{0, 0, 1})
	s, eb := mint(t, s, view, 0xb, comment(feature.ID), [16]byte{0, 0, 2})
	a, b := made2(ea), made2(eb)
	if a.Segment() != b.Segment() {
		t.Fatalf("the comments fixed %s and %s; the test wants one", a.Segment(), b.Segment())
	}
	_, r := address.Parse(s, schema, feature.Segment().String()+"/comments/"+a.Segment().String())
	var amb address.Ambiguous
	if !errors.As(r, &amb) || !slices.Equal(amb.Candidates, []string{a.ID.String(), b.ID.String()}) {
		t.Errorf("the shared segment = %v; want both comments listed", r)
	}
}

// TestAnItemTheFoldCannotPlaceIsUnplaced: a fact about an id nothing
// created reads Orphaned, so it has no place and no fields.
func TestAnItemTheFoldCannotPlaceIsUnplaced(t *testing.T) {
	var s fold.State
	ghost := must(events.ParseItemID("g0000000000000000000000000"))
	forged := events.CreateItem{Payload: events.ItemCreate{Place: events.BelowParent{Parent: ghost}}}
	s, ec := mint(t, s, s, 0xa, forged, [16]byte{0x20})
	_, r := address.Parse(s, schema, made(ec).ID.String())
	if r == nil || r.Reason() != "unplaced" {
		t.Errorf("an orphan = %v; want unplaced", r)
	}
}

// TestADeclaredFieldIsAddressable: a field a workspace version declares is
// a path with no code change.
func TestADeclaredFieldIsAddressable(t *testing.T) {
	text := []byte("prefix = \"lit\"\nclaim_ttl = \"6h\"\n[levels.1]\nname = \"story\"\n[levels.1.fields.points]\nkind = \"enum\"\nvalues = [\"1\", \"2\", \"3\", \"5\", \"8\"]\n[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n[columns.backlog]\ninitial = true\nnext = [\"done\"]\n[columns.done]\nterminal = true\n")
	declared := must(config.Parse(text, "workspace.toml", running))
	var s fold.State
	s, ev := mint(t, s, s, 0xa, top(1), [16]byte{0x10})
	a, r := address.Parse(s, declared, made(ev).Segment().String()+"/points")
	if r != nil {
		t.Fatal(r)
	}
	row := a.Target.(address.Field).Row.(config.FieldRow)
	if _, err := row.Decl.Kind.Parse("5"); err != nil {
		t.Errorf("points takes 5: %v", err)
	}
	if _, err := row.Decl.Kind.Parse("13"); err == nil {
		t.Error("points takes 13")
	}
}
