package verbs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
)

// Got is a read: each path's result, and when none was refused, what it
// prints.
type Got struct {
	Operands []Result
	Out      string
}

// Refusal is the first path's refusal, and false when none was refused.
func (g Got) Refusal() (Refused, bool) { return firstRefused(g.Operands) }

// line is one value a read prints, under the path it is at.
type line struct {
	at    string
	value string
}

// Get reads the nodes at paths. One path naming one value prints it bare;
// a node prints `$ = <node>` and a line per value it holds, each under its
// path from `$`; several paths print each value under its path. A value a
// reader could not tell the end of prints quoted.
func Get(w World, paths []string) Got {
	var g Got
	var reads [][]line
	var whole []bool
	for _, p := range paths {
		a, r := address.Parse(w.State, w.Schema, p)
		if r != nil {
			g.Operands = append(g.Operands, Refused{Text: p, Reason: r.Reason(), Err: r, Detail: r.Error()})
			continue
		}
		ls, no := read(w, a)
		if no != nil {
			g.Operands = append(g.Operands, Refused{Text: p, Reason: no.reason, Err: no.err})
			continue
		}
		g.Operands = append(g.Operands, Accepted{Text: p, Canon: a.String(), Values: len(ls)})
		reads = append(reads, ls)
		_, isWhole := a.Target.(address.Whole)
		whole = append(whole, isWhole)
	}
	if _, refused := g.Refusal(); refused {
		return g
	}
	var b strings.Builder
	for i, ls := range reads {
		single := len(reads) == 1
		if single && !whole[i] && len(ls) == 1 {
			b.WriteString(ls[0].value + "\n")
			continue
		}
		canon := g.Operands[i].(Accepted).Canon
		if single && whole[i] {
			fmt.Fprintf(&b, "$ = %s\n", canon)
		}
		for _, l := range ls {
			at := l.at
			if single && whole[i] {
				at = "$" + strings.TrimPrefix(at, strings.TrimSuffix(canon, "/"))
			}
			fmt.Fprintf(&b, "%s\t%s\n", at, shown(l.value, false))
		}
	}
	g.Out = b.String()
	return g
}

// read is the lines the address holds, each at its canonical path: a
// node's fields, then its lists' elements. A member it does not hold reads
// as nothing, and one its kind refuses is refused, which is how a typo
// shows, as rm's is.
func read(w World, a address.Address) ([]line, *refusal) {
	base := strings.TrimSuffix(a.String(), "/")
	switch t := a.Target.(type) {
	case address.Whole:
		var fields, lists []line
		for _, r := range w.Schema.Table().Rows(a.Place) {
			ls, no := rowLines(w, a, base, r.Path(), r)
			if no != nil {
				return nil, no
			}
			if f, ok := r.(config.FieldRow); ok && f.Decl.Multi == field.Elements {
				lists = append(lists, ls...)
			} else {
				fields = append(fields, ls...)
			}
		}
		return append(fields, lists...), nil
	case address.Field:
		return rowLines(w, a, strings.TrimSuffix(base, "/"+string(t.Path)), t.Path, t.Row)
	case address.Element:
		switch k := t.Key.(type) {
		case address.Member:
			held, _ := Read(w, a.Node, t.Path, config.FieldRow{At: t.Path, Decl: t.Decl}) // a set reads no status
			if slices.Contains(held, k.Name.String()) {
				return []line{{at: base, value: k.Name.String()}}, nil
			}
			if _, err := t.Decl.Kind.Parse(k.Name.String()); err != nil {
				return nil, refuse("value", "%s: %v", t.Path, err)
			}
			return nil, nil
		case address.ElementKey:
			for _, e := range elements(w.item(a.Node.(address.Item).ID), events.MustName(string(t.Path))) {
				if e.key == k.Rendered {
					return []line{{at: base, value: fmt.Sprintf("[%s] %s", e.by, e.text)}}, nil
				}
			}
		}
	}
	return nil, nil
}

// rowLines is one row's values on a node, each at the row's path, and a
// list's elements each at its own key.
func rowLines(w World, a address.Address, node string, path field.Path, r config.Row) ([]line, *refusal) {
	at := node + "/" + string(path)
	if f, ok := r.(config.FieldRow); ok && f.Decl.Multi == field.Elements {
		var out []line
		for _, e := range elements(w.item(a.Node.(address.Item).ID), events.MustName(string(path))) {
			out = append(out, line{at: at + "/" + e.key, value: fmt.Sprintf("[%s] %s", e.by, e.text)})
		}
		return out, nil
	}
	values, err := Read(w, a.Node, path, r)
	if err != nil {
		return nil, refuse("configuration", "%v", err)
	}
	var out []line
	for _, v := range values {
		out = append(out, line{at: at, value: v})
	}
	return out, nil
}
