package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"golang.org/x/mod/semver"
)

// Parse reads one version's bytes into one Schema, or refuses with every fault
// in it. source names the version in the refusal, and running is the reader
// the gate is judged against. It is pure: no file, no clock, no git
// `[LAW:effects-at-boundaries]`.
func Parse(text []byte, source string, running ReaderVersion) (Schema, error) {
	p := &parser{}
	var raw map[string]any
	if _, err := toml.Decode(string(text), &raw); err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			p.fail("", ReasonSyntax, "line %d, column %d: %s", pe.Position.Line, pe.Position.Col, pe.Message)
		} else {
			p.fail("", ReasonSyntax, "%v", err)
		}
		return Schema{}, p.refused(source)
	}
	// The gate is read before anything else: a reader it refuses does not know
	// the rest of the version's rules, so every other fault it found would be
	// its own ignorance, not the version's.
	gate := p.gate(raw)
	if g, ok := gate.(AtLeast); ok && !admits(g, running) {
		p.fail("min-reader-version", ReasonReaderTooOld, "this version needs lit2 %s or later, and this binary is %s: upgrade lit2 to read it", g.V, running)
		return Schema{}, p.refused(source)
	}
	s := p.schema(raw, gate)
	if len(p.refusals) > 0 {
		return Schema{}, p.refused(source)
	}
	return s, nil
}

// parser collects every refusal in one version. A parse never stops at the
// first fault, so a person sees them all at once. The table is built here,
// fields first, so a template and a test read the one table the schema
// keeps, and checks last, since a check's tests read the fields.
type parser struct {
	refusals   []Refusal
	table      Table
	checkNames map[string]bool
	checks     *expander
}

func (p *parser) fail(key KeyPath, r Reason, format string, args ...any) {
	p.refusals = append(p.refusals, Refusal{Key: key, Reason: r, Message: fmt.Sprintf(format, args...)})
}

func (p *parser) refused(source string) error {
	return &Refused{Source: source, Refusals: p.refusals}
}

// schema is the whole file. Each section is read in the order a later one
// needs: labels before the fields that carry them, fields before the checks
// and templates that name them, checks before the columns that include them.
func (p *parser) schema(raw map[string]any, gate ReaderGate) Schema {
	p.unknown(raw, "", "prefix", "min-reader-version", "claim_ttl", "labels", "levels", "links", "checks", "columns", "nodes")
	s := Schema{gate: gate}

	if v, ok := p.need(raw, "", "prefix", ReasonMissingKey, "the workspace names its prefix"); ok {
		if pre, err := events.ParsePrefix(v); err != nil {
			p.fail("prefix", ReasonBadPrefix, "%v", err)
		} else {
			s.prefix = pre
		}
	}
	s.labels = p.labels(raw)
	builtins := field.Builtins(s.labels)
	for _, path := range sortedKeys(builtins) {
		p.table.rows = append(p.table.rows, FieldRow{At: path, Decl: builtins[path]})
	}
	levels, nums := p.readLevels(raw, builtins)
	s.levels = levels
	p.table.levels = s.levels

	s.checks = p.readChecks(raw)
	for _, name := range sortedKeys(s.checks) {
		p.table.rows = append(p.table.rows, CheckRow{Check: s.checks[name]})
	}
	p.templates(raw, s.levels, nums)
	s.table = p.table
	s.links = p.links(raw, len(s.levels))

	ttl, ok := p.duration(raw, "", "claim_ttl")
	if !ok && !p.has(raw, "claim_ttl") {
		p.fail("claim_ttl", ReasonMissingKey, "the workspace sets its claim_ttl, how long a claim holds in silence")
	}

	rootCols := p.columns(raw, "")
	rootGraph, refusals := buildGraph(rootCols, func(name string) KeyPath {
		return "columns." + KeyPath(name)
	})
	p.refusals = append(p.refusals, refusals...)
	s.root = RootFrame{Columns: rootGraph, Declared: rootCols, ClaimTTL: ttl}

	// The root is a graph alone, since an item with no section on its
	// ancestry is judged by it alone. A section's column may reach a column
	// an ancestor's section declares, and which sections stack is known only
	// from the items, so here a section's next targets are checked only against
	// every column declared anywhere; each stacked graph is checked at apply.
	s.sections = p.sections(raw, s.levels)
	declared := map[string]bool{}
	for name := range rootCols {
		declared[name] = true
	}
	for _, sec := range s.sections {
		for name := range sec.Columns {
			declared[name] = true
		}
	}
	for _, node := range sortedNodeKeys(s.sections) {
		cols := s.sections[node].Columns
		for _, name := range sortedKeys(cols) {
			for _, next := range cols[name].Next() {
				if !declared[next] {
					p.fail(keyed(keyed(keyed("nodes", nodeSegment(node)), "columns."+name), "next"), ReasonNextUndeclared, "next names %s, which no column declares", next)
				}
			}
		}
	}
	return s
}

