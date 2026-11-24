package field

import (
	"errors"
	"reflect"
	"testing"

	"github.com/promptctl/lit2/internal/events"
)

func TestALiteralReadsAsTheGrammarSays(t *testing.T) {
	s := func(t string) Literal { return Scalar{Text: t} }
	for text, want := range map[string]Literal{
		"done":                         s("done"),
		"a note with spaces":           s("a note with spaces"),
		"":                             s(""),
		`"say ""hi"""`:                 s(`say "hi"`),
		`[a, b]`:                       List{Items: []Literal{s("a"), s("b")}},
		`[a b]`:                        List{Items: []Literal{s("a"), s("b")}},
		`[a, b,]`:                      List{Items: []Literal{s("a"), s("b")}},
		`[]`:                           List{},
		`{title: a}`:                   Object{Pairs: []Pair{{"title", s("a")}}},
		`{title:a,}`:                   Object{Pairs: []Pair{{"title", s("a")}}},
		`{title : a}`:                  Object{Pairs: []Pair{{"title", s("a")}}},
		`{title:"x y" labels:[ready]}`: Object{Pairs: []Pair{{"title", s("x y")}, {"labels", List{Items: []Literal{s("ready")}}}}},
		`[{title: a}, {title: b}]`: List{Items: []Literal{
			Object{Pairs: []Pair{{"title", s("a")}}}, Object{Pairs: []Pair{{"title", s("b")}}},
		}},
	} {
		got, err := ParseLiteral(text)
		if err != nil {
			t.Errorf("ParseLiteral(%q): %v", text, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParseLiteral(%q) = %#v; want %#v", text, got, want)
		}
	}
}

func TestALiteralTheGrammarDoesNotSayIsRefused(t *testing.T) {
	for _, c := range []struct {
		text, control string
		want          error
	}{
		{`[a,,b]`, `[a,b]`, errNotAValue},
		{`[,]`, `[]`, errNotAValue},
		{`[a`, `[a]`, errNotAValue},
		{`{title a}`, `{title: a}`, errNotAValue},
		{`{title: a`, `{title: a}`, errNotAValue},
		{`"open`, `"open"`, errNotAValue},
		{`"a" b`, `"a b"`, errNotAValue},
		{`[a] b`, `[a b]`, errNotAValue},
		{`{level: 1, level: 2}`, `{level: 1, title: 2}`, errKeyTwice}, // a key is given once
	} {
		if _, err := ParseLiteral(c.control); err != nil {
			t.Errorf("control ParseLiteral(%q): %v", c.control, err)
		}
		if got, err := ParseLiteral(c.text); !errors.Is(err, c.want) {
			t.Errorf("ParseLiteral(%q) = %#v, %v; want %v", c.text, got, err, c.want)
		}
	}
}

func TestTheEngineKindsReadTheirOwnValues(t *testing.T) {
	for _, c := range []struct {
		k        Kind
		in, want string
	}{
		{Column{}, "plan", "plan"},
		{Level{}, "3", "3"},
		{Level{}, "03", "3"},
		{ItemRef{}, "lit-2cw", "lit-2cw"},
	} {
		if got, err := c.k.Parse(c.in); err != nil || got != c.want {
			t.Errorf("%s.Parse(%q) = %q, %v; want %q", c.k.Name(), c.in, got, err, c.want)
		}
	}
	// the controls are the reads above; each refusal differs from one in
	// what its kind does not read
	for _, c := range []struct {
		k    Kind
		in   string
		want error
	}{
		{Column{}, "Not A Name", events.ErrNotAName}, {Level{}, "6", events.ErrNotALevel}, {Level{}, "x", events.ErrNotALevel},
		{ItemRef{}, "a b", errNotAnItem}, {ItemRef{}, "", errNotAnItem},
	} {
		if _, err := c.k.Parse(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s.Parse(%q) = %v; want %v", c.k.Name(), c.in, err, c.want)
		}
	}
}

func TestAFieldExistsWhereItsPlacesSay(t *testing.T) {
	level := func(n int) events.Level {
		t.Helper()
		l, err := events.ParseLevel(n)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	one, two, three := level(1), level(2), level(3)
	b := Builtins(Enum{})
	for _, c := range []struct {
		path Path
		at   Place
		want bool
	}{
		{"title", Leaf{}, true}, {"title", Node{Level: two}, true}, {"title", Home{Level: two}, false}, {"title", Root{}, false},
		{"status", Leaf{}, true}, {"status", Node{Level: two}, false},
		{"children", Root{}, true}, {"children", Home{Level: two}, true}, {"children", Node{Level: two}, true}, {"children", Leaf{}, false},
	} {
		if got := b[c.path].At.Holds(c.at); got != c.want {
			t.Errorf("%s at %#v holds %v; want %v", c.path, c.at, got, c.want)
		}
	}
	if !(OnLevel{Level: one}).Holds(Leaf{}) || (OnLevel{Level: one}).Holds(Node{Level: two}) {
		t.Error("a field declared on level 1 is not at the leaves alone")
	}
	if !(OnLevel{Level: two}).Holds(Node{Level: two}) || (OnLevel{Level: two}).Holds(Node{Level: three}) || (OnLevel{Level: two}).Holds(Leaf{}) {
		t.Error("a field declared on level 2 is not at level 2's items alone")
	}
}

func TestOnlySettableFieldsAreWrittenAndOnlyOneOrASetIsKeyed(t *testing.T) {
	b := Builtins(Enum{})
	for path, want := range map[Path]Unwritable{"title": nil, "labels": nil, "deleted": nil, "level": DerivedField{}, "parent": DerivedField{}, "pr/verdict": DerivedField{}, "status/reason": TransitionWritten{}} {
		if got := Writable(b[path]); got != want {
			t.Errorf("%s writable: %v; want %v", path, got, want)
		}
	}
	for path, want := range map[Path]error{"title": nil, "labels": nil, "status": nil, "comments": NotKeyable{Multi: Elements}, "children": NotKeyable{Multi: Children}} {
		if got := Keyable(b[path]); got != want {
			t.Errorf("%s keyable: %v; want %v", path, got, want)
		}
	}
}
