package verbs

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events"
)

// Echo is what a write prints: one line per change, rendered after
// minting from the state the minted events folded onto, so a new item
// shows by the path it now renders as, each arrival followed by its
// judgment's warnings and counts. minted is nil under --dry-run, and
// then a new item shows as (new N), the Nth the command creates, and a new
// comment as (new), and the echo ends saying nothing was
// written. It is the one renderer of a change `[LAW:single-enforcer]`.
func Echo(w World, c Compiled, minted []events.Event) string {
	about := drafted(c.Drafts, minted)
	filed := map[int]int{}
	for _, ch := range c.Changes {
		if f, ok := ch.(Filed); ok {
			filed[f.N] = f.Draft
		}
	}
	who := func(x Who) string {
		if n, created := x.(Created); created && about != nil {
			return w.render(about[filed[n.N]].(events.NewItem).ID)
		}
		return unwritten(x)
	}
	var b strings.Builder
	for _, ch := range c.Changes {
		switch ch := ch.(type) {
		case Filed:
			id := who(Created{N: ch.N})
			fmt.Fprintf(&b, "+ %s\n⇒ %s/parent = %s\n", id, id, ch.Parent)
			b.WriteString(ch.Judged.echo(who))
		case Wrote:
			after := shown(ch.After, ch.Prose)
			if _, created := ch.Who.(Created); created {
				fmt.Fprintf(&b, "⇒ %s/%s = %s%s\n", who(ch.Who), ch.Path, after, tagged(ch.Source))
				continue
			}
			before := "(unset)"
			if len(ch.Before) > 0 {
				shownAll := make([]string, len(ch.Before))
				for i, v := range ch.Before {
					shownAll[i] = shown(v, ch.Prose)
				}
				before = strings.Join(shownAll, " | ")
			}
			fmt.Fprintf(&b, "%s/%s %s → %s\n", who(ch.Who), ch.Path, before, after)
		case Moved:
			if _, created := ch.Who.(Created); created {
				fmt.Fprintf(&b, "⇒ %s/%s = %s%s\n", who(ch.Who), ch.Path, ch.To, tagged(ch.Source))
			} else {
				fmt.Fprintf(&b, "%s/%s %s → %s\n", who(ch.Who), ch.Path, ch.From, ch.To)
			}
			b.WriteString(ch.Judged.echo(who))
		case Added:
			if _, created := ch.Who.(Created); created {
				fmt.Fprintf(&b, "⇒ %s/%s/%s%s\n", who(ch.Who), ch.Path, ch.Member, tagged(ch.Source))
				continue
			}
			fmt.Fprintf(&b, "+ %s/%s/%s\n", who(ch.Who), ch.Path, ch.Member)
		case Removed:
			fmt.Fprintf(&b, "- %s/%s/%s\n", who(ch.Who), ch.Path, ch.Member)
		case Commented:
			key := "(new)"
			if about != nil {
				e := about[ch.Draft].(events.NewElement)
				key = e.Segment().String()
			}
			fmt.Fprintf(&b, "+ %s/%s/%s\n", who(ch.Who), ch.Path, key)
		}
	}
	if minted == nil {
		b.WriteString("(dry run: nothing written)\n")
	}
	return b.String()
}

// unwritten is x as a line names it before anything is stored: an
// existing item by its path, a new one as (new N), the Nth the command
// creates.
func unwritten(x Who) string {
	switch x := x.(type) {
	case Existing:
		return x.Rendered
	case Created:
		return fmt.Sprintf("(new %d)", x.N)
	}
	panic(fmt.Sprintf("verbs: %T is not a Who", x))
}

// drafted is what each draft's first minted event is about, by draft: a
// create's new node. A create's facts are minted after it, so its events
// are one plus its Then. Nil events are a dry run, which minted nothing.
func drafted(ds []events.Draft, minted []events.Event) []events.About {
	if minted == nil {
		return nil
	}
	out := make([]events.About, len(ds))
	at := 0
	for i, d := range ds {
		out[i] = minted[at].Header().About()
		at++
		if c, ok := d.(events.CreateItem); ok {
			at += len(c.Then)
		}
	}
	return out
}

// tagged marks a value a template gave.
func tagged(s Source) string {
	if _, ok := s.(FromTemplate); ok {
		return " (template)"
	}
	return ""
}

// shown is a value as a line prints it: prose always quoted, so its edges
// show, and any other value quoted only when a reader could not tell
// where it ends.
func shown(v string, prose bool) string {
	if prose || needsQuotes(v) {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// needsQuotes is whether v holds a newline, a tab or a quote, or a space at
// an edge, any of which a bare value would lose.
func needsQuotes(v string) bool {
	return v == "" || strings.ContainsAny(v, "\n\t\"") || strings.TrimSpace(v) != v
}