// has reports whether a key is present, whatever its value.
func (p *parser) has(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// gate reads min-reader-version: a semver, with or without its v. An absent
// one is no gate.
func (p *parser) gate(raw map[string]any) ReaderGate {
	v, ok := p.text(raw, "", "min-reader-version")
	if !ok {
		return NoGate{}
	}
	canon := v
	if !strings.HasPrefix(canon, "v") {
		canon = "v" + canon
	}
	if semver.Canonical(canon) != canon {
		p.fail("min-reader-version", ReasonBadVersion, "%q is not a version: MAJOR.MINOR.PATCH", v)
		return NoGate{}
	}
	return AtLeast{V: canon}
}

// labels reads the declared label set, the value set of the built-in labels.
func (p *parser) labels(raw map[string]any) field.Enum {
	list, ok := p.list(raw, "", "labels")
	if !ok {
		return field.Enum{}
	}
	var names []string
	seen := map[string]bool{}
	for i, item := range list {
		at := indexed("labels", i)
		s, ok := item.(string)
		if !ok {
			p.fail(at, ReasonWrongType, "a label is a string")
			continue
		}
		if _, err := events.ParseName(s); err != nil {
			p.fail(at, ReasonBadLabel, "%v", err)
			continue
		}
		if seen[s] {
			p.fail(at, ReasonDuplicateLabel, "label %s is declared twice", s)
			continue
		}
		seen[s] = true
		names = append(names, s)
	}
	if len(names) == 0 {
		return field.Enum{}
	}
	e, err := field.NewEnum(names)
	if err != nil {
		p.fail("labels", ReasonBadLabel, "%v", err)
		return field.Enum{}
	}
	return e
}

// readLevels reads the level tables, each with the fields it declares. It
// returns the levels by number, leaf first, and each one's number, which
// is its index plus one only when no level is missing.
func (p *parser) readLevels(raw map[string]any, builtins map[field.Path]field.FieldDecl) ([]Level, []int) {
	tbl, ok := p.tableAt(raw, "", "levels")
	if !ok || len(tbl) == 0 {
		p.fail("levels", ReasonNoLevels, "the workspace declares no levels: level 1 is the leaf")
		return nil, nil
	}
	var nums []int
	for key := range tbl {
		n, err := strconv.Atoi(key)
		if err != nil || n < 1 || n > 5 || strconv.Itoa(n) != key {
			p.fail(keyed("levels", key), ReasonLevelOutOfRange, "level %s is out of range: levels are 1 to 5", key)
			continue
		}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for i, n := range nums {
		if n != i+1 {
			p.fail("levels", ReasonLevelsNotContiguous, "levels are numbered 1 to %d with none missing: level %d is absent", len(nums), i+1)
			break
		}
	}

	levels := make([]Level, len(nums))
	for i, n := range nums {
		at := keyed("levels", strconv.Itoa(n))
		m, isTable := tbl[strconv.Itoa(n)].(map[string]any)
		if !isTable {
			p.fail(at, ReasonWrongType, "a level is a table")
			continue
		}
		lv := Level{Template: map[field.Path][]string{}}
		p.unknown(m, at, "name", "home", "fields", "template")
		if name, ok := p.need(m, at, "name", ReasonMissingKey, "a level has a name"); ok {
			lv.Name = name
			if name == "" {
				p.fail(keyed(at, "name"), ReasonEmptyLevelName, "a level's name is not empty")
			}
			for j := range levels[:i] {
				if levels[j].Name == name {
					p.fail(keyed(at, "name"), ReasonDuplicateLevelName, "level name %s is declared twice", name)
				}
			}
		}
		hasHome := p.has(m, "home")
		switch {
		case n == 1 && hasHome:
			p.fail(keyed(at, "home"), ReasonHomeOnLeaf, "level 1 is the leaf and holds no home")
		case n > 1 && !hasHome:
			p.fail(at, ReasonMissingHome, "level %d names its home: home = \"unfiled\" or another name", n)
		case hasHome:
			if home, ok := p.text(m, at, "home"); ok {
				if !isName(home) {
					p.fail(keyed(at, "home"), ReasonBadHome, "%q is not a home name: a lowercase word", home)
				}
				lv.Home = home
			}
		}
		if fieldsTbl, ok := p.tableAt(m, at, "fields"); ok {
			level, _ := events.ParseLevel(n)
			for _, fname := range sortedKeys(fieldsTbl) {
				fat := keyed(keyed(at, "fields"), fname)
				decl, ok := p.fieldDecl(fat, fname, fieldsTbl[fname], field.OnLevel{Level: level})
				if !ok {
					continue
				}
				path := field.Path(fname)
				// a comment's built-ins are no item's, so a level may take their names
				if b, builtin := builtins[path]; builtin && b.At.OfItems() {
					p.fail(fat, ReasonBuiltinRedeclared, "%s is a built-in field of items: a level cannot declare it", fname)
					continue
				}
				p.table.rows = append(p.table.rows, FieldRow{At: path, Decl: decl})
			}
		}
		levels[i] = lv
	}
	p.checkHomes(levels, nums)
	return levels, nums
}

// checkHomes refuses a home that repeats another home or a level's name, which
// would make `[nodes.<home>]` name two things. nums[i] is levels[i]'s number.
func (p *parser) checkHomes(levels []Level, nums []int) {
	homes := map[string]int{}
	names := map[string]bool{}
	for _, lv := range levels {
		names[lv.Name] = true
	}
	for i, lv := range levels {
		if lv.Home == "" {
			continue
		}
		at := keyed(keyed("levels", strconv.Itoa(nums[i])), "home")
		if names[lv.Home] {
			p.fail(at, ReasonHomeCollides, "home %s is also a level's name", lv.Home)
		}
		if j, dup := homes[lv.Home]; dup {
			p.fail(at, ReasonDuplicateHome, "home %s is level %d's and level %d's", lv.Home, j, nums[i])
			continue
		}
		homes[lv.Home] = nums[i]
	}
}

// fieldDecl reads one declared field: its kind and the parameters that kind
// takes, and nothing else.
func (p *parser) fieldDecl(at KeyPath, name string, v any, on field.OnLevel) (field.FieldDecl, bool) {
	if !isName(name) {
		p.fail(at, ReasonBadFieldName, "%q is not a field name: a lowercase word", name)
		return field.FieldDecl{}, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		p.fail(at, ReasonWrongType, "a field is a table")
		return field.FieldDecl{}, false
	}
	p.unknown(m, at, "kind", "values", "many", "pattern")
	kindWord, ok := p.need(m, at, "kind", ReasonMissingKey, "a field declares its kind")
	if !ok {
		return field.FieldDecl{}, false
	}
	hasValues, hasMany, hasPattern := p.has(m, "values"), p.has(m, "many"), p.has(m, "pattern")
	var kind field.Kind
	switch kindWord {
	case "enum":
		if !hasValues {
			p.fail(keyed(at, "values"), ReasonKindParameter, "an enum declares its values")
			return field.FieldDecl{}, false
		}
		values, ok := p.strings(m, at, "values")
		if !ok {
			return field.FieldDecl{}, false
		}
		e, err := field.NewEnum(values)
		if err != nil {
			p.fail(keyed(at, "values"), ReasonKindParameter, "%v", err)
			return field.FieldDecl{}, false
		}
		kind = e
	case "pattern":
		if !hasPattern {
			p.fail(keyed(at, "pattern"), ReasonKindParameter, "a pattern declares its pattern, a regular expression")
			return field.FieldDecl{}, false
		}
		re, _ := p.text(m, at, "pattern")
		pat, err := field.NewPattern(re)
		if err != nil {
			p.fail(keyed(at, "pattern"), ReasonKindParameter, "%v", err)
			return field.FieldDecl{}, false
		}
		kind = pat
	default:
		k, ok := field.KindNamed(kindWord)
		if !ok {
			p.fail(keyed(at, "kind"), ReasonUnknownKind, "%q is not a kind: text, number, date, enum or pattern", kindWord)
			return field.FieldDecl{}, false
		}
		kind = k
	}
	if hasValues && kindWord != "enum" {
		p.fail(keyed(at, "values"), ReasonKindParameter, "values belong to an enum, not a %s", kindWord)
	}
	if hasPattern && kindWord != "pattern" {
		p.fail(keyed(at, "pattern"), ReasonKindParameter, "pattern belongs to a pattern, not a %s", kindWord)
	}
	multi := field.One
	if hasMany {
		if many, _ := p.flag(m, at, "many"); many {
			if kindWord != "enum" {
				p.fail(keyed(at, "many"), ReasonKindParameter, "only an enum holds many values, not a %s", kindWord)
				return field.FieldDecl{}, false
			}
			// A set's members are names, so a value that is none could
			// never be added.
			for _, v := range kind.(field.Enum).Values() {
				if _, err := events.ParseName(v); err != nil {
					p.fail(keyed(at, "values"), ReasonKindParameter, "a many enum's values are its members, each a name: %v", err)
					return field.FieldDecl{}, false
				}
			}
			multi = field.Set
		}
	}
	return field.FieldDecl{Kind: kind, Multi: multi, Origin: field.Declared, Writability: field.Settable, At: on}, true
}

// templates reads each level's template, once every field and check is a
// row: a key is read by the table at the level's items, as a key of a
// child object is, so the two take the same fields. nums[i] is levels[i]'s
// number.
func (p *parser) templates(raw map[string]any, levels []Level, nums []int) {
	tbl, _ := raw["levels"].(map[string]any)
	for i, lv := range levels {
		n := strconv.Itoa(nums[i])
		m, _ := tbl[n].(map[string]any)
		at := keyed("levels", n)
		tmpl, ok := p.tableAt(m, at, "template")
		if !ok || lv.Template == nil {
			continue
		}
		l, _ := events.ParseLevel(nums[i])
		for _, key := range sortedKeys(tmpl) {
			path := field.Path(key)
			if v, ok := p.templateValue(keyed(keyed(at, "template"), key), field.ItemPlace(l), path, tmpl[key]); ok {
				lv.Template[path] = v
			}
		}
	}
}

// templateValue reads one template default: a field the table has at
// place, a person writes, and holds one value or a set, and a value that
// parses under its kind, a set's members each once.
func (p *parser) templateValue(at KeyPath, place field.Place, path field.Path, v any) ([]string, bool) {
	r, err := p.table.Find(place, path)
	if err != nil {
		p.fail(at, ReasonTemplateFieldUndeclared, "%v", err)
		return nil, false
	}
	row, isField := r.(FieldRow)
	if !isField {
		p.fail(at, ReasonTemplateFieldDerived, "%s is a check: the engine computes it, and a template sets only what a person writes", path)
		return nil, false
	}
	if err := field.Writable(row.Decl); err != nil {
		p.fail(at, ReasonTemplateFieldDerived, "%s: %v; a template sets only what a person writes", path, err)
		return nil, false
	}
	if err := field.Keyable(row.Decl); err != nil {
		p.fail(at, ReasonTemplateFieldNotKeyable, "%s: %v", path, err)
		return nil, false
	}
	var raw []string
	switch v := v.(type) {
	case string:
		raw = []string{v}
	case []any:
		if row.Decl.Multi == field.One {
			p.fail(at, ReasonWrongType, "%s holds one value: a string", path)
			return nil, false
		}
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				p.fail(at, ReasonWrongType, "a value of %s is a string", path)
				return nil, false
			}
			raw = append(raw, s)
		}
	default:
		p.fail(at, ReasonWrongType, "%s holds a string or a list of strings", path)
		return nil, false
	}
	canon := make([]string, 0, len(raw))
	for _, r := range raw {
		c, err := row.Decl.Kind.Parse(r)
		if err != nil {
			p.fail(at, ReasonTemplateValue, "%v", err)
			return nil, false
		}
		if slices.Contains(canon, c) {
			p.fail(at, ReasonTemplateValue, "%s names %s twice", path, c)
			return nil, false
		}
		canon = append(canon, c)
	}
	return canon, true
}

