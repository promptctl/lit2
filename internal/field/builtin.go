package field

// Path names a field of an item by its slash path: `description`,
// `plan/verdict`. It is the name a test's atom reads and a template's key
// names.
type Path string

// Origin is who declared a field: the binary, or the workspace file.
type Origin uint8

const (
	Builtin Origin = iota + 1
	Declared
)

// Writability is whether a person may set a field's value. It is independent
// of Origin: a built-in field can be settable, and a declared one derived
// `[LAW:types-are-the-program]`.
type Writability uint8

const (
	Settable Writability = iota + 1
	Derived
)

// Multiplicity is whether a field holds one value or many.
type Multiplicity uint8

const (
	One Multiplicity = iota + 1
	Many
)

// FieldDecl describes one field, built-in or declared, in one shape: what it
// holds, who declared it, and whether a person can write it.
type FieldDecl struct {
	Kind        Kind
	Multi       Multiplicity
	Origin      Origin
	Writability Writability
}

// Builtins is the built-in field table. Its one input is the workspace's
// declared labels as an enum, so `labels` reads the declared value set and
// nothing else. A zero Enum is no labels declared.
func Builtins(labels Enum) map[Path]FieldDecl {
	verdict, _ := NewEnum([]string{"pass", "fail"})
	return map[Path]FieldDecl{
		"description":        {Kind: Text{}, Multi: One, Origin: Builtin, Writability: Settable},
		"reason":             {Kind: Text{}, Multi: One, Origin: Builtin, Writability: Settable},
		"labels":             {Kind: labels, Multi: Many, Origin: Builtin, Writability: Settable},
		"of":                 {Kind: Text{}, Multi: Many, Origin: Builtin, Writability: Derived},
		"blockers":           {Kind: Text{}, Multi: Many, Origin: Builtin, Writability: Derived},
		"unmet_requirements": {Kind: Text{}, Multi: Many, Origin: Builtin, Writability: Derived},
		"pr/verdict":         {Kind: verdict, Multi: One, Origin: Builtin, Writability: Derived},
		"plan/verdict":       {Kind: verdict, Multi: One, Origin: Builtin, Writability: Derived},
	}
}
