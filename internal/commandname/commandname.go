// Package commandname is each lit2 command's name as a person types it:
// the one spelling cli's rows parse and help by, the command log records,
// and every message that tells a person which command to run reads
// `[LAW:one-source-of-truth]`. It sits below every layer that names a
// command, so renaming or regrouping a command is one edit here, and the
// identifiers are the commands' own, never their groups', so none of them
// lies after a regroup.
package commandname

// Name is a command as typed after lit2: a verb, or a group's word and the
// command in it. Its fields are unexported, so a name is one of this
// package's or one built through Verb and InGroup, which only a test of a
// line other than lit2's builds.
type Name struct {
	group, word string
	grouped     bool
}

// Verb is the name of a verb on paths, outside every group.
func Verb(word string) Name { return Name{word: word} }

// InGroup is the name of the command word in the group group. An empty
// group is still a group, so cli refuses its word rather than reading it
// as a verb.
func InGroup(group, word string) Name { return Name{group: group, word: word, grouped: true} }

// Words is the name as argv spells it: the verb, or the group then the
// command.
func (n Name) Words() []string {
	if !n.grouped {
		return []string{n.word}
	}
	return []string{n.group, n.word}
}

// Group is the group the name is in, and whether it is in one.
func (n Name) Group() (string, bool) { return n.group, n.grouped }

func (n Name) String() string {
	if !n.grouped {
		return n.word
	}
	return n.group + " " + n.word
}

// Get reads nodes.
func Get() Name { return Verb("get") }

// Set replaces values.
func Set() Name { return Verb("set") }

// Add appends values.
func Add() Name { return Verb("add") }

// Rm deletes nodes.
func Rm() Name { return Verb("rm") }

// Init makes a checkout a writer; it keeps a checkout's store, with Sync.
func Init() Name { return InGroup("admin", "init") }

// Sync publishes and fetches events.
func Sync() Name { return InGroup("admin", "sync") }

// Commit applies the committed workspace file; it applies the workspace's
// versions, with Status.
func Commit() Name { return InGroup("config", "commit") }

// Status reports the version in force against the file.
func Status() Name { return InGroup("config", "status") }

// Rows is the name of every command lit2's command line has a row for,
// the one list cli's test holds lit2's rows to, so no row is named but
// here.
func Rows() []Name { return []Name{Get(), Set(), Add(), Rm(), Init(), Sync(), Commit(), Status()} }

// Unparsed names an argv that did not parse.
func Unparsed() Name { return Verb("unparsed") }

// Help names --help, asked anywhere on the line.
func Help() Name { return Verb("help") }

// Forged names a test's forged write, run by the twin, never by a person.
func Forged() Name { return Verb("forge") }

// Reserved is the names that are no row's: a command the log records by
// one of them is not a command a row parsed, so no row may take one.
func Reserved() []Name { return []Name{Unparsed(), Help(), Forged()} }