// readChecks reads the check tables and expands each into its flat list of
// tests. A check holds the tests it names and the checks it names,
// transitively; a cycle among them is refused where it closes.
func (p *parser) readChecks(raw map[string]any) map[string]Check {
	tbl, _ := p.tableAt(raw, "", "checks")
	// Every test in a check is read against these names, so they are set
	// before the first expansion, not after.
	p.checkNames = map[string]bool{}
	for name := range tbl {
		p.checkNames[name] = true
	}
	p.checks = &expander{p: p, raw: tbl, done: map[string]expansion{}}
	out := map[string]Check{}
	for _, name := range sortedKeys(tbl) {
		if tests, ok := p.checks.expand(name); ok {
			out[name] = Check{Name: name, Tests: tests}
		}
	}
	return out
}

// expander expands each check once to its flat test list. A check is
// expanded the first time anything names it, and its outcome, refused or not,
// is kept, so each fault in a check is reported once however often it is
// named.
type expander struct {
	p    *parser
	raw  map[string]any
	open []string // the checks being expanded, outermost first
	done map[string]expansion
}

// expansion is one check's outcome: its tests, or ok false when it was
// refused, its faults already reported.
type expansion struct {
	tests []Test
	ok    bool
}

func (x *expander) expand(name string) ([]Test, bool) {
	if e, ok := x.done[name]; ok {
		return e.tests, e.ok
	}
	at := keyed("checks", name)
	if i := slices.Index(x.open, name); i >= 0 {
		x.p.fail(at, ReasonCheckCycle, "check %s includes itself: %s", name, strings.Join(slices.Concat(x.open[i:], []string{name}), " -> "))
		return nil, false
	}
	x.open = append(x.open, name)
	tests, ok := x.body(name, at)
	x.open = x.open[:len(x.open)-1]
	x.done[name] = expansion{tests: tests, ok: ok}
	return tests, ok
}

