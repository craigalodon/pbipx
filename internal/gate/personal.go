package gate

import (
	"maps"
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
// 'policy' is allowed for 'policy status' and bare (toolAloneSubcommand).
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

// personOnlyCommands are the tool's own subcommands that are a person's
// step at a terminal, with every flag: init writes the user's Claude Code
// settings and the approval store, gate is the hook. A subcommand joins
// this list in the change that builds it, if an agent may not run it.
var personOnlyCommands = map[string]bool{"init": true, "gate": true}

// terminalWrappers are the programs that give what they run a terminal,
// which approval and --show-values ask for.
var terminalWrappers = []string{"script", "expect", "unbuffer", "winpty"}

// showValuesFlag is the tool's flag that prints values, refused without a
// terminal and so unsafe under a program that supplies one.
const showValuesFlag = "--show-values"

// personWord reports whether a begins with a person-only subcommand.
func personWord(a string) bool {
	return slices.ContainsFunc(personOnlySubcommands, func(p string) bool { return strings.HasPrefix(a, p) })
}

// expands reports whether a word holds an expansion whose value the gate
// cannot tell.
func expands(a string) bool { return strings.ContainsAny(a, "$`") }

// untellable reports whether the gate cannot tell a word: it holds an
// expansion, or a glob character the shell may expand to a file name.
func untellable(a string) bool { return strings.ContainsAny(a, "$`*?[") }

func helpFlag(a string) bool { return a == "--help" || a == "-h" }

// personOnly reports whether the tool's arguments (the words after its
// name) run, or could run, a person-only subcommand: init or gate; 'policy'
// and, after it, a word that begins with approve or unenroll or that cannot
// be told; a subcommand word that cannot be told, which could be any of
// them. known says which words the gate can tell (nil: judge by the text).
// fed is set when a runner (xargs, parallel) adds words the gate cannot see,
// which could be 'policy approve' whole. A help request for init or gate
// alone runs nothing.
func personOnly(args []string, known []bool, fed bool) bool {
	policy := slices.Index(args, "policy")
	if fed && (policy >= 0 || !slices.ContainsFunc(args, func(a string) bool { return agentSubcommands[a] })) {
		return true
	}
	unknownAt := func(j int) bool { return untellable(args[j]) || known != nil && j < len(known) && !known[j] }
	// The subcommand is the first word that is not an option: the tool's
	// options (--help, --version) take no value.
	for j, a := range args {
		if unknownAt(j) {
			return true
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if personOnlyCommands[a] {
			return !slices.ContainsFunc(args, helpFlag) || !onlyHelpBesides(args, j)
		}
		break
	}
	unknownSeen := false
	for j, a := range args {
		switch {
		case policy >= 0 && j > policy && (unknownAt(j) || personWord(a)):
			return true
		case policy < 0 && unknownSeen && personWord(a):
			// 'pbipx $X approve': the unknown word could be 'policy'.
			return true
		}
		unknownSeen = unknownSeen || expands(a) || known != nil && j < len(known) && !known[j]
	}
	return false
}

// onlyHelpBesides reports whether every word of args but the one at j is a
// help flag.
func onlyHelpBesides(args []string, j int) bool {
	for i, a := range args {
		if i != j && !helpFlag(a) {
			return false
		}
	}
	return true
}

// toolAloneSubcommand reports whether the tool's arguments (the words after
// its name) name a subcommand on the table that an agent may run alone. A
// help flag anywhere makes the tool print help and run nothing, so it
// allows unless a person-only subcommand is named.
func toolAloneSubcommand(rest []string) bool {
	if personOnly(rest, nil, false) {
		return false
	}
	if len(rest) == 0 || slices.ContainsFunc(rest, helpFlag) {
		return true // the usage, or help
	}
	if rest[0] == "policy" {
		return len(rest) == 1 || slices.Contains(AgentPolicySubcommands(), rest[1])
	}
	return agentSubcommands[rest[0]]
}

// revealsValues reports whether v asks the tool for --show-values: by name,
// or in a string given to a program that supplies a terminal, where the
// flag's refusal without a terminal does not hold. An agent has no use for
// the flag, so no tree is consulted. Under such a program a word of the
// tool's the gate cannot tell could be the flag.
func revealsValues(v view) bool {
	switch {
	case v.name == toolName:
		if slices.ContainsFunc(v.rest, func(a string) bool { return strings.HasPrefix(a, showValuesFlag) }) {
			return true
		}
		return slices.ContainsFunc(v.under, func(u string) bool { return slices.Contains(terminalWrappers, u) }) &&
			slices.ContainsFunc(v.rest, untellable)
	case slices.Contains(terminalWrappers, v.name):
		text := strings.Join(v.rest, " ")
		if strings.Contains(text, showValuesFlag) {
			return true
		}
		tokens := terminalTokens(v.rest)
		for i, t := range tokens {
			if progName(t) == toolName && slices.ContainsFunc(tokens[i+1:], untellable) {
				return true
			}
		}
	}
	return false
}

// terminalTokens are the words in the arguments of a program that runs a
// command string: split at spaces, quotes and shell punctuation.
func terminalTokens(rest []string) []string {
	return strings.FieldsFunc(strings.Join(rest, " "), func(r rune) bool {
		return strings.ContainsRune(" \t\r\n;&|'\"{}()", r)
	})
}

// personStep reports whether v runs a step that is a person's at a
// terminal, and the message for the deny. The message never names a
// subcommand an agent could try.
func personStep(v view) (string, bool) {
	switch {
	case selfApproval(v):
		return "the command runs a subcommand of the tool that is a person's step at a terminal (approving the policy, " +
			"setting pbipx up); an agent may not run it", true
	case revealsValues(v):
		return "the command asks the tool to print values, which only a person at a terminal may do; " +
			"the tool's output stays redacted for an agent", true
	}
	return "", false
}

// AgentSubcommands are the tool's own subcommands, and flags, that an agent
// may run alone (agentSubcommands), sorted. 'policy' is not among them: it
// is allowed bare and for AgentPolicySubcommands.
func AgentSubcommands() []string {
	return slices.Sorted(maps.Keys(agentSubcommands))
}

// AgentPolicySubcommands are the words after 'policy' that an agent may
// run.
func AgentPolicySubcommands() []string { return []string{"status"} }

// PersonOnlyCommands are the tool's own subcommands that are a person's
// step at a terminal, sorted (personOnlyCommands).
func PersonOnlyCommands() []string {
	return slices.Sorted(maps.Keys(personOnlyCommands))
}

// PersonOnlyPolicySubcommands are the words after 'policy' that are a
// person's step at a terminal (personOnlySubcommands).
func PersonOnlyPolicySubcommands() []string { return slices.Clone(personOnlySubcommands) }
