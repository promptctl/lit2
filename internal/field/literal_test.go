package field

import (
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
	for _, text := range []string{
		`[a,,b]`, `[,]`, `[a`, `{title a}`, `{title: a`, `"open`, `"a" b`, `[a] b`,
		`{level: 1, level: 2}`, // a key is given once
	} {
		if got, err := ParseLiteral(text); err == nil {
			t.Errorf("ParseLiteral(%q) = %#v; want it refused", text, got)
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
	for _, c := range []struct {
		k  Kind
		in string
	}{
		{Column{}, "Not A Name"}, {Level{}, "6"}, {Level{}, "x"}, {ItemRef{}, "a b"}, {ItemRef{}, ""},
	} {
		if _, err := c.k.Parse(c.in); err == nil {
			t.Errorf("%s.Parse(%q) reads", c.k.Name(), c.in)
		}
	}
}

func TestAFieldExistsWhereItsPlacesSay(t *testing.T) {
	two, _ := events.ParseLevel(2)
	three, _ := events.ParseLevel(3)
	one, _ := events.ParseLevel(1)
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
	for path, want := range map[Path]bool{"title": true, "labels": true, "deleted": true, "level": false, "parent": false, "pr/verdict": false} {
		if got := Writable(b[path]) == nil; got != want {
			t.Errorf("%s writable %v; want %v", path, got, want)
		}
	}
	for path, want := range map[Path]bool{"title": true, "labels": true, "status": true, "comments": false, "children": false} {
		if got := Keyable(b[path]) == nil; got != want {
			t.Errorf("%s keyable %v; want %v", path, got, want)
		}
	}
}