// body reads one check's table: its name, and each of its tests.
func (x *expander) body(name string, at KeyPath) ([]Test, bool) {
	if !isName(name) {
		x.p.fail(at, ReasonBadCheckName, "%q is not a check name: a lowercase word", name)
		return nil, false
	}
	if len(x.p.table.Named(field.Path(name))) > 0 {
		x.p.fail(at, ReasonCheckCollides, "%s is a field's name, so a check cannot take it", name)
		return nil, false
	}
	body, isTable := x.raw[name].(map[string]any)
	if !isTable {
		x.p.fail(at, ReasonWrongType, "a check is a table with its tests")
		return nil, false
	}
	x.p.unknown(body, at, "tests")
	entries, _ := x.p.list(body, at, "tests")
	var tests []Test
	ok := true
	for i, item := range entries {
		eat := indexed(keyed(at, "tests"), i)
		m, isTable := item.(map[string]any)
		if !isTable {
			x.p.fail(eat, ReasonWrongType, "a test is a table")
			ok = false
			continue
		}
		x.p.unknown(m, eat, "test", "check", "scope", "severity")
		if x.p.has(m, "severity") {
			x.p.fail(keyed(eat, "severity"), ReasonUnknownKey, "a check's tests carry no severity: the column that lists the check sets it")
			ok = false
		}
		t, good := x.entry(m, eat, name)
		if !good {
			ok = false
			continue
		}
		tests = append(tests, t...)
	}
	if !ok {
		return nil, false
	}
	return tests, true
}

