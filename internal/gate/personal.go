package gate

import (
	"slices"
	"strings"
)

// agentSubcommands is the explicit table of what an agent may run as the
// tool alone (behavior.md, "The gate", Bash rule 1). A subcommand is on it
// because it redacts on its own, writes nothing that controls the gate, and
// needs no person. Everything else, a subcommand that does not exist yet
// included, is not "the tool alone" and goes to the ordinary rules: the deny
// comes before the capability.
//
// 'policy' is allowed for 'policy status' only (toolAloneSubcommand).
var agentSubcommands = map[string]bool{
	"scan":      true, // redacts its findings
	"scan-text": true, // redacts its findings
	"explain":   true, // prints structure, never a value
	"baseline":  true, // record writes a reviewable file and refuses under CI; verify reads
	"version":   true,
	"help":      true,
	"--help":    true,
	"-h":        true,
	"--version": true,
}

// personOnlySubcommands are the words after 'policy' that are a person's
// step at a terminal. A word that begins with one counts, as does any word
// the gate cannot tell: it could be one.
var personOnlySubcommands = []string{"approve", "unenroll"}

// personWord reports whether a begins with a person-only subcommand.
func personWord(a string) bool {
	return slices.ContainsFunc(personOnlySubcommands, func(p string) bool { return strings.HasPrefix(a, p) })
}

// tellable reports whether the gate can tell a word: it holds no
// expansion.
func tellable(a string) bool { return !strings.ContainsAny(a, "$`") }

// personOnly reports whether the tool's arguments (the words after its
// name) run, or could run, a person-only subcommand: 'policy' and, after
// it, a word that begins with one or that cannot be told. known says which
// words the gate can tell (nil: judge by the text). fed is set when a
// runner (xargs, parallel) adds words the gate cannot see, which could be
// 'policy approve' whole.
func personOnly(args []string, known []bool, fed bool) bool {
	policy := slices.Index(args, "policy")
	if fed && (policy >= 0 || !slices.ContainsFunc(args, func(a string) bool { return agentSubcommands[a] })) {
		return true
	}
	unknownSeen := false
	for j, a := range args {
		unknown := !tellable(a) || known != nil && j < len(known) && !known[j]
		switch {
		case policy >= 0 && j > policy && (unknown || personWord(a)):
			return true
		case policy < 0 && unknownSeen && personWord(a):
			// 'pbipx $X approve': the unknown word could be 'policy'.
			return true
		}
		unknownSeen = unknownSeen || unknown
	}
	return false
}

// toolAloneSubcommand reports whether the tool's arguments (the words after
// its name) name a subcommand on the table that an agent may run alone.
func toolAloneSubcommand(rest []string) bool {
	if len(rest) == 0 || personOnly(rest, nil, false) {
		return false
	}
	if rest[0] == "policy" {
		return len(rest) >= 2 && rest[1] == "status"
	}
	return agentSubcommands[rest[0]]
}
