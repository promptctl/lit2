// Package commandname is each lit2 command's name as a person types it:
// the one spelling cli's rows parse and help by, the command log records,
// and every message that tells a person which command to run reads
// `[LAW:one-source-of-truth]`. It sits below every layer that names a
// command, so a rename here is the whole rename.
package commandname

// Name is a command as typed after lit2: a verb, or a group's word and the
// command in it. Its fields are unexported, so a name is one of this
// package's or one a test builds through Verb and InGroup.
type Name struct{ group, word string }

// Verb is the name of a verb on paths, outside every group.
func Verb(word string) Name { return Name{word: word} }

// InGroup is the name of the command word in the group group.
func InGroup(group, word string) Name { return Name{group: group, word: word} }

// Words is the name as argv spells it: the verb, or the group then the
// command.
func (n Name) Words() []string {
	if n.group == "" {
		return []string{n.word}
	}
	return []string{n.group, n.word}
}

// Group is the group the name is in, and whether it is in one.
func (n Name) Group() (string, bool) { return n.group, n.group != "" }

func (n Name) String() string {
	if n.group == "" {
		return n.word
	}
	return n.group + " " + n.word
}

// The verbs on paths.
func Get() Name { return Verb("get") }
func Set() Name { return Verb("set") }
func Add() Name { return Verb("add") }
func Rm() Name  { return Verb("rm") }

// The admin group: what keeps a checkout's store.
func AdminInit() Name { return InGroup("admin", "init") }
func AdminSync() Name { return InGroup("admin", "sync") }

// The config group: what applies the workspace's versions.
func ConfigCommit() Name { return InGroup("config", "commit") }
func ConfigStatus() Name { return InGroup("config", "status") }

// Unparsed names an argv that did not parse.
func Unparsed() Name { return Verb("unparsed") }

// Help names --help, asked anywhere on the line.
func Help() Name { return Verb("help") }

// Forged names a test's forged write, run by the twin, never by a person.
func Forged() Name { return Verb("forge") }

// Reserved is the names that are no row's: a command the log records by
// one of them is not a command a row parsed, so no row may take one.
func Reserved() []Name { return []Name{Unparsed(), Help(), Forged()} }