// entry reads one entry of a list of tests, in a check or on a column: a
// test, declared in the check it sits in (declaredIn, empty on a column), or a
// check reference, which brings that check's tests with their own
// declarations. It is the one reader of an entry `[LAW:single-enforcer]`.
func (x *expander) entry(m map[string]any, at KeyPath, declaredIn string) ([]Test, bool) {
	hasTest, hasCheck := x.p.has(m, "test"), x.p.has(m, "check")
	if hasTest == hasCheck {
		x.p.fail(at, ReasonTestOrCheck, "an entry names a test or a check, one of them")
		return nil, false
	}
	if hasCheck {
		name, ok := x.p.text(m, at, "check")
		if !ok {
			return nil, false
		}
		if !x.p.checkNames[name] {
			x.p.fail(keyed(at, "check"), ReasonCheckUndeclared, "%s names no declared check", name)
			return nil, false
		}
		if x.p.has(m, "scope") {
			x.p.fail(keyed(at, "scope"), ReasonUnknownKey, "a check reference takes no scope: its tests carry their own")
			return nil, false
		}
		return x.expand(name)
	}
	src, ok := x.p.text(m, at, "test")
	if !ok {
		return nil, false
	}
	t, good := x.p.test(src, m, at, declaredIn)
	if !good {
		return nil, false
	}
	return []Test{t}, true
}

// test reads one atom and its scope, and checks the atom against the fields
// the workspace declares and each value against the kind of every field the
// path names.
func (p *parser) test(src string, m map[string]any, at KeyPath, declaredIn string) (Test, bool) {
	scope := Self
	if word, ok := m["scope"]; ok {
		switch word {
		case "self":
			scope = Self
		case "ancestors":
			scope = Ancestors
		default:
			p.fail(keyed(at, "scope"), ReasonBadScope, "scope is self or ancestors, not %v", word)
			return Test{}, false
		}
	}
	atom, err := field.ParseAtom(src)
	if err != nil {
		p.fail(keyed(at, "test"), ReasonBadAtom, "%v", err)
		return Test{}, false
	}
	path := atom.Path()
	// A field's name is the field's: a check that takes one is refused for
	// that, and no test that reads the field is.
	decls := p.table.Named(path)
	if len(decls) == 0 && p.checkNames[string(path)] {
		p.fail(keyed(at, "test"), ReasonPathIsCheck, "%s names a check: name it with check = %q", path, path)
		return Test{}, false
	}
	if len(decls) == 0 {
		p.fail(keyed(at, "test"), ReasonPathUndeclared, "%s is neither a built-in field nor declared on any level", path)
		return Test{}, false
	}
	var values []string
	switch op := atom.Op().(type) {
	case field.Equals:
		values = op.Values
	case field.NotEquals:
		values = op.Values
	}
	for _, d := range decls {
		for _, v := range values {
			if _, err := d.Kind.Parse(v); err != nil {
				p.fail(keyed(at, "test"), ReasonAtomValue, "%v", err)
				return Test{}, false
			}
		}
	}
	return Test{Atom: atom, Scope: scope, DeclaredIn: declaredIn}, true
}

// links reads the link types.
func (p *parser) links(raw map[string]any, levelCount int) map[string]LinkType {
	tbl, _ := p.tableAt(raw, "", "links")
	out := map[string]LinkType{}
	for _, name := range sortedKeys(tbl) {
		at := keyed("links", name)
		if !isName(name) {
			p.fail(at, ReasonBadLinkName, "%q is not a link name: a lowercase word", name)
			continue
		}
		m, ok := tbl[name].(map[string]any)
		if !ok {
			p.fail(at, ReasonWrongType, "a link type is a table")
			continue
		}
		p.unknown(m, at, "kind", "between")
		var lt LinkType
		kindWord, ok := p.need(m, at, "kind", ReasonMissingKey, "a link type names its kind: dependency or relates")
		if !ok {
			continue
		}
		switch kindWord {
		case "dependency":
			lt.Kind = Dependency
		case "relates":
			lt.Kind = Relates
		default:
			p.fail(keyed(at, "kind"), ReasonBadLinkKind, "a link kind is dependency or relates, not %q", kindWord)
			continue
		}
		pairs, ok := p.list(m, at, "between")
		if !ok && p.has(m, "between") {
			continue
		}
		if len(pairs) == 0 {
			p.fail(keyed(at, "between"), ReasonEmptyBetween, "a link type names at least one pair of levels")
			continue
		}
		good := true
		for i, item := range pairs {
			pat := indexed(keyed(at, "between"), i)
			pair, ok := item.([]any)
			if !ok || len(pair) != 2 {
				p.fail(pat, ReasonBadPair, "a pair is [from, to], two level numbers")
				good = false
				continue
			}
			from, okFrom := pair[0].(int64)
			to, okTo := pair[1].(int64)
			if !okFrom || !okTo {
				p.fail(pat, ReasonBadPair, "a pair is [from, to], two level numbers")
				good = false
				continue
			}
			if from < 1 || int(from) > levelCount || to < 1 || int(to) > levelCount {
				p.fail(pat, ReasonPairLevelUndeclared, "pair [%d, %d] names a level that is not declared", from, to)
				good = false
				continue
			}
			lt.Between = append(lt.Between, [2]int{int(from), int(to)})
		}
		if good {
			out[name] = lt
		}
	}
	return out
}

// columns reads the columns table of m, the root's or a section's. at is the
// key path of m, so every refusal names where it is.
func (p *parser) columns(m map[string]any, at KeyPath) map[string]Column {
	tbl, _ := p.tableAt(m, at, "columns")
	out := map[string]Column{}
	for _, name := range sortedKeys(tbl) {
		cat := keyed(keyed(at, "columns"), name)
		cm, ok := tbl[name].(map[string]any)
		if !ok {
			p.fail(cat, ReasonWrongType, "a column is a table")
			continue
		}
		if c, ok := p.column(cat, name, cm); ok {
			out[name] = c
		}
	}
	return out
}

// column reads one column's shape. The variant is decided here, so a column
// that has an exit list and is terminal is refused before it exists.
func (p *parser) column(at KeyPath, name string, m map[string]any) (Column, bool) {
	p.unknown(m, at, "initial", "terminal", "next", "aliases", "entry", "exit")
	if !isName(name) {
		p.fail(at, ReasonBadColumnName, "%q is not a column name: a lowercase word", name)
		return nil, false
	}
	initial, _ := p.flag(m, at, "initial")
	terminal, _ := p.flag(m, at, "terminal")
	aliases, _ := p.strings(m, at, "aliases")
	for _, a := range aliases {
		if !isName(a) {
			p.fail(keyed(at, "aliases"), ReasonBadColumnName, "alias %q is not a column name", a)
		}
	}
	entry := p.requirements(at, m, "entry")
	if initial && terminal {
		p.fail(at, ReasonInitialAndTerm, "a column is initial or terminal, not both")
		return nil, false
	}
	base := columnBase{name: name, aliases: aliases, entry: entry}
	if terminal {
		if p.has(m, "exit") {
			p.fail(keyed(at, "exit"), ReasonExitOnTerminal, "a terminal column has no forward edge, so no exit list")
		}
		if p.has(m, "next") {
			p.fail(keyed(at, "next"), ReasonNextOnTerminal, "a terminal column has no next")
		}
		return TerminalColumn{columnBase: base}, true
	}
	next, _ := p.strings(m, at, "next")
	exit := p.requirements(at, m, "exit")
	if initial {
		return InitialColumn{columnBase: base, next: next, exit: exit}, true
	}
	return ActiveColumn{columnBase: base, next: next, exit: exit}, true
}

// requirements reads a column's entry or exit list: each entry a test or a
// check, with the severity it carries. at is the key path of the list.
func (p *parser) requirements(at KeyPath, m map[string]any, key string) []Requirement {
	list, _ := p.list(m, at, key)
	var out []Requirement
	for i, item := range list {
		eat := indexed(keyed(at, key), i)
		e, ok := item.(map[string]any)
		if !ok {
			p.fail(eat, ReasonWrongType, "a requirement is a table")
			continue
		}
		p.unknown(e, eat, "test", "check", "scope", "severity")
		sev, sevOK := p.severity(e, eat)
		tests, ok := p.checks.entry(e, eat, "")
		if !sevOK || !ok {
			continue
		}
		for _, t := range tests {
			out = append(out, Requirement{Test: t, Severity: sev})
		}
	}
	return out
}

// severity reads an entry's severity: error or warning, and never a default.
func (p *parser) severity(m map[string]any, at KeyPath) (Severity, bool) {
	word, ok := p.need(m, at, "severity", ReasonBadSeverity, "an entry names its severity: error or warning")
	if !ok {
		return 0, false
	}
	switch word {
	case "error":
		return Error, true
	case "warning":
		return Warning, true
	}
	p.fail(keyed(at, "severity"), ReasonBadSeverity, "severity is error or warning, not %q", word)
	return 0, false
}

// sections reads the node sections: per-node columns and claim lifetimes,
// keyed by an item's full id or a declared home.
func (p *parser) sections(raw map[string]any, levels []Level) map[NodeKey]Section {
	tbl, _ := p.tableAt(raw, "", "nodes")
	homes := map[string]bool{}
	for _, lv := range levels {
		if lv.Home != "" {
			homes[lv.Home] = true
		}
	}
	sections := map[NodeKey]Section{}
	for _, key := range sortedKeys(tbl) {
		node, ok := nodeKey(key, homes)
		if !ok {
			p.fail(keyed("nodes", `"`+key+`"`), ReasonBadNodeKey, "%q is neither a declared home nor an item's full id, its 26 digits bare; a section never names its item by a short id, which can come to name two", key)
			continue
		}
		at := keyed("nodes", nodeSegment(node))
		m, ok := tbl[key].(map[string]any)
		if !ok {
			p.fail(at, ReasonWrongType, "a section is a table")
			continue
		}
		p.unknown(m, at, "columns", "claim_ttl")
		var sec Section
		if d, ok := p.duration(m, at, "claim_ttl"); ok {
			sec.ClaimTTL = declared(d)
		}
		sec.Columns = p.columns(m, at)
		sections[node] = sec
	}
	return sections
}

// nodeKey reads a section key: a declared home, or else an item's full id,
// bare, its one spelling. A committed file outlives every sync, and a
// segment two offline writers fixed for two items names neither, so a key
// is the item's identity, never its short form (§3.2).
func nodeKey(key string, homes map[string]bool) (NodeKey, bool) {
	if homes[key] {
		return HomeNode(key), true
	}
	id, err := events.ParseItemID(key)
	return ItemNode(id), err == nil
}

// duration reads a claim lifetime: a positive Go duration.
func (p *parser) duration(m map[string]any, at KeyPath, key string) (time.Duration, bool) {
	s, ok := p.text(m, at, key)
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		p.fail(keyed(at, key), ReasonBadDuration, "%q is not a positive duration: 6h, 30m", s)
		return 0, false
	}
	return d, true
}

// isName is whether s is a name a person writes, a lowercase word, by the
// one grammar of names, events' `[LAW:one-source-of-truth]`.
func isName(s string) bool {
	_, err := events.ParseName(s)
	return err == nil
}

// nodeSegment is a section key as it is written in the file.
func nodeSegment(n NodeKey) string {
	if n.home != "" {
		return n.home
	}
	return `"` + n.item.String() + `"`
}

// sortedNodeKeys orders section keys for a stable refusal order.
func sortedNodeKeys[V any](m map[NodeKey]V) []NodeKey {
	keys := make([]NodeKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return nodeSegment(keys[i]) < nodeSegment(keys[j]) })
	return keys
}

// --- readers of one table's keys. Each reports a fault at its key path and
// returns what it could read.

func (p *parser) unknown(m map[string]any, at KeyPath, allowed ...string) {
	for _, k := range sortedKeys(m) {
		if !slices.Contains(allowed, k) {
			p.fail(keyed(at, k), ReasonUnknownKey, "%s is not a key here", k)
		}
	}
}

func (p *parser) tableAt(m map[string]any, at KeyPath, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	t, ok := v.(map[string]any)
	if !ok {
		p.fail(keyed(at, key), ReasonWrongType, "%s is a table", key)
		return nil, false
	}
	return t, true
}

// need reads a string a table must hold. An absent one is refused with r and
// msg; one of the wrong type is refused once, by text, as wrong-type.
func (p *parser) need(m map[string]any, at KeyPath, key string, r Reason, msg string) (string, bool) {
	if !p.has(m, key) {
		p.fail(keyed(at, key), r, "%s", msg)
		return "", false
	}
	return p.text(m, at, key)
}

func (p *parser) text(m map[string]any, at KeyPath, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		p.fail(keyed(at, key), ReasonWrongType, "%s is a string", key)
		return "", false
	}
	return s, true
}

func (p *parser) flag(m map[string]any, at KeyPath, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	if !ok {
		p.fail(keyed(at, key), ReasonWrongType, "%s is true or false", key)
		return false, false
	}
	return b, true
}

func (p *parser) list(m map[string]any, at KeyPath, key string) ([]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	l, ok := v.([]any)
	if !ok {
		p.fail(keyed(at, key), ReasonWrongType, "%s is a list", key)
		return nil, false
	}
	return l, true
}

func (p *parser) strings(m map[string]any, at KeyPath, key string) ([]string, bool) {
	l, ok := p.list(m, at, key)
	if !ok {
		return nil, false
	}
	// A list with an entry of the wrong type is refused whole, so nothing reads
	// what is left of it as if the person had written that.
	out := make([]string, 0, len(l))
	good := true
	for i, item := range l {
		s, ok := item.(string)
		if !ok {
			p.fail(indexed(keyed(at, key), i), ReasonWrongType, "an entry of %s is a string", key)
			good = false
			continue
		}
		out = append(out, s)
	}
	if !good {
		return nil, false
	}
	return out, true
}

// declared wraps a value a section sets.
func declared[T any](v T) Declared[T] { return Declared[T]{v: v, ok: true} }
