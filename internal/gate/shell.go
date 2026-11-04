package gate

import (
	"cmp"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
)

// toolName is the command that runs pbipx itself.
const toolName = "pbipx"

// maxDepth is how deeply command lines nested in a command line (sh -c,
// eval, a command substitution) are read. A line nested deeper counts as
// touching every protected root.
const maxDepth = 4

// maxCommands bounds the simple commands one line holds, nested lines
// included. No command line an agent writes comes near it; one past it is
// denied rather than judged in part, so the hook stays inside its time
// limit.
const maxCommands = 20000

// shell decides a shell command line (behavior.md, "The gate", Bash, and
// the local-state divergence in Clarifications). The line is never quoted
// in a message: it can carry anything.
func (s *setup) shell(line string) Decision {
	s.ctlVars = ctlVarsFixed(line)
	top := splitCommands(line)
	w := &walker{s: s, cache: newSpellCache(), text: line}
	st := cwdState{dirs: []string{s.cwd}}
	w.commands(line, top, &st, 0)
	if w.tooMany {
		return deny(ReasonInternalError, "the command line holds more than %d commands, too many to judge", maxCommands)
	}
	if w.tooManyWords {
		return deny(ReasonInternalError, "brace expansion in the command line makes more than %d words, too many to judge", maxBraceLine)
	}
	if w.uncertainWhy != "" {
		// What the gate cannot read as bash would, it does not judge in
		// part.
		return deny(ReasonInternalError, "the gate cannot read the command line as bash would: %s. %s", w.uncertainWhy, hintSimplify)
	}

	// Before anything else: changing Claude Code's settings in the
	// repository, or approving the policy. Even the tool alone could
	// redirect into a settings file.
	ctl := loadControls()
	if w.interpreterNamesSettings() {
		return deny(ReasonAgentSettings, "an interpreter is given code that names a Claude Code settings file "+
			"(.claude/settings.json or .claude/settings.local.json); %s", agentSettingsWhy)
	}
	for _, v := range w.views {
		if what, ok := s.reachesControl(v, ctl); ok {
			return deny(ReasonAgentSettings, "the command names a path %s; %s", what, controlWhy)
		}
		if s.viewNamesAgentSettings(v) || s.intoAgentDir(v) {
			return deny(ReasonAgentSettings, "the command names a Claude Code settings file "+
				"(.claude/settings.json or .claude/settings.local.json) or writes into a .claude directory; %s", agentSettingsWhy)
		}
		if msg, ok := personStep(v); ok {
			return deny(ReasonAgentSettings, "%s", msg)
		}
	}
	// 1. The tool alone redacts on its own.
	if toolAlone(line, top) {
		return allow()
	}
	// 2. git printing content git history holds.
	if w.runtimeWhy != "" {
		// Code built at run time could run git, and history can always
		// be reached.
		return deny(ReasonGitContent, "%s, which could run git printing content; git history cannot be vetted. %s",
			w.runtimeWhy, hintPlain)
	}
	for _, v := range w.views {
		if v.name == "hub" {
			// hub runs git with its arguments.
			v = asGit(v)
		}
		if g := s.gitRule(v); g != nil {
			return deny(ReasonGitContent, "%s", g.message())
		}
		if ghPrintsPatch(v) {
			return deny(ReasonGitContent, "gh pr diff prints the pull request's patch, whose files can be in a "+
				"protected root; remote history cannot be vetted either. Use gh pr diff --name-only, or gh pr view.")
		}
	}
	// A git command not known to be read-only (gitReadOnly), then one that could
	// print file content: the second was judged against the repository
	// as it was before the first (in an unordered line, anywhere).
	if len(w.changes) > 0 {
		unordered := w.loopy || w.anywhere != ""
		headCommits := !slices.ContainsFunc(w.views, func(v view) bool { return headMayLeaveCommits(v.argv) })
		for _, v := range w.views {
			if v.name != "git" {
				continue
			}
			var before [][]string
			label := ""
			for _, c := range w.changes {
				if c.seq != v.seq && (unordered || c.seq < v.seq) {
					before = append(before, c.argv)
					label = cmp.Or(label, c.label)
				}
			}
			if len(before) > 0 && s.contentAfter(v.argv, s.viewDir(v), before, headCommits) {
				return deny(ReasonGitContent, "%s can change the index, HEAD or a ref, and a later git command in the line "+
					"could print file content; it would be judged against the repository as it was before. %s",
					label, hintChange)
			}
		}
	}
	// A variable git reads, set or exported by a command of its own in the
	// line, as an assignment before git is.
	if lineEnvUnsafe(w.views) {
		for _, v := range w.views {
			if v.name == "git" {
				return deny(ReasonGitContent, "%s", gitDenial{"git", "runs in a line that sets or exports a variable " +
					"(a GIT_ variable, a pager, an editor or LESSOPEN) that can change what it prints",
					"Drop the assignment, or run it as a separate call."}.message())
			}
		}
	}
	// git config setting a key a git command anywhere in the line reads:
	// the gate read the configuration as it is before the line runs.
	if slices.ContainsFunc(w.views, func(v view) bool { return afterConfig(v.argv) != "" }) && (lineSets(w.views, "HOME", "XDG_CONFIG_HOME") ||
		slices.ContainsFunc(w.views, func(v view) bool { return configWrite(v.argv, unknownArgs(v)) || s.writesGitDir(v) })) {
		for _, v := range w.views {
			if sub := afterConfig(v.argv); sub != "" {
				return deny(ReasonGitContent, "%s", gitDenial{sub, whyAfterConfig, hintAfterConfig}.message())
			}
		}
	}
	// A command that changes the working tree, then one that reads: the
	// read would be judged against the tree as it is before the change.
	// A change in a process substitution, or any change in a line with a
	// loop or a function: a read anywhere in the line can run after it.
	anywhere := w.anywhere
	if anywhere == "" && w.loopy {
		anywhere = w.changed
	}
	if anywhere != "" && !w.readAfterChange && slices.ContainsFunc(w.views, w.reads) {
		w.readAfterChange, w.changed = true, anywhere
	}
	if w.readAfterChange {
		return deny(ReasonGitContent,
			"%s changes the working tree, and a later command in the line reads files; the read would be judged "+
				"against the tree as it was before. %s",
			w.changed, hintChange)
	}
	// Divergence: naming local state denies, as a Read of it does.
	for _, v := range w.views {
		for _, xw := range v.words {
			if rel, ok := s.namesLocalState(v, xw); ok {
				return deny(ReasonLocalState, "the command names %s, Power BI Desktop local state, which is never read. %s",
					displayPath(rel), hintLocal)
			}
		}
	}
	// git putting history back in the working tree, then a command that
	// may write files, then git printing the working tree: what it prints
	// comes from history, which the tree scan below does not see.
	if change := w.historyThenWrite(); change != "" {
		return deny(ReasonGitContent, "%s puts content from history in the working tree, and a command after it may "+
			"write files that a later git command prints; it would be judged against the tree as it was before. %s",
			change, hintChange)
	}
	// A command that may write files, then git printing the working tree
	// (F1): the tree it prints is scanned as for Grep.
	if w.worktreeAfterWrite() {
		w.setReach(whyWorktreeAfterWrite, hintChange)
	}
	// 3 and 4: a command that can reach a protected root scans the tree.
	if t := w.reaches(); t.set() {
		return s.tree(t)
	}
	return allow()
}

// gitLabel is "git SUB" for one of git's own subcommands, and "git" for
// any other word: a word from the command line is never echoed.
func gitLabel(sub string) string {
	if gitBuiltins[sub] {
		return "git " + sub
	}
	return "git"
}

// gitTreeChangers are git's subcommands that write files from history, or
// from outside the tree, into a working tree, each with the arguments
// that make it not do so (a whole word, or a short option's letter).
var gitTreeChangers = map[string][]string{
	"switch": nil, "checkout": nil, "merge": nil, "pull": nil, "cherry-pick": nil,
	"revert": nil, "am": nil, "rebase": nil, "clone": nil, "checkout-index": nil,
	"filter-branch": nil, "mergetool": nil,
	"restore":         {"--staged", "-S"},
	"apply":           {"--check", "--stat", "--numstat", "--summary", "--cached"},
	"stash":           {"list", "show", "create", "store", "drop", "clear"},
	"reset":           nil, // judged by resetChanges
	"read-tree":       nil, // judged by its -u
	"sparse-checkout": {"list"},
	"bisect":          {"log", "visualize", "view", "terms", "help"},
	"submodule":       nil, // judged by its subcommand
	"worktree":        nil, // judged by its subcommand
	"bundle":          nil, // judged by its subcommand
	"merge-file":      {"-p", "--stdout"},
}

// unknownProgram says why v runs a program the gate cannot tell, "" when it
// can: a command word that holds an expansion ($(which git) log -p,
// ${GIT:-git}, git${X}, $G) or an unquoted glob (/usr/bin/g[i]t), given
// arguments or split into words, which could be git; or a name the line
// made stand for another program with hash -p or alias. It also records
// such names for the commands after it.
func (w *walker) unknownProgram(c simple, v view) string {
	if v.name == "" {
		return ""
	}
	if v.name == "hash" && slices.ContainsFunc(v.rest, func(a string) bool { return strings.HasPrefix(a, "-") && strings.Contains(a, "p") }) ||
		v.name == "alias" {
		for _, a := range v.rest {
			name, _, isAlias := strings.Cut(a, "=")
			if v.name == "alias" && isAlias || v.name == "hash" && !strings.HasPrefix(a, "-") && !strings.Contains(a, "/") {
				if w.hashed == nil {
					w.hashed = map[string]bool{}
				}
				w.hashed[progName(name)] = true
			}
		}
	}
	if w.hashed[v.name] {
		return "a command runs a name the line made stand for another program (hash -p, alias), which could be git"
	}
	// The program's word among the command's words.
	var args []int
	for i, wd := range c.words {
		if !wd.redirect {
			args = append(args, i)
		}
	}
	// The words that name what runs: the first command word (an unknown
	// one can be taken for a wrapper of the program after it) up to the
	// program's.
	first := len(args) - len(v.argv)
	for first < len(args) && isAssignment(c.words[args[first]].text) {
		first++
	}
	k := len(args) - len(v.rest) - 1
	// The wrappers' options, their values and operands (timeout's
	// duration, nice -n's value, env NAME=value) are not the command, even
	// when unknown: the wrappers' option tables say which words they are.
	_, _, _, valueWords := commandIdx(v.argv)
	for j := first; j <= k && j >= 0 && j < len(args); j++ {
		wd := c.words[args[j]]
		known := v.argvKnown[j-(len(args)-len(v.argv))]
		if valueWords[j-(len(args)-len(v.argv))] && j != k {
			continue
		}
		switch {
		case !known && j == k && w.literalProgram(wd):
			// A path whose directory cannot be told, to a program named
			// by its literal last element: that program.
		case !known && (j < len(args)-1 || wd.splits):
			return "a command word the gate cannot expand (a variable or a command substitution) runs a program it cannot tell"
		case wd.globs() && wd.text != "[" && wd.text != "[[" && j < len(args)-1:
			return "a command word with a glob runs a program the gate cannot tell"
		}
	}
	return ""
}

// literalProgram reports whether wd, a command word whose value cannot be
// told, is a path to a program named by its literal last element
// ("$(go env GOPATH)/bin/golangci-lint", "${GOBIN:-$HOME/go/bin}/staticcheck"),
// which the program is known by, whatever the directory. It is not when
// the last element holds an expansion or a glob; when it names a shell,
// an interpreter, a wrapper or eval, source or ., whose directory matters
// little but whose arguments do; or when the word could split into other
// words (an unquoted command substitution, positional parameters, or a
// variable the line could set).
func (w *walker) literalProgram(wd word) bool {
	var tail strings.Builder
	i := len(wd.parts)
	for i > 0 && wd.parts[i-1].kind == litPart {
		i--
	}
	for _, p := range wd.parts[i:] {
		tail.WriteString(p.text)
	}
	slash := strings.LastIndexByte(tail.String(), '/')
	if i == 0 || slash < 0 {
		return false
	}
	name := tail.String()[slash+1:]
	for _, p := range wd.parts[i:] {
		if !p.quoted && hasGlob(p.text) {
			return false
		}
	}
	base := progName(name)
	_, wrap := wrappers[base]
	_, interp := interpreters[interpreterFamily(base)]
	_, runner := runners[base]
	if base == "" || wrap || interp || runner || shells[base] || slices.Contains([]string{"eval", "source", "."}, base) {
		return false
	}
	for _, p := range wd.parts[:i] {
		if p.kind == litPart || p.inDouble {
			continue
		}
		if p.kind == tildePart {
			continue
		}
		// An unquoted expansion splits: what it holds must come from
		// outside the line.
		if strings.Contains(p.raw, "$(") || strings.Contains(p.raw, "`") ||
			strings.ContainsFunc(p.raw, func(r rune) bool { return r == '@' || r == '*' || r == '!' || '0' <= r && r <= '9' }) {
			return false
		}
		for _, m := range paramNames.FindAllStringSubmatch(p.raw, -1) {
			if setsName(w.text, m[1]) {
				return false
			}
		}
	}
	return true
}

// paramNames finds the names of the parameters an expansion reads.
var paramNames = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)`)

// setsName reports whether line names the variable name other than as an
// expansion ($name, ${name}), where it could set it (name=, read name,
// for name in, export name ...).
func setsName(line, name string) bool {
	for i := 0; i < len(line); {
		j := strings.Index(line[i:], name)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(name)
		before := at == 0 || !isNameRune(rune(line[at-1]))
		after := end >= len(line) || !isNameRune(rune(line[end]))
		expansion := at > 0 && line[at-1] == '$' || at > 1 && line[at-1] == '{' && line[at-2] == '$'
		if before && after && !expansion {
			return true
		}
		i = end
	}
	return false
}

// modeled reports whether the gate reads a command of that name in a way
// of its own: a builtin it follows, a wrapper, a reader, a search, a
// shell, an interpreter, git and the like. A function of that name makes
// the name stand for another program.
func modeled(name string) bool {
	_, wrap := wrappers[name]
	_, search := searchers[name]
	_, interp := interpreters[interpreterFamily(name)]
	return wrap || search || interp || readers[name] || shells[name] || inert[name] || redefiners[name] ||
		slices.Contains([]string{"cd", "pushd", "popd", "chdir", "git", "hub", "gh", "exec", "read", "test", "[",
			"find", "fd", "xargs", "trap", "printf", "echo", "type", "builtin", "command"}, name)
}

// promptVars are the variables bash runs, or expands as code, later: set
// anywhere in a line, they are code built at run time.
var promptVars = []string{"PS0", "PS1", "PS2", "PS4", "PROMPT_COMMAND", "BASH_ENV", "ENV", "SHELLOPTS", "BASHOPTS"}

// quotedCode reports whether c holds quoted text that bash would run if
// it were evaluated: $(, a backtick or ${ inside quotes.
func quotedCode(c simple) bool {
	for _, wd := range c.words {
		for _, p := range wd.parts {
			if p.kind == litPart && p.quoted && (strings.Contains(p.text, "$(") || strings.Contains(p.text, "`") ||
				strings.Contains(p.text, "${")) {
				return true
			}
		}
	}
	return false
}

// holdsCode reports whether s holds text bash would run if it were
// evaluated: $(, a backtick or ${.
func holdsCode(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "`") || strings.Contains(s, "${")
}

// valueSetters are the commands whose words, here-strings and
// here-documents can become a variable's value.
var valueSetters = map[string]bool{
	"read": true, "declare": true, "typeset": true, "local": true, "export": true, "readonly": true,
	"mapfile": true, "readarray": true, "let": true, "set": true, "getopts": true,
}

// assignWord matches a word that assigns a variable or an array element,
// NAME=, NAME+=, NAME[...]= or NAME[...]+=, or a compound array's
// [...]= element.
var assignWord = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*)?(?:\[[^\]]*\])?\+?=`)

// valueCode reports whether c holds quoted code (holdsCode) where it can
// become a variable's value, the only way an arithmetic context or a
// prompt expansion can read it: an assignment word; a word of a command
// that sets values (valueSetters, printf -v), of a for or select list, or
// of a compound array; a ${x=...} or ${x:=...} default; a here-string or
// here-document a value setter reads; and anything at all a command in a
// substitution or a nested line prints (depth above 0), or one whose
// output a pipe or a process substitution passes on. A git message, a sed
// script or an echo at the top of the line becomes no value.
func valueCode(c simple, v view, depth int, called bool) bool {
	setter := valueSetters[v.name] || v.name == "printf" && slices.Contains(v.rest, "-v")
	if setter {
		for _, doc := range c.docs {
			if holdsCode(doc.body) {
				return true
			}
		}
	}
	lead := c.words[:min(len(c.words), leadingKeywords(c.words)+1)]
	list := slices.ContainsFunc(lead, func(w word) bool { return !w.quoted && (w.text == "for" || w.text == "select") })
	// test -v, [[ -v and unset evaluate the subscript of the name they
	// are given, as the setters do.
	names := v.name == "test" || v.name == "[" || v.name == "[[" || v.name == "unset"
	whole := depth > 0 || c.pipeOut || c.procSub || c.arrayElems || setter || list || names || called
	for _, wd := range c.words {
		for _, p := range wd.parts {
			switch {
			case p.kind == litPart && p.quoted && holdsCode(p.text):
				if whole || wd.here || assignWord.MatchString(wd.text) {
					return true
				}
			case p.kind == opaquePart && strings.HasPrefix(p.raw, "${") && strings.Contains(p.raw, "=") && holdsCode(p.raw[2:]):
				// ${x=...} and ${x:=...} assign their default.
				return true
			}
		}
	}
	return false
}

// readsValueFromFile reports whether v reads a variable's value from a
// file (read or mapfile with an input redirection that is not a
// here-string), whose content the gate cannot see.
func readsValueFromFile(v view) bool {
	if v.name != "read" && v.name != "mapfile" && v.name != "readarray" {
		return false
	}
	return slices.ContainsFunc(v.words, func(xw xword) bool { return xw.redirect && xw.input && !xw.here })
}

// evaluates reports whether c makes bash evaluate a string as code: an
// array subscript or a name that test -v, [[ -v, printf -v, read,
// mapfile, declare -n (or a subscripted name), let or unset reads, an
// assignment to a variable declared -i, the operands of [[ -eq and the
// other arithmetic comparisons, an
// arithmetic expression that names a variable (whose value is evaluated
// in turn), or ${x@P} and ${x@E}, which expand a value as a prompt.
func evaluates(c simple, v view) bool {
	if c.arith {
		return true
	}
	for _, wd := range c.words {
		for _, p := range wd.parts {
			if p.kind != opaquePart {
				continue
			}
			if strings.HasPrefix(p.raw, "$((") || strings.HasPrefix(p.raw, "$[") {
				if strings.ContainsFunc(p.raw, func(r rune) bool { return r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }) {
					return true
				}
			}
			if strings.HasPrefix(p.raw, "${") && (strings.HasSuffix(p.raw, "@P}") || strings.HasSuffix(p.raw, "@E}")) {
				return true
			}
		}
	}
	subscript := slices.ContainsFunc(v.rest, func(a string) bool { return strings.Contains(a, "[") })
	switch v.name {
	case "test", "[":
		return slices.Contains(v.rest, "-v")
	case "[[":
		// [[ evaluates the operands of its arithmetic operators.
		return slices.ContainsFunc(v.rest, func(a string) bool {
			return slices.Contains([]string{"-v", "-eq", "-ne", "-lt", "-le", "-gt", "-ge"}, a)
		})
	case "printf":
		return slices.Contains(v.rest, "-v")
	case "read", "mapfile", "readarray", "let":
		return true
	case "declare", "typeset", "local", "export", "readonly":
		// -n reads a value as a name; -i evaluates every assignment to
		// the variable, anywhere in the line.
		return subscript || slices.ContainsFunc(v.rest, func(a string) bool {
			return strings.HasPrefix(a, "-") && strings.ContainsAny(a, "ni")
		})
	case "unset":
		return subscript
	}
	return false
}

// redefiners are the commands that can make a name run another program:
// an alias, a hashed path, a builtin turned off or loaded, and a script
// or text run in the shell itself, which can define a function.
var redefiners = map[string]bool{
	"alias": true, "hash": true, "enable": true, "source": true, ".": true, "eval": true,
	"function": true,
}

// redefines reports whether c may make cat or git run another program for
// the rest of the line: a function definition (name(), function name),
// alias, hash, enable, source, . or eval, or an assignment or export of
// PATH.
func redefines(c simple, v view) bool {
	if c.funcDef {
		return true
	}
	words := stripKeywords(c.args())
	if len(words) > 0 && (redefiners[words[0]] || redefiners[v.name]) {
		return true
	}
	if len(c.words) > 0 && c.words[0].text == "function" {
		return true
	}
	for _, a := range v.argv {
		if name, _, ok := strings.Cut(a, "="); ok && name == "PATH" {
			return true
		}
	}
	switch v.name {
	case "export", "declare", "typeset", "local", "readonly":
		for _, a := range v.rest {
			if name, _, _ := strings.Cut(a, "="); name == "PATH" {
				return true
			}
		}
	}
	return false
}

// loopWords are the reserved words that start a loop, or its body, or a
// function definition: bash's, and zsh's repeat and foreach.
var loopWords = map[string]bool{
	"for": true, "while": true, "until": true, "select": true, "do": true, "function": true,
	"repeat": true, "foreach": true,
}

// detachers are the programs that run a command detached from the line, to
// finish after it: daemonizers and job schedulers. setsid is one only with
// -f (--fork), screen with -d and -m, and tmux with -d (tmuxDetached).
var detachers = map[string]bool{
	"disown": true, "daemonize": true, "start-stop-daemon": true, "systemd-run": true,
	"at": true, "batch": true,
}

// unordered reports whether c makes the line's commands run in an order
// other than their place in it, so that a read can run after a tree
// change written later: a loop (after any grouping or reserved words), a
// function definition, alias, eval, source or ., a background job, trap,
// coproc, a process substitution, a detached job (detachers, setsid -f,
// screen -dm, tmux -d), and xargs, find -exec, fd -x, parallel or watch,
// which run a command repeatedly. A pipeline is unordered among its own
// stages only (walker.commands).
func unordered(c simple, v view) bool {
	if c.funcDef || c.background || c.procSub {
		return true
	}
	for _, wd := range c.words {
		if wd.quoted || wd.redirect {
			break
		}
		if loopWords[wd.text] || wd.text == "coproc" {
			return true
		}
		if !keywords[wd.text] {
			break
		}
	}
	under := func(names ...string) bool {
		return slices.ContainsFunc(names, func(n string) bool { return v.name == n || slices.Contains(v.under, n) })
	}
	switch {
	case under("trap", "coproc", "parallel", "watch", "xargs", "alias", "eval", "source", "."):
		return true
	case slices.ContainsFunc(v.under, func(n string) bool { return detachers[n] }) || detachers[v.name]:
		return true
	case under("setsid") && (slices.Contains(v.argv, "-f") || slices.Contains(v.argv, "--fork")):
		return true
	case under("screen") && slices.ContainsFunc(v.argv, func(a string) bool {
		return strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "dD") && strings.Contains(a, "m")
	}):
		return true
	case v.name == "tmux" && tmuxDetached(v.rest):
		return true
	case v.name == "find":
		return len(findExecs(v.rest)) > 0
	case v.name == "fd" || v.name == "fdfind":
		execs, _ := fdArgs(v.rest)
		return len(execs) > 0
	}
	return false
}

// tmuxCommand reads a tmux command line: its subcommand, the shell
// command it runs (new-session, new-window, split-window, respawn-pane and
// run-shell take one as their last argument), and whether it sends keys
// to a pane (send-keys), which runs what the gate cannot follow.
func tmuxCommand(rest []string) (sub, command string, keys bool) {
	i := 0
	for i < len(rest) && strings.HasPrefix(rest[i], "-") {
		i++
	}
	if i >= len(rest) {
		return "", "", false
	}
	sub = rest[i]
	switch sub {
	case "send-keys", "send":
		return sub, "", true
	case "new-session", "new", "new-window", "neww", "split-window", "splitw", "respawn-pane",
		"respawnp", "respawn-window", "respawnw", "run-shell", "run":
		if last := rest[len(rest)-1]; len(rest) > i+1 && !strings.HasPrefix(last, "-") {
			return sub, last, false
		}
	}
	return sub, "", false
}

// tmuxDetached reports whether a tmux command runs its command detached
// (-d), or in a session of its own.
func tmuxDetached(rest []string) bool {
	sub, command, _ := tmuxCommand(rest)
	return command != "" && (sub != "run-shell" && sub != "run" || slices.Contains(rest, "-b")) &&
		slices.ContainsFunc(rest, func(a string) bool {
			return strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "db")
		})
}

// lineEnvUnsafe reports whether a command of the line sets or exports a
// variable git reads that is not known to be harmless (unsafeEnv): a
// command of assignments alone, or export, declare, typeset, local or
// readonly of one (a name alone, exported with its value unknown).
func lineEnvUnsafe(views []view) bool {
	var set []string
	for _, v := range views {
		switch {
		case len(v.argv) > 0 && !slices.ContainsFunc(v.argv, func(a string) bool { return !isAssignment(a) }):
			set = append(set, v.argv...)
		case v.name == "export" || v.name == "declare" || v.name == "typeset" || v.name == "local" || v.name == "readonly":
			for _, a := range v.rest {
				switch {
				case isAssignment(a):
					set = append(set, a)
				case isName(a):
					set = append(set, a+"=\x00")
				}
			}
		}
	}
	return unsafeEnv(set)
}

// viewDir is the directory v runs in, the repository's root when it cannot
// be told.
func (s *setup) viewDir(v view) string {
	if v.cwd.known() {
		return v.cwd.dirs[0]
	}
	if s.repo != nil {
		return s.repo.Root
	}
	return s.cwd
}

// indexChange is indexChangeOf for the git command v.
func (s *setup) indexChange(v view) string {
	if v.name == "gh" || v.name == "hub" {
		// gh pr checkout and the like move HEAD and the index too.
		return s.treeChange(v)
	}
	return s.indexChangeOf(v.argv, s.viewDir(v))
}

// jqFilter is the index in v.words of the filter jq, yq or gojq is given
// as its first operand, -1 when it has none or reads it from a file (-f,
// --from-file): that word is a program, not a path. Options' values are
// not told apart, which can only leave the filter placed.
func jqFilter(v view) int {
	if v.name != "jq" && v.name != "yq" && v.name != "gojq" {
		return -1
	}
	if slices.ContainsFunc(v.rest, func(a string) bool {
		return a == "-f" || a == "--from-file" || strings.HasPrefix(a, "--from-file=")
	}) {
		return -1
	}
	var args []int // the index in v.words of each argument
	for i, xw := range v.words {
		if !xw.redirect {
			args = append(args, i)
		}
	}
	start := len(args) - len(v.rest)
	at := func(j int) int {
		if start+j >= 0 && start+j < len(args) {
			return args[start+j]
		}
		return -1
	}
	for j := 0; j < len(v.rest); j++ {
		a := v.rest[j]
		switch {
		case a == "--":
			return at(j + 1)
		case !strings.HasPrefix(a, "-") || a == "-":
			return at(j)
		case !strings.Contains(a, "="):
			// The values of jq's options are not its filter.
			j += jqOptionValues[a]
		}
	}
	return -1
}

// jqOptionValues are how many words jq's (and gojq's and yq's) options
// that take values take after them.
var jqOptionValues = map[string]int{
	"-L": 1, "--arg": 2, "--argjson": 2, "--slurpfile": 2, "--rawfile": 2, "--indent": 1,
	"--from-file": 1, "-f": 1, "--seq": 0, "--library-path": 1,
}

// jqImports reports whether jq's filter in v imports or includes modules,
// which jq reads from the paths -L names (placed as any word) or from its
// search path: the working directory is then read too.
func jqImports(v view, filter int) bool {
	return filter >= 0 && filter < len(v.words) &&
		(strings.Contains(v.words[filter].text, "import") || strings.Contains(v.words[filter].text, "include"))
}

// asGit is v, a hub command, as the git command hub runs.
func asGit(v view) view {
	v.argv = slices.Clone(v.argv)
	for i, a := range v.argv {
		if progName(a) == "hub" {
			v.argv[i] = "git"
			break
		}
	}
	v.name = "git"
	return v
}

// ghPrintsPatch reports whether v is gh pr diff printing a patch: any but
// --name-only.
func ghPrintsPatch(v view) bool {
	pos := positional(v.rest)
	return v.name == "gh" && ghSub(pos, "pr", "diff") && !slices.Contains(v.rest, "--name-only")
}

// ghSub reports whether gh's positional words hold the command group
// followed by one of subs, after any option values taken for positional
// words (gh -R owner/repo pr diff).
func ghSub(pos []string, group string, subs ...string) bool {
	for i := 0; i+1 < len(pos); i++ {
		if pos[i] == group && slices.Contains(subs, pos[i+1]) {
			return true
		}
	}
	return false
}

// ghTreeChange names the gh or hub command in v that changes the working
// tree (gh pr checkout, gh repo sync, gh repo clone, hub checkout and the
// git commands hub runs that do), "" otherwise.
func (s *setup) ghTreeChange(v view) string {
	pos := positional(v.rest)
	switch {
	case v.name == "gh" && ghSub(pos, "pr", "checkout", "co"):
		return "gh pr checkout"
	case v.name == "gh" && ghSub(pos, "repo", "sync", "clone"):
		return "gh repo sync or clone"
	case v.name == "hub" && len(pos) >= 1:
		// hub wraps git: its git subcommands are judged as git's.
		if change := s.treeChange(view{name: "git", rest: v.rest, cwd: v.cwd}); change != "" {
			return "hub " + pos[0]
		}
	}
	return ""
}

// isTreeChanger reports whether sub is one of git's subcommands that can
// change the working tree (gitTreeChangers).
func isTreeChanger(sub string) bool {
	_, ok := gitTreeChangers[sub]
	return ok
}

// treeChange names the command v runs when it changes the working tree
// with content the gate has not seen: a git command that checks out,
// merges, applies or restores (gitTreeChangers; any git subcommand that is
// not git's own, an alias included, counts), git clone, git worktree add,
// git bundle unbundle, or an archive extracted (tar -x, unzip and the
// like) or a patch applied. "" when it does not.
func (s *setup) treeChange(v view) string {
	switch v.name {
	case "git":
		dir := s.cwd
		if v.cwd.known() {
			dir = v.cwd.dirs[0]
		}
		inv, ok := parseGit(v.rest, dir)
		if !ok {
			return ""
		}
		if !gitBuiltins[inv.sub] {
			return "git"
		}
		if inv.ambiguous && isTreeChanger(inv.sub) {
			// An abbreviated option the gate cannot place could be one
			// that makes it change the tree.
			return "git " + inv.sub
		}
		if s.branchOnly(inv) {
			return ""
		}
		if gitChangesTree(inv.sub, inv.rest) {
			return "git " + inv.sub
		}
	case "gh", "hub":
		return s.ghTreeChange(v)
	case "tar", "bsdtar", "gtar":
		if tarExtracts(v.rest) {
			return v.name + " -x"
		}
	case "unzip", "funzip", "gunzip", "bunzip2", "unxz", "unzstd", "unlzma", "uncompress", "patch", "ar":
		if v.name == "ar" && (len(v.rest) == 0 || !strings.Contains(v.rest[0], "x")) {
			return ""
		}
		if !slices.ContainsFunc(v.rest, func(a string) bool {
			return slices.Contains([]string{"-c", "--stdout", "-l", "--list", "-t", "--test", "--dry-run", "-Z"}, a)
		}) {
			return v.name
		}
	case "7z", "7za", "7zz", "7zr":
		if len(v.rest) > 0 && (v.rest[0] == "x" || v.rest[0] == "e") {
			return v.name + " " + v.rest[0]
		}
	case "cpio":
		if slices.ContainsFunc(v.rest, func(a string) bool {
			return a == "--extract" || len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.Contains(a, "i")
		}) {
			return "cpio -i"
		}
	}
	return ""
}

// branchOnly reports whether a git checkout -b/-B or switch -c/-C only
// creates a branch, leaving the tree as it is: no start point, or one that
// is HEAD's commit, and no paths. An orphan or detached switch is a change.
func (s *setup) branchOnly(inv gitInvocation) bool {
	var create []string
	switch inv.sub {
	case "checkout":
		create = []string{"-b", "-B"}
	case "switch":
		create = []string{"-c", "-C", "--create", "--force-create"}
	default:
		return false
	}
	created := false
	var pos []string
	for i := 0; i < len(inv.rest); i++ {
		a := inv.rest[i]
		switch {
		case a == "--":
			return false
		case slices.Contains(create, a):
			created = true
			i++ // the new branch's name
		case !strings.HasPrefix(a, "-"):
			pos = append(pos, a)
		case a == "--orphan" || a == "--detach" || a == "-p" || a == "--patch" || a == "--merge" || a == "-m":
			return false
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
		case strings.HasPrefix(a, "--"):
		default:
			// A cluster ending in the create flag: -fb NEW.
			if c := a[len(a)-1:]; slices.Contains(create, "-"+c) {
				created = true
				i++
			}
		}
	}
	switch {
	case !created || len(pos) > 1:
		return false
	case len(pos) == 0:
		return true
	}
	start, err := s.git().value(inv.dir, "rev-parse", "--verify", "--quiet", "--end-of-options", pos[0]+"^{commit}")
	if err != nil {
		return false
	}
	head, err := s.git().value(inv.dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	return err == nil && start == head
}

// gitChangesTree reports whether git's subcommand sub, with the words
// after it, changes the working tree.
func gitChangesTree(sub string, rest []string) bool {
	exempt, ok := gitTreeChangers[sub]
	if !ok {
		return false
	}
	first := ""
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			first = a
			break
		}
	}
	switch sub {
	case "reset":
		return slices.ContainsFunc(rest, func(a string) bool { return a == "--hard" || a == "--merge" || a == "--keep" })
	case "read-tree":
		return slices.Contains(rest, "-u")
	case "submodule":
		return first == "update" || first == "add" || first == "foreach"
	case "worktree":
		return first == "add"
	case "bundle":
		return first == "unbundle"
	case "stash", "bisect", "sparse-checkout":
		return !slices.Contains(exempt, first)
	case "restore":
		// --staged alone touches only the index; with --worktree, both.
		staged := slices.ContainsFunc(rest, func(a string) bool { return slices.Contains(exempt, a) })
		return !staged || slices.Contains(rest, "--worktree") || slices.Contains(rest, "-W")
	}
	return !slices.ContainsFunc(rest, func(a string) bool { return slices.Contains(exempt, a) })
}

// tarExtracts reports whether tar's arguments extract: -x, --extract or
// --get, or an x in its first word of bundled letters.
func tarExtracts(rest []string) bool {
	for i, a := range rest {
		switch {
		case a == "--extract" || a == "--get":
			return true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-"):
			if strings.ContainsRune(a, 'x') {
				return true
			}
		case i == 0 && strings.ContainsRune(a, 'x'):
			return true
		}
	}
	return false
}

// silent are commands whose words are never read as paths.
var silent = map[string]bool{
	"echo": true, "printf": true, ":": true, "true": true, "false": true, "exit": true,
	"return": true, "sleep": true,
}

// reads reports whether v, run after a command that changed the working
// tree, may read what that command wrote where it matters: a reading or
// searching program (a reader, a search, find, fd, a shell or eval), or a
// word, placed against v's working directory, that is, is inside or
// contains a protected root, names local state or a file that controls
// the gate, or cannot be placed. A word that only looks like a path
// (./..., src/) does not count, nor does an output redirection's target.
// git's own reads are judged by rule 2.
// pipeFilters are the filters pipeFilter knows, each with the options it
// takes that take a value (as the next word unless attached), and the
// options that take none; any other option, or any operand, and it is no
// filter of its input alone.
var pipeFilters = map[string]struct{ value, flags []string }{
	"head": {[]string{"-n", "-c", "--lines", "--bytes"}, []string{"-q", "-v", "-z", "--quiet", "--silent", "--verbose", "--zero-terminated"}},
	"tail": {[]string{"-n", "-c", "--lines", "--bytes"}, []string{"-q", "-v", "-z", "-f", "-F", "--quiet", "--silent", "--verbose", "--zero-terminated", "--follow"}},
	"wc":   {nil, []string{"-l", "-c", "-w", "-m", "-L", "--lines", "--bytes", "--words", "--chars", "--max-line-length"}},
	"sort": {[]string{"-k", "-t", "--key", "--field-separator"}, []string{"-n", "-r", "-u", "-h", "-V", "-f", "-b", "-g", "-M", "-s", "-z",
		"--numeric-sort", "--reverse", "--unique", "--human-numeric-sort", "--version-sort", "--ignore-case", "--stable"}},
	"uniq": {[]string{"-f", "-s", "-w", "--skip-fields", "--skip-chars", "--check-chars"}, []string{"-c", "-d", "-u", "-i", "-z",
		"--count", "--repeated", "--unique", "--ignore-case"}},
	"cut": {[]string{"-d", "-f", "-c", "-b", "--delimiter", "--fields", "--characters", "--bytes", "--output-delimiter"},
		[]string{"-s", "--complement", "--only-delimited"}},
	"cat": {nil, []string{"-n", "-A", "-b", "-e", "-s", "-t", "-v", "-E", "-T"}},
}

// pipeFilter reports whether v, a command reading a pipe, reads only its
// input: no redirection reads, no wrapper runs it, and it is a filter of
// pipeFilters given no operand and only options from its lists, or a
// search taken to read its input alone (no operand, no option naming a
// file, no configuration: searchScope).
func (w *walker) pipeFilter(v view) bool {
	if len(v.under) > 0 || slices.ContainsFunc(v.words, func(x xword) bool { return x.redirect && x.input }) ||
		slices.Contains(v.restKnown, false) {
		return false
	}
	if _, ok := searchers[v.name]; ok {
		sc := w.searchScope(v)
		return len(sc.operands) == 0 && len(sc.reads) == 0 && sc.widen == "" && sc.everywhere == "" && searchReadsInput(v)
	}
	f, ok := pipeFilters[v.name]
	if !ok {
		return false
	}
	for i := 0; i < len(v.rest); i++ {
		a := v.rest[i]
		name, _, attached := strings.Cut(a, "=")
		switch {
		case a == "-":
		case slices.Contains(f.flags, a):
		case slices.Contains(f.value, name):
			if !attached {
				i++
			}
		case len(a) > 2 && a[0] == '-' && a[1] != '-' && slices.Contains(f.value, a[:2]):
			// -n5, -d:
		case len(a) > 1 && a[0] == '-' && strings.Trim(a[1:], "0123456789") == "" && (v.name == "head" || v.name == "tail"):
			// head -20
		default:
			return false
		}
	}
	return true
}

// searchReadsInput reports whether a search with no operand reads its
// input rather than its working directory: grep, egrep and fgrep do, with
// the options the search rule allows (search.go); rg, ag and the others
// read a pipe too when nothing else is given.
func searchReadsInput(v view) bool {
	return slices.Contains([]string{"grep", "egrep", "fgrep", "rg", "ag", "ugrep", "ug"}, v.name)
}

func (w *walker) reads(v view) bool {
	s := w.s
	if v.name == "cat" && len(v.rest) == 0 && len(v.under) == 0 && v.docOnly && !w.redefines {
		// cat of a here-document alone reads no file; the substitutions
		// an unquoted one runs are commands of their own.
		return false
	}
	if v.pipeIn && !w.redefines && w.pipeFilter(v) {
		// A filter whose only input is the pipe reads no file.
		return false
	}
	if judged(v.name) && v.name != "git" {
		return true
	}
	if v.name == "git" || silent[v.name] {
		return false
	}
	if !v.cwd.known() {
		return true
	}
	for _, xw := range v.words {
		if xw.redirect && !xw.input {
			continue
		}
		abss, unknown := s.wordSpellings(v, xw)
		if unknown && !optionOnly(xw.text) {
			return true
		}
		for _, a := range abss {
			if w.cache.memo("touches", a, s.touchesAbs) || w.cache.control(a, false, w.controls()) != "" {
				return true
			}
		}
		if _, ok := s.namesLocalState(v, xw); ok {
			return true
		}
	}
	return false
}

// controls are the files that control the gate, loaded once a decision
// needs them.
func (w *walker) controls() controls {
	if w.ctl == nil {
		c := loadControls()
		w.ctl = &c
	}
	return *w.ctl
}

// toolAlone reports whether line is one simple command running the tool,
// optionally after environment assignments, with no command substitution,
// and a subcommand on the table of what an agent may run
// (agentSubcommands).
func toolAlone(line string, cmds []simple) bool {
	if len(cmds) != 1 || len(cmds[0].subs) > 0 || strings.Contains(line, "$(") || strings.Contains(line, "`") {
		return false
	}
	args := cmds[0].args()
	for i, w := range args {
		if !isAssignment(w) {
			return w == toolName && toolAloneSubcommand(args[i+1:])
		}
	}
	return false
}

// cwdState is the working directory a command runs in, as far as the
// command line tells.
type cwdState struct {
	// dirs are the spellings of the effective working directory: as cd
	// leaves it logically, and with its symlinks resolved. Nil when it
	// cannot be told.
	dirs []string
	// oldpwd is the previous working directory, "" when unknown.
	oldpwd string
}

func (c cwdState) known() bool { return c.dirs != nil }

func (c cwdState) clone() cwdState { return cwdState{dirs: slices.Clone(c.dirs), oldpwd: c.oldpwd} }

// xword is one word of a command, expanded as far as the gate can.
type xword struct {
	// text is the word with HOME, PWD, OLDPWD and a leading ~ expanded,
	// and any other expansion as written.
	text string
	// known is false when an expansion's value cannot be told.
	known bool
	// pattern is text as a glob: quoted glob characters escaped. glob is
	// true when an unquoted glob character makes the shell expand it.
	pattern  string
	glob     bool
	redirect bool
	// input and here are word's: a redirection that reads, a here-string.
	input, here bool
	// rw is word's: a redirection that reads and writes (<>).
	rw bool
	// fed is set for a word that holds an input word a runner (xargs,
	// parallel) substitutes (markFed): known is false too.
	fed bool
	// message is set for git's message text (gitMessageText), which is
	// not placed as a path.
	message bool
}

// view is one simple command as the rules see it.
type view struct {
	words []xword
	// argv are the expanded arguments, redirection targets left out and
	// leading reserved words stripped; argvKnown says which are known,
	// argvShapes how the shell may make each several words.
	argv       []string
	argvKnown  []bool
	argvShapes []argShape
	// name is the command run, rest its arguments and under the wrappers
	// it runs under (command); restKnown says which of rest are known.
	name      string
	rest      []string
	restKnown []bool
	under     []string
	cwd       cwdState
	pipeIn    bool
	// seq is the order the command runs in, among the line's commands:
	// after the substitutions in its words.
	seq int
	// docOnly is set when its only input is one here-document: no pipe,
	// no other input redirection, no here-string.
	docOnly bool
	// cache holds the decision's expanded words and its glob budget.
	cache *spellCache
}

// changeAt is a git command in the line that changes the repository: its
// seq, its words and its label for a message.
type changeAt struct {
	seq   int
	argv  []string
	label string
}

// walker reads a command line, the lines nested in it, and the working
// directory each command runs in.
type walker struct {
	s     *setup
	views []view
	lines []string
	// reach is set, with the first trigger found, when something read
	// along the way touches every protected root: a line nested too deep
	// or built at run time, or a find or xargs reading under a root.
	reach trigger
	cache *spellCache
	// changed names the first command that changes the working tree
	// (treeChange), "" before one; readAfterChange is set when a command
	// after it reads.
	changed         string
	readAfterChange bool
	// redefines is set when a command of the line may make cat or git run
	// another program (redefines): git's message text is then not left
	// out of the root-name tests. message are the indexes in lines of the
	// lines that are cat reading a here-document that is git's message.
	redefines bool
	// hashed are the names the line made stand for another program (hash
	// -p, alias): a command that runs one runs what the gate cannot tell.
	hashed map[string]bool
	// quotedValue is set once a command holds quoted code that can become
	// a variable's value (valueCode), quotedAny once one holds any quoted
	// code, readsFile once a read or mapfile reads a value from a file,
	// and evaluates once a command makes bash evaluate a string.
	quotedValue, quotedAny, readsFile, evaluates bool
	// posRef is set once a command reads a positional parameter or OPTARG
	// (positionalRef).
	posRef bool
	// funcNames are the functions the line defines, whose calls pass their
	// arguments on as values.
	funcNames map[string]bool
	// shellName is the shell running the line being read when a shell
	// command names it (sh -c, bash -c), "" for the top level, where the
	// Bash tool's shell is not known; xpgEcho is set once the line sets
	// bash's xpg_echo option.
	shellName string
	xpgEcho   bool
	// ph are the placeholders of the runners (xargs, parallel) the command
	// being read runs under (feedOf), which stand for input words in the
	// lines nested in it.
	ph      []string
	message map[int]bool
	// anywhere names a change after which every read in the line counts,
	// wherever it is: one in a process substitution, which starts before
	// or alongside the command it is given to, and any change in a line
	// with a loop or a function, which can run a read again after it.
	// inProc counts the process substitutions being walked, and loopy is
	// set for a line with a loop or a function definition.
	anywhere string
	inProc   int
	loopy    bool
	// changes are the git (and gh and hub) commands not known to be
	// read-only (indexChange), in the order they run.
	changes []changeAt
	seq     int
	// tooMany is set when the line holds more than maxCommands commands.
	tooMany bool
	// tooManyWords is set when brace expansion makes more words than
	// maxBraceLine in a line.
	tooManyWords bool
	// text is the command line decided, as given.
	text string
	// uncertainWhy says why the gate cannot read the line as bash would
	// (setUncertain), and runtimeWhy what in it is code built at run time
	// (setRuntime); "" for none.
	uncertainWhy, runtimeWhy string
	ctl                      *controls
	// probe is set for a walk that judges nothing and only finds the
	// paths the line touches (adopt.go), with a setup that has no
	// repository; probed are the paths find, fd, xargs and parallel read
	// under.
	probe  bool
	probed []string
}

// commands reads the simple commands cmds of line, nested depth deep, from
// the working directory st, which cd changes for the commands after it.
func (w *walker) commands(line string, cmds []simple, st *cwdState, depth int) {
	if depth > maxDepth {
		w.setUncertain(whyTooDeep)
		return
	}
	w.lines = append(w.lines, line)
	var lister *view // a find or fd whose output this pipeline carries
	var prev *simple // the command before, in this pipeline
	// The stages of a pipeline run together: a change in one and a read in
	// another, in any order, is a read after the change.
	// In a line with a group or a subshell, a stage can be a whole group,
	// which the gate does not track: the stages are taken to reach back to
	// the line's start.
	lineStart := len(w.views)
	grouped := strings.ContainsAny(line, "{(")
	pipeStart, stages := len(w.views), 0
	defer func() { w.pipeline(pipeStart, stages) }()
	for i, c := range cmds {
		if !c.pipeIn {
			w.pipeline(pipeStart, stages)
			pipeStart, stages = len(w.views), 0
			if grouped {
				pipeStart = lineStart
			}
		}
		stages++
		if len(w.views) >= maxCommands {
			w.tooMany = true
			return
		}
		if c.overflow {
			w.tooManyWords = true
			return
		}
		if c.uncertain != "" {
			// The tokenizer cannot read the line as bash would: what it
			// runs, and in what order, cannot be told.
			w.setUncertain(c.uncertain)
			w.loopy = true
		}
		if !c.pipeIn {
			prev = nil
		}
		c.pipeOut = i+1 < len(cmds) && cmds[i+1].pipeIn
		v := w.simple(c, st, depth)
		if c.fallback {
			// The line did not tokenize: what it runs cannot be told.
			w.setUncertain("the line has an unbalanced quote or a trailing backslash, or nests substitutions too deeply")
		}
		if c.pipeIn && (shellReadsScript(v) || v.name == "at" || v.name == "batch") {
			w.pipedScript(prev, v, depth)
		}
		if !c.pipeIn {
			lister = nil
		}
		if v.name == "find" || v.name == "fd" || v.name == "fdfind" {
			lister = &v
		} else if lister != nil && feedOf(v.argv, v.argvKnown).runner != "" {
			// find P ... | xargs ... reads every file find prints.
			for _, p := range listRoots(*lister) {
				w.reachesIn(lister.cwd, p, "xargs or parallel reads the files find or fd lists")
			}
		}
		prev = &cmds[i]
	}
}

// pipeline judges the views from start on, the commands of a pipeline of
// stages stages, nested ones included: with more than one stage, a tree
// change in any and a read in any other is a read after the change.
func (w *walker) pipeline(start, stages int) {
	if w.probe || stages < 2 || w.readAfterChange || start >= len(w.views) {
		return
	}
	views := w.views[start:]
	for i, v := range views {
		change := w.s.treeChange(v)
		if change == "" {
			continue
		}
		for j, r := range views {
			if j != i && w.reads(r) {
				w.readAfterChange, w.changed = true, change
				return
			}
		}
	}
}

// line reads a nested command line.
func (w *walker) line(line string, st *cwdState, depth int) {
	if depth > maxDepth {
		w.setUncertain(whyTooDeep)
		return
	}
	w.commands(line, splitCommands(line), st, depth)
}

// programShown reports whether an interpreter reading its program from its
// input (readsProgram) is fed a program the line shows as written: a
// here-document whose body is known (quoted, or with no expansion) or a
// known here-string, and nothing else on its input. Its text is then
// tested for a root's name as any other word of the line.
func programShown(c simple, v view) bool {
	if c.pipeIn {
		return false
	}
	shown := false
	for _, doc := range c.docs {
		if !docKnown(doc) {
			return false
		}
		shown = true
	}
	for _, xw := range v.words {
		switch {
		case xw.here && xw.known:
			shown = true
		case xw.here, xw.redirect && xw.input:
			return false
		}
	}
	return shown
}

// shellReadsScript reports whether v is a shell that reads its script from
// its input: no -c string.
func shellReadsScript(v view) bool {
	return shells[v.name] && shellString(v.rest) < 0
}

// pipedScript reads the script a shell v reads from a pipe, from prev, the
// command before it: prev's here-documents and here-strings, or the
// arguments of an echo or printf. Anything else is built at run time.
func (w *walker) pipedScript(prev *simple, v view, depth int) {
	if prev == nil {
		w.setRuntime(whyPipedScript, hintPlain)
		return
	}
	pv := w.s.viewOf(*prev, v.cwd, w.ph)
	if f := feedOf(pv.argv, pv.argvKnown); f.runner != "" && f.fromInput {
		// A runner adds the words it reads to what it runs, or puts them
		// in its command: what it prints, a script for the shell, is
		// built from input.
		w.setRuntime(whyPipedScript, hintPlain)
		return
	}
	fed := false
	for _, doc := range prev.docs {
		w.nested(doc.body, docKnown(doc), v.cwd.clone(), depth)
		fed = true
	}
	for _, xw := range pv.words {
		if xw.here {
			w.nested(xw.text, xw.known, v.cwd.clone(), depth)
			fed = true
		}
	}
	if !fed && (pv.name == "echo" || pv.name == "printf") {
		// The script is what echo or printf prints, read as any line is
		// (its backslash-newlines joined): printf '%s\n' 'git log \' -p.
		if text, ok := printed(pv.name, pv.rest, w.echoMayEscape()); ok {
			w.nested(text, !slices.Contains(pv.restKnown, false), v.cwd.clone(), depth)
			fed = true
		}
	}
	if !fed {
		w.setRuntime(whyPipedScript, hintPlain)
	}
}

// docKnown reports whether a here-document's body is known as written: a
// quoted one, or one with no expansion in it.
func docKnown(doc *heredoc) bool {
	return doc.quoted || !strings.ContainsAny(doc.body, "$`\\")
}

// reachesIn sets reach when p, from the working directory st, touches a
// protected root, or when st is unknown and p relative. why says what
// reads under p, in fixed words; hint what to do instead.
func (w *walker) reachesIn(st cwdState, p, why string) {
	if w.probe {
		w.probeIn(st, p)
		return
	}
	paths := []string{p}
	if pl, needed, ok := w.s.msys.word(p); !ok {
		w.setReach(why+", at a path the gate cannot place", hintLister)
		return
	} else if needed {
		paths = append(paths, pl)
	}
	for _, p := range paths {
		if !st.known() {
			if !fullPath(p) || w.s.touchesIn("/", p) {
				w.setReach(why+", from a working directory the gate cannot tell", hintLister)
			}
			continue
		}
		for _, d := range st.dirs {
			for _, a := range spellings(d, p) {
				if w.s.touchesAbs(a) {
					w.setReach(why+", in a directory that "+w.s.whereAbs(a), hintLister)
					return
				}
			}
		}
	}
}

// trigger is why a call reaches the protected tree, in fixed words and
// paths printed by displayPath, and one short thing to do instead. The
// zero trigger is none.
type trigger struct{ why, hint string }

func (t trigger) set() bool { return t.why != "" }

// setReach records the first trigger that makes the line reach every
// protected root.
func (w *walker) setReach(why, hint string) {
	if !w.reach.set() {
		w.reach = trigger{why, hint}
	}
}

// setUncertain records that the gate cannot read the line as bash would
// (an unfinished construct, an unbalanced quote, a line nested too deep):
// the call is denied outright. It reaches every root too, for the paths
// adoption takes the line to touch.
func (w *walker) setUncertain(why string) {
	w.setReach(why, hintPlain)
	if w.uncertainWhy == "" {
		w.uncertainWhy = why
	}
}

// setRuntime records that the line runs code built at run time, which
// could run git printing history: the call is denied with git-content.
// It reaches every root too.
func (w *walker) setRuntime(why, hint string) {
	w.setReach(why, hint)
	if w.runtimeWhy == "" {
		w.runtimeWhy = why
	}
}

// The triggers that recur.
const (
	whyTooDeep     = "a command line is nested more than 4 deep, too deep to follow"
	whyPipedScript = "a shell reads its script from a pipe whose content is built at run time"
	// whyPipedProgram is the trigger for a script interpreter reading its
	// program from its input.
	whyPipedProgram = "a script interpreter (python, node, perl and the like) reads its program from its input, " +
		"which is built at run time"
)

// The hints a refusal ends with: one short thing to do instead.
const (
	hintSearch = "Name the files to search explicitly, or exclude the protected folders."
	hintCwd    = "Run it from outside the protected folders; use `pbipx explain` for the structure of what is in them."
	hintNames  = "Leave the protected folders out of the command; use `pbipx explain` for the structure of what is in them."
	hintExpand = "Write the paths out in full, without variables or command substitutions; files in the protected folders stay unreadable while they hold findings."
	hintPlain  = "Write the command out plainly, without building it at run time or nesting it more than 4 deep."
	hintOther  = "Run the command in bash directly, with its paths written out."
	hintTool   = "Run pbipx scan, scan-text, explain, baseline, policy status, version or help, written out in full; other subcommands are not for an agent."
	hintLister = "Point find or fd away from the protected folders, or exclude them."
	hintQuotes = "Balance the quotes in the command."
	// hintSimplify ends a refusal for a line the gate cannot read.
	hintSimplify = "Write the command line plainly, as separate simple commands."
	hintGit      = "Limit the paths (`-- src`) or use --stat."
	// hintGitExpand ends a git refusal for an argument the gate cannot
	// expand.
	hintGitExpand = "Write git's arguments out in full, without variables or command substitutions."
	hintChange    = "Run the change on its own, then the read as a separate call."
	hintSelect    = "Use `pbipx explain` for its structure; to clear the selection, ask a person to clear it in Power BI Desktop and save, or to run `pbipx scan` at a terminal."
	hintValue     = "Use `pbipx explain` for its structure; to clear the value, ask a person to remove it in Power BI Desktop or an editor, or to run `pbipx scan` at a terminal."
	// hintPowerShell ends a refusal for a PowerShell command, which the
	// gate cannot read, whatever the working directory.
	hintPowerShell = "Use the Read tool for a single file, or the Bash tool with the paths written out."
	// hintSettings ends a refusal to change what controls the gate.
	hintSettings = "Show the person the exact change, for them to make at a terminal."
	hintVet      = "Ask a person to cover the file in the policy (.pbipx.toml), or leave it out of the call."
	hintUnread   = "Check that the file can be read, or ask a person to run `pbipx scan` at a terminal."
	hintLocal    = "Leave Power BI Desktop's .pbi folders out of the call; Desktop keeps them on its own."
)

// simple records one simple command run from st, reads what it nests, and
// applies a cd to st.
func (w *walker) simple(c simple, st *cwdState, depth int) view {
	s := w.s
	v := s.viewOf(c, *st, w.ph)
	v.cache = w.cache
	// The lines nested in this command are read with the placeholders of
	// the runners it runs under: they stand for input words there too.
	feed := feedOf(v.argv, v.argvKnown)
	defer func(old []string) { w.ph = old }(w.ph)
	w.ph = slices.Concat(w.ph, feed.placeholders)
	// The shell running the lines nested in a shell command is that
	// shell, whose echo is read as it reads it (echoMayEscape); xpg_echo,
	// set anywhere in the line, makes bash's echo read escapes.
	if shells[v.name] {
		defer func(old string) { w.shellName = old }(w.shellName)
		w.shellName = v.name
	}
	if slices.Contains(v.rest, "xpg_echo") {
		w.xpgEcho = true
	}
	if c.funcDef {
		// A function definition's name is no command: it never runs
		// here (cd() would otherwise move the directory). Its body is
		// read where it stands. A function named like a program or a
		// builtin the gate models runs, where it is called, what the gate
		// cannot tell. The line is unordered: the function runs where it
		// is called.
		w.redefines, w.loopy = true, true
		if len(v.argv) > 0 {
			if w.funcNames == nil {
				w.funcNames = map[string]bool{}
			}
			w.funcNames[v.argv[len(v.argv)-1]] = true
			if name := progName(v.argv[len(v.argv)-1]); modeled(name) {
				if w.hashed == nil {
					w.hashed = map[string]bool{}
				}
				w.hashed[name] = true
			}
		}
		return view{cwd: v.cwd}
	}
	w.views = append(w.views, v)
	idx := len(w.views) - 1
	if redefines(c, v) {
		w.redefines = true
	}
	if why := w.unknownProgram(c, v); why != "" {
		w.setRuntime(why, hintPlain)
	}
	// Quoted code bash evaluates later (F4): quoted $(...) that can become
	// a variable's value, with a construct that evaluates a string, in
	// any order in the line. A value read from a file the line may have
	// written cannot be seen: then any quoted code counts.
	w.quotedValue = w.quotedValue || valueCode(c, v, depth, w.funcNames[v.name])
	w.quotedAny = w.quotedAny || quotedCode(c)
	w.readsFile = w.readsFile || readsValueFromFile(v)
	w.posRef = w.posRef || positionalRef(c)
	w.evaluates = w.evaluates || evaluates(c, v)
	if (w.quotedValue || w.quotedAny && (w.readsFile || w.posRef)) && w.evaluates {
		w.setRuntime("quoted code is evaluated by bash (an array subscript, test -v, printf -v, read, declare -n, "+
			"let, arithmetic or a prompt expansion)", hintPlain)
	}
	if lineSets([]view{v}, promptVars...) {
		w.setRuntime("a variable whose value bash runs later (PS4, PROMPT_COMMAND, BASH_ENV and the like) is set", hintPlain)
	}
	if (v.name == "mapfile" || v.name == "readarray") && hasShort(v.rest, 'C', "--callback", "dnOsuc") {
		w.setRuntime("mapfile or readarray runs a callback, code given as a string", hintPlain)
	}
	if c.procSub {
		// What it runs, nested lines included, is in the process
		// substitution too.
		w.inProc++
		defer func() { w.inProc-- }()
	}
	if unordered(c, v) {
		w.loopy = true
	}

	// The command substitutions in its words and here-documents run
	// before it, each in a subshell in this directory: a read in one is
	// not after this command's own change to the tree, and a change in
	// one comes before this command. A process substitution, <(...) or
	// >(...), runs alongside it and is read as a command after it.
	msg := gitMessageText(c)
	for k, sub := range c.subs {
		sst := v.cwd.clone()
		n := len(w.lines)
		w.line(sub, &sst, depth+1)
		if msg.subs[k] && n < len(w.lines) && w.lines[n] == sub {
			// cat reading a here-document that is git's message: the
			// body is not tested for a root's name (namesRootOutsideListers),
			// unless the line may redefine cat or git; the lines its body
			// runs, walked after it, still are.
			if w.message == nil {
				w.message = map[int]bool{}
			}
			w.message[n] = true
		}
	}
	for _, doc := range c.docs {
		for _, sub := range doc.subs {
			sst := v.cwd.clone()
			w.line(sub, &sst, depth+1)
		}
	}

	// Every command after one that changes the working tree is judged
	// for reading; the first change is the one named. A probe judges
	// nothing.
	if !w.probe && w.changed != "" && !w.readAfterChange && w.reads(v) {
		w.readAfterChange = true
	}
	if !w.probe && (w.changed == "" || w.anywhere == "") {
		if change := s.treeChange(v); change != "" {
			if w.changed == "" {
				w.changed = change
			}
			if w.anywhere == "" && w.inProc > 0 {
				w.anywhere = change
			}
		}
	}
	// git commands run in order: this one after its substitutions.
	w.seq++
	w.views[idx].seq = w.seq
	if !w.probe && (v.name == "git" || v.name == "gh" || v.name == "hub") {
		if change := s.indexChange(v); change != "" {
			w.changes = append(w.changes, changeAt{seq: w.seq, argv: v.argv, label: change})
		}
	}

	known := func() bool { return !slices.Contains(v.restKnown, false) }
	if shells[v.name] {
		if i := shellString(v.rest); i >= 0 {
			w.nested(v.rest[i], v.restKnown[i], v.cwd.clone(), depth)
		} else {
			w.fedScript(c, v, depth)
		}
	}
	if readsProgram(v.name, v.rest) && !programShown(c, v) {
		// python3, node, perl ... reading their program from a pipe, a
		// file or a process substitution run what the gate cannot read.
		w.setRuntime(whyPipedProgram, hintPlain)
	}
	switch v.name {
	case "source", ".":
		// source and . alike. source /dev/stdin reads its script from a
		// here-document; from a pipe, or from a process substitution
		// (source <(...)), the script is built at run time.
		stdin := len(v.rest) > 0 && slices.Contains([]string{"/dev/stdin", "-", "/proc/self/fd/0", "/dev/fd/0"}, v.rest[0])
		switch {
		case c.procArg || len(v.rest) > 0 && strings.HasPrefix(v.rest[0], "/dev/fd/") && !stdin:
			w.setRuntime("source or . reads a script built at run time (a process substitution)", hintPlain)
		case stdin && c.pipeIn:
			w.setRuntime("source or . reads its script from a pipe", hintPlain)
		case stdin:
			w.fedScript(c, v, depth)
		}
	case "script":
		// script -c 'command' runs the command under a terminal.
		// The option may end a cluster (-qc), or carry its value (--command=).
		for i, a := range v.rest {
			cluster := strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "c")
			switch {
			case (a == "-c" || a == "--command" || cluster) && i+1 < len(v.rest):
				w.nested(v.rest[i+1], v.restKnown[i+1], v.cwd.clone(), depth)
			case strings.HasPrefix(a, "--command="):
				w.nested(strings.TrimPrefix(a, "--command="), v.restKnown[i], v.cwd.clone(), depth)
			}
		}
	case "-c", "--command":
		// flock FILE -c 'command'.
		if len(v.under) > 0 && v.under[len(v.under)-1] == "flock" && len(v.rest) > 0 {
			w.nested(v.rest[0], v.restKnown[0], v.cwd.clone(), depth)
		}
	case "alias":
		// alias name='command': the command runs where the name is used.
		for i, a := range v.rest {
			if _, body, ok := strings.Cut(a, "="); ok {
				w.nested(body, v.restKnown[i], v.cwd.clone(), depth)
			}
		}
	case "eval":
		// eval runs in this shell: a cd in it moves the commands after it.
		w.nestedIn(strings.Join(v.rest, " "), known(), st, depth)
	case "trap":
		// trap 'command' SIGNAL...: the command runs later, in this shell.
		i := 0
		for i < len(v.rest) && strings.HasPrefix(v.rest[i], "-") && v.rest[i] != "-" {
			if v.rest[i] == "--" {
				i++
				break
			}
			i++
		}
		if i < len(v.rest) && v.rest[i] != "-" {
			w.nested(v.rest[i], v.restKnown[i], v.cwd.clone(), depth)
		}
	case "watch":
		// watch runs its words, joined, with sh -c.
		w.nested(strings.Join(watchCommand(v.rest), " "), known(), v.cwd.clone(), depth)
	case "parallel":
		for _, line := range parallelLines(v.rest) {
			w.nested(line, known(), v.cwd.clone(), depth)
		}
		if !strings.Contains(strings.Join(v.rest, " "), ":::") {
			// Arguments from the input: like xargs.
			if name, _ := program(parallelTemplate(v.rest)); readers[name] {
				w.reachesIn(v.cwd, ".", "parallel runs a reader on the file names its input carries")
			}
		}
	case "tmux":
		// tmux runs a shell command it is given in a pane; keys sent to a
		// pane run what the gate cannot follow.
		if _, command, keys := tmuxCommand(v.rest); keys {
			w.setRuntime("tmux sends keys to a pane, which runs a command the gate cannot follow", hintOther)
		} else if command != "" {
			w.nested(command, v.restKnown[len(v.restKnown)-1], v.cwd.clone(), depth)
		}
	case "at", "batch":
		// at reads the commands it schedules from its input.
		switch {
		case len(c.docs) > 0 || slices.ContainsFunc(v.words, func(x xword) bool { return x.here }):
			w.fedScript(c, v, depth)
		case slices.ContainsFunc(v.rest, func(a string) bool { return strings.HasPrefix(a, "-f") }),
			slices.ContainsFunc(v.words, func(x xword) bool { return x.redirect && x.input }):
			w.setRuntime("at runs commands from a file the gate does not read", hintOther)
		}
	case toolName:
		// A subcommand off the table of what an agent may run (a word
		// the gate cannot tell, a subcommand not made yet) could print
		// what it reads: it counts as touching every protected root.
		if !toolAloneSubcommand(v.rest) {
			w.setReach("the tool's subcommand is not one an agent may run on its own", hintTool)
		}
	case "pwsh", "powershell", "cmd":
		// No parser for these: a command string, or a script on the
		// input, could read anything.
		if v.pipeIn || len(v.rest) > 0 && !versionOnly(v.rest) {
			w.setRuntime("PowerShell or cmd runs a command the gate cannot read", hintOther)
		}
	case "ssh", "su", "runuser", "machinectl", "sshpass":
		if remoteCommand(v.name, v.rest) || v.pipeIn {
			w.setReach("ssh, su, runuser or machinectl runs a command the gate cannot follow", hintOther)
		}
	case "find":
		for _, exec := range findExecs(v.rest) {
			w.simple(simple{words: literalWords(exec)}, new(v.cwd.clone()), depth+1)
		}
		if findReads(v.rest) {
			for _, p := range findPaths(v.rest) {
				w.reachesIn(v.cwd, p, "find runs a command on, or reads, the files it finds")
			}
		}
	case "fd", "fdfind":
		execs, paths := fdArgs(v.rest)
		for _, exec := range execs {
			w.simple(simple{words: literalWords(exec)}, new(v.cwd.clone()), depth+1)
		}
		if len(execs) > 0 {
			for _, p := range paths {
				w.reachesIn(v.cwd, p, "fd runs a command on the files it finds")
			}
		}
	case "cd", "pushd", "popd", "chdir":
		s.chdir(v, st)
	}
	if line, known, ok := envSplit(v); ok {
		w.nested(line, known, v.cwd.clone(), depth)
	}
	// xargs or parallel running a reader, or any program not known to
	// leave its operands unread (neverReadsOperands), with the input words
	// added to its own, reads files named on its input, from here. (With a
	// placeholder the words are marked, and judged where they are used:
	// markFed.)
	if feed.fromInput && (readers[v.name] || feed.appends && v.name != "" && readsOperands(v.name)) {
		w.reachesIn(v.cwd, ".", feed.runner+" runs a reader on the file names its input carries")
	}
	return v
}

// viewOf is the simple command c, run from st, as the rules see it.
func (s *setup) viewOf(c simple, st cwdState, ph []string) view {
	v := view{cwd: st.clone(), pipeIn: c.pipeIn}
	v.docOnly = len(c.docs) == 1 && !c.pipeIn && !slices.ContainsFunc(c.words, func(wd word) bool { return wd.redirect && wd.input })
	var args []string
	var known []bool
	var shapes []argShape
	msg := gitMessageText(c)
	for i, wd := range c.words {
		xw := s.expandWord(wd, v.cwd)
		xw.message = msg.words[i]
		s.placeCheck(&xw)
		v.words = append(v.words, xw)
		if !xw.redirect {
			args = append(args, xw.text)
			known = append(known, xw.known)
			shapes = append(shapes, argShape{splits: wd.splits, glob: wd.globs(), brace: wd.braceUnknown})
		}
	}
	v.argv = stripKeywords(args)
	v.argvKnown = known[len(known)-len(v.argv):]
	v.argvShapes = shapes[len(shapes)-len(v.argv):]
	v.name, v.rest, v.under = command(v.argv)
	v.restKnown = v.argvKnown[len(v.argvKnown)-len(v.rest):]
	// Words a runner (xargs, parallel) substitutes input words for: those
	// of the runners this command runs under, and of any it is nested in.
	if phs := slices.Concat(ph, feedOf(v.argv, v.argvKnown).placeholders); len(phs) > 0 {
		markFed(&v, known, len(args)-len(v.argv), phs)
	}
	return v
}

// placeCheck places the absolute Git Bash paths in xw (msys.go) and makes
// the word unknown when one cannot be placed: a reader given it then
// touches every protected root, a cd to it leaves the working directory
// unknown, and git given it prints no content. The word's text is never
// changed, so a script in it is still read as written.
func (s *setup) placeCheck(xw *xword) {
	if s.msys == nil || !xw.known || xw.message {
		return
	}
	for _, text := range []string{xw.text, xw.pattern} {
		for _, c := range msysCands(text) {
			if _, _, ok := s.msys.word(c); !ok {
				xw.known = false
				return
			}
		}
	}
}

// spellingsAt are the spellings of the path p against dir, and as placed
// when p is an absolute Git Bash path (msys.go). A path that cannot be
// placed has the first only: where it is a word, placeCheck has made it
// unknown.
func (s *setup) spellingsAt(dir, p string) []string {
	out := spellings(dir, p)
	if pl, needed, ok := s.msys.word(p); needed && ok {
		out = append(out, spellings(dir, pl)...)
	}
	return out
}

// fedScript reads the script a shell reads from a here-document or a
// here-string given to it.
func (w *walker) fedScript(c simple, v view, depth int) {
	for _, doc := range c.docs {
		w.nested(doc.body, docKnown(doc), v.cwd.clone(), depth)
	}
	for _, xw := range v.words {
		if xw.here {
			w.nested(xw.text, xw.known, v.cwd.clone(), depth)
		}
	}
}

// versionOnly reports whether a command's arguments only ask for its
// version or help.
func versionOnly(rest []string) bool {
	for _, a := range rest {
		switch strings.ToLower(strings.TrimLeft(a, "-/")) {
		case "v", "version", "h", "help", "?":
		default:
			return false
		}
	}
	return true
}

// remoteCommand reports whether ssh, su or the like is given a command to
// run: ssh's words after the host, su's -c.
func remoteCommand(name string, rest []string) bool {
	switch name {
	case "ssh":
		const withValue = "BbcDEeFIiJLlmOopQRSWw"
		host := false
		for i := 0; i < len(rest); i++ {
			a := rest[i]
			switch {
			case host:
				return true
			case a == "--":
				host = i+1 < len(rest)
				i++
			case strings.HasPrefix(a, "-") && len(a) > 1:
				if len(a) == 2 && strings.IndexByte(withValue, a[1]) >= 0 {
					i++
				}
			default:
				host = true
			}
		}
		return false
	case "sshpass", "runuser":
		return len(rest) > 0
	}
	for _, a := range rest {
		if a == "--command" || a == "--session-command" || strings.HasPrefix(a, "--command=") ||
			strings.HasPrefix(a, "--session-command=") ||
			len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'c') {
			return true
		}
		if name == "machinectl" && (a == "shell" || a == "run") {
			return true
		}
	}
	return false
}

// watchCommand is the command watch runs: its words after its options.
func watchCommand(rest []string) []string {
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--":
			return rest[i+1:]
		case a == "-n" || a == "--interval" || a == "-q" || a == "--equexit" || a == "--errexit-cmd":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return rest[i:]
		}
	}
	return nil
}

// parallelTemplate is the command GNU parallel runs for each argument: its
// words after its options, up to the first ":::" or "::::".
func parallelTemplate(rest []string) []string {
	withValue := []string{
		"-j", "--jobs", "-a", "--arg-file", "-d", "--delimiter", "-I", "-S", "--sshlogin", "-N",
		"-n", "--max-args", "-E", "--colsep", "-C", "--results", "--joblog", "--tmpdir", "--timeout",
		"--halt", "--memfree", "--load", "--retries", "--delay", "--tag-string", "--workdir", "--wd",
	}
	i := 0
	for i < len(rest) && strings.HasPrefix(rest[i], "-") && !strings.HasPrefix(rest[i], ":::") {
		if slices.Contains(withValue, rest[i]) {
			i++
		}
		i++
	}
	end := i
	for end < len(rest) && !strings.HasPrefix(rest[end], ":::") {
		end++
	}
	return rest[min(i, end):end]
}

// parallelLines are the command lines GNU parallel runs: its template with
// each argument after ":::" (or "::::") appended, or each argument alone
// when there is no template.
func parallelLines(rest []string) []string {
	tmpl := strings.Join(parallelTemplate(rest), " ")
	var args []string
	inArgs := false
	for _, a := range rest {
		if strings.HasPrefix(a, ":::") {
			inArgs = true
			continue
		}
		if inArgs {
			args = append(args, a)
		}
	}
	if len(args) == 0 {
		if tmpl == "" {
			return nil
		}
		return []string{tmpl}
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, strings.TrimSpace(tmpl+" "+a))
	}
	return out
}

// envSplit is the command line env -S (--split-string) runs: its string,
// then the words after it.
func envSplit(v view) (line string, known, ok bool) {
	i := slices.Index(v.under, "env")
	if i < 0 {
		return "", false, false
	}
	for j := 0; j < len(v.argv); j++ {
		if progName(v.argv[j]) != "env" {
			continue
		}
		for k := j + 1; k < len(v.argv); k++ {
			a := v.argv[k]
			var s string
			next := k
			switch {
			case a == "-S" || a == "--split-string":
				if k+1 >= len(v.argv) {
					return "", false, false
				}
				next = k + 1
				s = v.argv[next]
			case strings.HasPrefix(a, "--split-string="):
				s = strings.TrimPrefix(a, "--split-string=")
			case strings.HasPrefix(a, "-S"):
				s = a[2:]
			case strings.HasPrefix(a, "-") || isAssignment(a):
				continue
			default:
				return "", false, false
			}
			known := !slices.Contains(v.argvKnown[k:], false)
			return strings.Join(append([]string{s}, v.argv[next+1:]...), " "), known, true
		}
		return "", false, false
	}
	return "", false, false
}

// listRoots are the paths a find or fd lists under.
func listRoots(v view) []string {
	if v.name == "find" {
		return findPaths(v.rest)
	}
	_, paths := fdArgs(v.rest)
	return paths
}

// fdArgs reads fd's arguments: the commands -x/--exec and -X/--exec-batch
// run (each up to ";" or the end), and the paths it searches (positional
// arguments after the pattern, --search-path, under --base-directory),
// "." when there are none.
func fdArgs(rest []string) (execs [][]string, paths []string) {
	withValue := map[string]bool{
		"-d": true, "--max-depth": true, "--min-depth": true, "--exact-depth": true, "-E": true,
		"--exclude": true, "-e": true, "--extension": true, "-t": true, "--type": true, "-c": true,
		"--color": true, "-j": true, "--threads": true, "-S": true, "--size": true,
		"--changed-within": true, "--changed-before": true, "--change-newer-than": true,
		"--change-older-than": true, "--newer": true, "--older": true, "-o": true, "--owner": true,
		"--path-separator": true, "--max-results": true, "--ignore-file": true, "--format": true,
		"--and": true, "--batch-size": true, "--max-buffer-time": true,
	}
	base := ""
	var pos []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		opt, value, hasValue := strings.Cut(a, "=")
		switch {
		case a == "-x" || a == "--exec" || a == "-X" || a == "--exec-batch":
			j := i + 1
			for j < len(rest) && rest[j] != ";" {
				j++
			}
			execs = append(execs, rest[i+1:j])
			i = j
		case a == "--":
			pos = append(pos, rest[i+1:]...)
			i = len(rest)
		case opt == "--base-directory" || opt == "--search-path":
			if !hasValue && i+1 < len(rest) {
				i++
				value = rest[i]
			}
			if opt == "--base-directory" {
				base = value
			} else {
				paths = append(paths, value)
			}
		case withValue[a]:
			i++
		case strings.HasPrefix(a, "-") && len(a) > 1:
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) > 1 {
		paths = append(paths, pos[1:]...)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if base != "" {
		for i, p := range paths {
			if !isAbs(p) {
				paths[i] = filepath.Join(base, p)
			}
		}
	}
	return execs, paths
}

// nested reads a command line passed as a word, in a working directory of
// its own; nestedIn reads it in st.
func (w *walker) nested(line string, known bool, st cwdState, depth int) {
	w.nestedIn(line, known, &st, depth)
}

func (w *walker) nestedIn(line string, known bool, st *cwdState, depth int) {
	if !known {
		// Built at run time: what it runs cannot be told.
		w.setRuntime("a nested command line is built at run time (an expansion in sh -c, eval or the like)", hintPlain)
	}
	w.line(line, st, depth+1)
}

// literalWords makes words of texts already expanded: quoted, so no glob.
func literalWords(texts []string) []word {
	out := make([]word, len(texts))
	for i, t := range texts {
		out[i] = word{text: t, parts: []part{{kind: litPart, text: t, quoted: true}}, quoted: true}
	}
	return out
}

// shellString returns the index in a shell's arguments of the command
// string -c gives it, or -1.
func shellString(rest []string) int {
	c := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--" || a == "-":
			// The end of the options: with -c, the string follows.
			if c && i+1 < len(rest) {
				return i + 1
			}
			return -1
		case a == "-o" || a == "+o" || a == "-O" || a == "+O":
			i++
		case a == "--command":
			c = true
		case a == "--rcfile" || a == "--init-file":
			i++
		case strings.HasPrefix(a, "--"):
		case len(a) > 1 && (a[0] == '-' || a[0] == '+'):
			c = c || strings.Contains(a[1:], "c")
		default:
			if c {
				return i
			}
			return -1
		}
	}
	return -1
}

// findPaths are the paths find starts from: the arguments before its
// expression, after its own options; "." when there are none.
func findPaths(rest []string) []string {
	i := 0
	for i < len(rest) {
		a := rest[i]
		switch {
		case a == "-H" || a == "-L" || a == "-P" || strings.HasPrefix(a, "-O"):
			i++
			continue
		case a == "-D" || a == "-f":
			i += 2
			continue
		}
		break
	}
	var paths []string
	for ; i < len(rest); i++ {
		a := rest[i]
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" || a == ")" || a == "," {
			break
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return paths
}

// findExecActions are find's actions that run a command on each file.
var findExecActions = []string{"-exec", "-execdir", "-ok", "-okdir"}

// findExecs are the commands find's -exec actions run, each up to its ";"
// or "+".
func findExecs(rest []string) [][]string {
	var out [][]string
	for i := 0; i < len(rest); i++ {
		if !slices.Contains(findExecActions, rest[i]) {
			continue
		}
		j := i + 1
		for j < len(rest) && rest[j] != ";" && (rest[j] != "+" || rest[j-1] != "{}") {
			j++
		}
		out = append(out, rest[i+1:j])
		i = j
	}
	return out
}

// findReads reports whether find runs a command on the files it finds.
func findReads(rest []string) bool {
	return slices.ContainsFunc(rest, func(a string) bool { return slices.Contains(findExecActions, a) })
}

// chdir applies cd, pushd or popd, as v runs it, to st. A target that
// cannot be told leaves the working directory unknown.
func (s *setup) chdir(v view, st *cwdState) {
	unknown := func() { *st = cwdState{} }
	if v.name == "popd" {
		unknown()
		return
	}
	args, known := v.rest, v.restKnown
	i := 0
	for i < len(args) && len(args[i]) > 1 && strings.HasPrefix(args[i], "-") {
		if args[i] == "--" {
			i++
			break
		}
		if v.name != "cd" && v.name != "chdir" {
			// pushd -n, -N: a stack this gate does not keep.
			unknown()
			return
		}
		i++
	}
	var target string
	switch {
	case i >= len(args) && v.name == "pushd":
		unknown()
		return
	case i >= len(args):
		target = s.home
	case !known[i]:
		unknown()
		return
	case args[i] == "-":
		target = st.oldpwd
	case strings.HasPrefix(args[i], "+") && v.name == "pushd":
		unknown()
		return
	case targetGlobs(v, i):
		// The shell expands an unquoted glob character to a directory the
		// gate cannot tell, as it does an expansion.
		unknown()
		return
	default:
		target = args[i]
		if os.Getenv("CDPATH") != "" && !isAbs(target) && !dotRelative(target) {
			// CDPATH can take a bare name anywhere.
			unknown()
			return
		}
	}
	if target == "" || (!st.known() && !fullPath(target)) {
		unknown()
		return
	}
	// An absolute Git Bash path (/tmp/x) is a directory as written and the
	// one Git Bash puts it in; one that cannot be placed leaves the
	// working directory unknown.
	targets := []string{target}
	if pl, needed, ok := s.msys.word(target); !ok {
		unknown()
		return
	} else if needed {
		targets = append(targets, pl)
	}
	var dirs []string
	for _, tg := range targets {
		bases := st.dirs
		if fullPath(tg) {
			bases = []string{string(filepath.Separator)}
		}
		for _, d := range bases {
			// cd is logical by default: ".." is applied to the path as
			// written. The physical directory is judged as well.
			for _, nd := range []string{abs(d, tg), resolvePath(rawJoin(d, tg))} {
				if !slices.Contains(dirs, nd) {
					dirs = append(dirs, nd)
				}
			}
		}
	}
	old := ""
	if st.known() {
		old = st.dirs[0]
	}
	*st = cwdState{dirs: dirs, oldpwd: old}
}

// dotRelative reports whether p starts with "." or "..", which CDPATH
// does not search.
func dotRelative(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

// expandWord expands w as far as the gate can, for a command run from st:
// HOME (the gate's own), PWD and OLDPWD (as the line leaves them), a
// leading ~, and the two variables that name what controls the gate
// (controlVar). Any other expansion leaves it unknown.
func (s *setup) expandWord(w word, st cwdState) xword {
	xw := xword{known: true, redirect: w.redirect, input: w.input, here: w.here, rw: w.rw}
	var text, pattern strings.Builder
	quotedGlob := false
	add := func(t string, quoted bool) {
		text.WriteString(t)
		if quoted || !hasGlob(t) {
			pattern.WriteString(escapeGlob(t))
			quotedGlob = quotedGlob || hasGlob(t)
			return
		}
		xw.glob = true
		pattern.WriteString(t)
	}
	value := func(name string) (string, bool) {
		switch name {
		case "HOME":
			return s.home, s.home != ""
		case "PWD":
			if st.known() {
				return st.dirs[0], true
			}
		case "OLDPWD":
			return st.oldpwd, st.oldpwd != ""
		case approval.EnvConfigDir, claudeConfigDirEnv:
			if s.ctlVars {
				return controlVar(name)
			}
		}
		return "", false
	}
	for _, p := range w.parts {
		switch p.kind {
		case litPart:
			add(p.text, p.quoted)
		case paramPart:
			if v, ok := value(p.text); ok {
				add(v, true)
				continue
			}
			xw.known = false
			add(p.raw, true)
		case tildePart:
			if p.text == "" && s.home != "" {
				add(s.home, true)
				continue
			}
			xw.known = false
			add(p.raw, true)
		default:
			xw.known = false
			add(p.raw, true)
		}
	}
	xw.text, xw.pattern = text.String(), pattern.String()
	if w.braceUnknown {
		// Brace expansion makes it several words, which the gate does
		// not expand.
		xw.known = false
	}
	if quotedGlob && filepath.Separator == '\\' {
		// Windows has no glob escape: a quoted glob character would match.
		xw.glob = false
	}
	return xw
}

// escapeGlob escapes glob characters for filepath.Glob. Windows has no
// escape, so there a quoted glob character is left as is.
func escapeGlob(t string) string {
	if filepath.Separator == '\\' || !strings.ContainsAny(t, `*?[\`) {
		return t
	}
	var b strings.Builder
	for _, r := range t {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// maxGlobEntries bounds the work every glob of one decision does, all
// together: each directory entry read or walked costs one, each match
// globMatchCost. A glob past it touches every protected root, and so does
// every glob after it.
const maxGlobEntries = 200000

// globMatchCost is what one match costs against maxGlobEntries: each is
// resolved, symlinks and all, for every rule.
const globMatchCost = 10

// maxGlobSegments bounds a glob's segments after its literal prefix; a
// longer one touches every protected root.
const maxGlobSegments = 64

// spellCache holds one decision's expanded words, so each is placed and
// each glob expanded once, whichever rule asks, and what is left of the
// decision's glob budget.
type spellCache struct {
	m      map[string]spelled
	ctl    map[string]string
	paths  map[string]string
	budget int
}

type spelled struct {
	out     []string
	unknown bool
}

func newSpellCache() *spellCache {
	return &spellCache{m: map[string]spelled{}, ctl: map[string]string{}, paths: map[string]string{}, budget: maxGlobEntries}
}

// wordSpellings are the absolute paths an expanded word can stand for in
// v: as written against each spelling of the working directory, the value
// of a --flag=value, what an unquoted glob matches, and for a word with
// "**" everything under its literal prefix (the prefix itself stands for
// that: "is inside or contains" judges it). unknown is true when the word
// cannot be placed: an expansion the gate cannot tell, or a relative path
// from an unknown working directory. A glob past the decision's budget
// stands for the repository root, which touches every protected root.
func (s *setup) wordSpellings(v view, xw xword) (out []string, unknown bool) {
	c := v.cache
	if c == nil {
		c = newSpellCache()
	}
	key := strings.Join([]string{
		strings.Join(v.cwd.dirs, "\x00"), strconv.FormatBool(v.cwd.known()), xw.text, xw.pattern,
		strconv.FormatBool(xw.known), strconv.FormatBool(xw.glob),
	}, "\x01")
	if r, ok := c.m[key]; ok {
		return r.out, r.unknown
	}
	out, unknown = s.spellWord(v, xw, c)
	c.m[key] = spelled{out, unknown}
	return out, unknown
}

func (s *setup) spellWord(v view, xw xword, c *spellCache) (out []string, unknown bool) {
	if !xw.known {
		return nil, true
	}
	cands := []string{xw.text}
	if _, val, ok := strings.Cut(xw.text, "="); ok {
		cands = append(cands, expandHome(val, s.home))
	}
	if xw.glob && strings.Contains(xw.pattern, "**") {
		cands = append(cands, globPrefix(xw.pattern))
	}
	dirs := v.cwd.dirs
	if !v.cwd.known() {
		dirs = []string{string(filepath.Separator)}
	}
	for _, cand := range cands {
		if cand == "" {
			continue
		}
		if !v.cwd.known() && !fullPath(cand) {
			unknown = true
			continue
		}
		for _, d := range dirs {
			out = append(out, spellings(d, cand)...)
		}
		// An absolute Git Bash path is also the place Git Bash puts it.
		if pl, needed, ok := s.msys.word(cand); !ok {
			unknown = true
		} else if needed {
			for _, d := range dirs {
				out = append(out, spellings(d, pl)...)
			}
		}
	}
	if xw.glob {
		pats := []string{xw.pattern}
		if pl, needed, ok := s.msys.word(xw.pattern); !ok {
			unknown = true
		} else if needed {
			pats = append(pats, pl)
		}
	globs:
		for _, d := range dirs {
			for _, pattern := range pats {
				if !v.cwd.known() && !fullPath(pattern) {
					continue
				}
				pat := rawJoin(d, pattern)
				matches, over := globMatches(pat, c)
				if over && s.repo == nil {
					// A probe (adopt.go): the glob could stand for anything.
					unknown = true
					break globs
				}
				if over {
					out = append(out, s.repo.Root)
					break globs
				}
				for _, m := range matches {
					out = append(out, spellings(d, m)...)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), unknown
}

// globPrefix is the directory a glob pattern starts in: the text before
// its first unescaped glob character, up to the last separator, unescaped.
func globPrefix(pattern string) string {
	var b strings.Builder
	lastSep := -1
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && filepath.Separator != '\\' && i+1 < len(pattern) {
			i++
			b.WriteByte(pattern[i])
			continue
		}
		if strings.IndexByte("*?[", c) >= 0 {
			break
		}
		if c == '/' || c == filepath.Separator {
			lastSep = b.Len()
		}
		b.WriteByte(c)
	}
	prefix := b.String()
	switch {
	case lastSep < 0:
		return "."
	case lastSep == 0:
		return prefix[:1]
	}
	return prefix[:lastSep]
}

// globMeta reports whether a pattern segment has an unescaped glob
// character.
func globMeta(seg string) bool {
	for i := 0; i < len(seg); i++ {
		switch {
		case seg[i] == '\\' && filepath.Separator != '\\':
			i++
		case strings.IndexByte("*?[", seg[i]) >= 0:
			return true
		}
	}
	return false
}

// unescapeGlob removes a literal segment's escapes.
func unescapeGlob(seg string) string {
	if filepath.Separator == '\\' || !strings.Contains(seg, `\`) {
		return seg
	}
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		if seg[i] == '\\' && i+1 < len(seg) {
			i++
		}
		b.WriteByte(seg[i])
	}
	return b.String()
}

// globMatches is what the absolute pattern matches, as a shell expands it,
// with "**" as a whole segment matching any number of directories (zsh,
// and bash with globstar); a "**" inside a segment is "*". Its work is
// charged to c's budget. over is true when the budget runs out or the
// pattern has more than maxGlobSegments segments after its literal
// prefix: what it matches is then not told.
func globMatches(pattern string, c *spellCache) (matches []string, over bool) {
	vol := filepath.VolumeName(pattern)
	sep := string(filepath.Separator)
	base := vol + sep
	segs := segments(pattern[len(vol):])
	for len(segs) > 0 && !globMeta(segs[0]) {
		base = strings.TrimSuffix(base, sep) + sep + unescapeGlob(segs[0])
		segs = segs[1:]
	}
	if len(segs) > maxGlobSegments || c.budget <= 0 {
		return nil, true
	}
	if slices.Contains(segs, "**") {
		return globStar(base, segs, c)
	}
	cur := []string{base}
	for _, seg := range segs {
		var next []string
		for _, dir := range cur {
			if !globMeta(seg) {
				c.budget--
				p := strings.TrimSuffix(dir, sep) + sep + unescapeGlob(seg)
				if _, err := os.Lstat(p); err == nil {
					next = append(next, p)
				}
				continue
			}
			entries, err := os.ReadDir(dir)
			c.budget -= len(entries) + 1
			if c.budget < 0 {
				return nil, true
			}
			if err != nil {
				continue
			}
			for _, e := range entries {
				if ok, err := filepath.Match(seg, e.Name()); err == nil && ok {
					next = append(next, strings.TrimSuffix(dir, sep)+sep+e.Name())
				}
			}
		}
		c.budget -= globMatchCost * len(next)
		if c.budget < 0 {
			return nil, true
		}
		cur = next
	}
	return cur, false
}

// globStar is globMatches for a pattern with a "**" segment: it walks
// everything under base, without following symlinks.
func globStar(base string, segs []string, c *spellCache) (out []string, over bool) {
	// Consecutive "**" segments match what one does.
	var pats []string
	for _, p := range segs {
		if p == "**" && len(pats) > 0 && pats[len(pats)-1] == "**" {
			continue
		}
		pats = append(pats, p)
	}
	_ = filepath.WalkDir(base, func(p string, _ fs.DirEntry, err error) error {
		c.budget--
		if c.budget < 0 {
			over = true
			return filepath.SkipAll
		}
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry matches nothing under it
		}
		r, rerr := filepath.Rel(base, p)
		if rerr != nil || r == "." {
			return nil //nolint:nilerr // a path that cannot be placed matches nothing
		}
		if matchStar(pats, strings.Split(filepath.ToSlash(r), "/")) {
			c.budget -= globMatchCost
			out = append(out, p)
		}
		return nil
	})
	if over {
		return nil, true
	}
	return out, false
}

// matchStar matches glob segments against path segments; a "**" segment
// matches any number of them. It takes time proportional to the product
// of the two lengths.
func matchStar(pats, segs []string) bool {
	// cur[j] is whether the patterns so far match segs[:j].
	cur := make([]bool, len(segs)+1)
	next := make([]bool, len(segs)+1)
	cur[0] = true
	for _, p := range pats {
		clear(next)
		if p == "**" {
			seen := false
			for j := range cur {
				seen = seen || cur[j]
				next[j] = seen
			}
		} else {
			pp := strings.ReplaceAll(p, "**", "*")
			for j := 1; j < len(cur); j++ {
				if cur[j-1] {
					ok, err := path.Match(pp, segs[j-1])
					next[j] = err == nil && ok
				}
			}
		}
		cur, next = next, cur
	}
	return cur[len(segs)]
}

// viewNamesAgentSettings reports whether a word of v names a Claude Code
// settings file in the repository, under any spelling. No path is returned
// for a message: a file that does not exist yet is named only by the
// command's text.
func (s *setup) viewNamesAgentSettings(v view) bool {
	c := v.cache
	if c == nil {
		c = newSpellCache()
	}
	settings := func(a string) bool {
		return slices.ContainsFunc(s.relsOf([]string{a}), scan.IsAgentSettings)
	}
	writable := mayWriteAll(v)
	for i, xw := range v.words {
		if !writable[i] {
			continue
		}
		abss, _ := s.wordSpellings(v, xw)
		for _, a := range abss {
			if c.memo("settings", a, settings) {
				return true
			}
		}
	}
	return false
}

// mayWriteAll is mayWrite for each word of v, in order, in time linear in
// their number: a command line can hold a great many words.
func mayWriteAll(v view) []bool {
	out := make([]bool, len(v.words))
	ro := readOnly(v)
	nargs := 0
	for _, w := range v.words {
		if !w.redirect {
			nargs++
		}
	}
	lead := nargs - len(v.rest)
	leading := map[xword]bool{}
	k := 0
	for _, w := range v.words {
		if !w.redirect {
			if k < lead {
				leading[w] = true
			}
			k++
		}
	}
	for i, w := range v.words {
		out[i] = w.redirect || strings.HasPrefix(w.text, "-") || !ro || lead < 0 || leading[w]
	}
	return out
}

// writesGitDir reports whether v could write a path inside a git
// directory: one with a .git component, or inside this repository's
// common git directory, where git keeps its configuration. A word could
// be written as for the files that control the gate (mayWriteAll, and the
// destinations of cp, mv, tee and the like). git's own commands are left
// to rule 2 (git config).
func (s *setup) writesGitDir(v view) bool {
	if v.name == "git" {
		return false
	}
	common := s.commonDir()
	writable := mayWriteAll(v)
	var words []xword
	for i, xw := range v.words {
		if writable[i] && !(xw.redirect && (xw.here || xw.input && !xw.rw)) {
			// An input redirection and a here-string write nothing.
			words = append(words, xw)
		}
	}
	dests, _ := s.destinations(v)
	for _, xw := range dests {
		if _, unknown := s.wordSpellings(v, xw); unknown {
			// A write whose target cannot be told could be a config file.
			return true
		}
	}
	users := s.userGitConfigs()
	for _, xw := range append(words, dests...) {
		abss, _ := s.wordSpellings(v, xw)
		for _, a := range abss {
			if slices.Contains(strings.Split(filepath.ToSlash(a), "/"), ".git") || gitConfigName(a, s.fold) {
				return true
			}
			if _, ok := relTo(common, a, s.fold); common != "" && ok {
				return true
			}
			if slices.ContainsFunc(users, func(u string) bool { return samePath(a, u, s.fold) }) {
				return true
			}
		}
	}
	return false
}

// userGitConfigs are the configuration files git reads outside the
// repository: the user's (~/.gitconfig, $XDG_CONFIG_HOME/git/config,
// ~/.config/git/config), the system's (/etc/gitconfig), and those
// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM name.
func (s *setup) userGitConfigs() []string {
	var out []string
	if s.home != "" {
		out = append(out, filepath.Join(s.home, ".gitconfig"), filepath.Join(s.home, ".config", "git", "config"))
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		out = append(out, filepath.Join(x, "git", "config"))
	}
	out = append(out, filepath.FromSlash("/etc/gitconfig"))
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if p := os.Getenv(name); p != "" {
			out = append(out, p)
		}
	}
	if s.repo != nil {
		out = append(out, s.git().configFiles(s.repo.Root)...)
	}
	return out
}

// gitConfigName reports whether the path a names a git configuration
// file by its name, wherever it is: a file named gitconfig or .gitconfig,
// or one named config in a directory named git or .git.
func gitConfigName(a string, fold bool) bool {
	parts := strings.Split(filepath.ToSlash(a), "/")
	eq := func(x, y string) bool { return x == y || fold && strings.EqualFold(x, y) }
	base := parts[len(parts)-1]
	if eq(base, "gitconfig") || eq(base, ".gitconfig") {
		return true
	}
	return len(parts) >= 2 && eq(base, "config") && (eq(parts[len(parts)-2], "git") || eq(parts[len(parts)-2], ".git"))
}

// commonDir is this repository's common git directory, "" when git cannot
// tell it.
func (s *setup) commonDir() string {
	if s.repo == nil {
		return ""
	}
	common, err := s.git().commonDir(s.repo.Root) // asked once a decision
	if err != nil {
		return ""
	}
	return common
}

// lineSets reports whether a command of the line sets or exports one of
// names: an assignment before a command or alone, after reserved words
// (time PS4=...) or under a wrapper (env BASH_ENV=...), or an export,
// declare, typeset, local or readonly of it.
func lineSets(views []view, names ...string) bool {
	for _, v := range views {
		// The words before the program's: an assignment alone has none.
		before := v.argv
		if v.name != "" {
			before = v.argv[:max(0, len(v.argv)-len(v.rest)-1)]
		}
		for _, a := range before {
			name, _, ok := strings.Cut(a, "=")
			if ok && isName(name) && slices.Contains(names, name) {
				return true
			}
		}
		switch v.name {
		case "export", "declare", "typeset", "local", "readonly":
			for _, a := range v.rest {
				if name, _, _ := strings.Cut(a, "="); slices.Contains(names, name) {
					return true
				}
			}
		}
	}
	return false
}

// reachesControl reports whether v names a path outside the repository
// that controls the gate (controls.target) in any word, or copies or
// writes into a user-level Claude Code settings directory. It returns what
// the path is, in words.
func (s *setup) reachesControl(v view, ctl controls) (string, bool) {
	c := v.cache
	if c == nil {
		c = newSpellCache()
	}
	writable := mayWriteAll(v)
	for i, xw := range v.words {
		if !writable[i] {
			continue
		}
		abss, _ := s.wordSpellings(v, xw)
		for _, a := range abss {
			if what := c.control(a, false, ctl); what != "" {
				return what, true
			}
		}
	}
	dests, _ := s.destinations(v)
	for _, d := range dests {
		abss, _ := s.wordSpellings(v, d)
		for _, a := range abss {
			if what := c.control(a, true, ctl); what != "" {
				return what, true
			}
		}
	}
	return "", false
}

// control is ctl.target, or with dir ctl.dir, for one absolute path,
// remembered for the decision.
func (c *spellCache) control(a string, dir bool, ctl controls) string {
	key := strconv.FormatBool(dir) + a
	if what, ok := c.ctl[key]; ok {
		return what
	}
	var what string
	if dir {
		what, _ = ctl.dir([]string{a})
	} else {
		what, _ = ctl.target([]string{a})
	}
	c.ctl[key] = what
	return what
}

// copiers write into their last argument, or into -t's.
var copiers = map[string]string{
	"cp": "S", "mv": "S", "ln": "S", "install": "mogS", "rsync": "efB",
}

// intoAgentDir reports whether v writes into a .claude directory inside
// the repository: as a redirection's target, the destination of cp, mv,
// ln, install or rsync (or a .claude directory copied into the
// repository), a file tee writes or dd's of=. A settings file can be made
// there under any name the command does not show.
func (s *setup) intoAgentDir(v view) bool {
	dests, sources := s.destinations(v)
	intoRepo := false
	for _, d := range dests {
		abss, _ := s.wordSpellings(v, d)
		for _, a := range abss {
			rel, ok := s.rel(a)
			if !ok {
				continue
			}
			intoRepo = true
			if isAgentDir(rel) {
				return true
			}
		}
	}
	if !intoRepo {
		return false
	}
	for _, src := range sources {
		if strings.EqualFold(path.Base(filepath.ToSlash(strings.TrimRight(src.text, `/\`))), ".claude") {
			return true
		}
	}
	return false
}

// destinations are the words v writes into: the targets of its output
// redirections and of <> (not of an input redirection or a here-string),
// the destination of cp, mv, ln, install or rsync, the files tee writes
// and dd's of=. sources are what a copier copies.
func (s *setup) destinations(v view) (dests, sources []xword) {
	for _, xw := range v.words {
		if xw.redirect && !xw.here && (!xw.input || xw.rw) {
			// An output redirection, or <>: an input one and a here-string
			// write nothing.
			dests = append(dests, xw)
		}
	}
	rest := s.restWords(v)
	switch withValue, copier := copiers[v.name]; {
	case copier:
		var pos []xword
		for i := 0; i < len(rest); i++ {
			a := rest[i].text
			switch {
			case a == "-t" || a == "--target-directory":
				if i+1 < len(rest) {
					dests = append(dests, rest[i+1])
				}
				i++
			case strings.HasPrefix(a, "--target-directory="):
				dests = append(dests, xword{text: strings.TrimPrefix(a, "--target-directory="), known: rest[i].known})
			case len(a) == 2 && a[0] == '-' && strings.IndexByte(withValue, a[1]) >= 0:
				i++
			case strings.HasPrefix(a, "-") && a != "-":
			default:
				pos = append(pos, rest[i])
			}
		}
		if len(pos) >= 2 {
			dests = append(dests, pos[len(pos)-1])
			sources = pos[:len(pos)-1]
		}
	case v.name == "tee":
		for _, xw := range rest {
			if !strings.HasPrefix(xw.text, "-") {
				dests = append(dests, xw)
			}
		}
	case v.name == "dd":
		for _, xw := range rest {
			if of, ok := strings.CutPrefix(xw.text, "of="); ok {
				dests = append(dests, xword{text: of, known: xw.known})
			}
		}
	}
	return dests, sources
}

// isAgentDir reports whether the repository-relative rel is a .claude
// directory.
func isAgentDir(rel string) bool { return strings.EqualFold(path.Base(rel), ".claude") }

// restWords are v's expanded words after its command name, redirection
// targets left out.
func (s *setup) restWords(v view) []xword {
	var args []xword
	for _, xw := range v.words {
		if !xw.redirect {
			args = append(args, xw)
		}
	}
	return args[len(args)-len(v.rest):]
}

// selfApproval reports whether v runs a person-only subcommand of the tool
// ('pbipx policy approve', 'pbipx policy unenroll'; personOnly): the tool by
// any path, or under script, expect, unbuffer or winpty, which give it the
// terminal approval asks for.
func selfApproval(v view) bool {
	switch v.name {
	case toolName:
		fed := feedOf(v.argv, v.argvKnown).runner != ""
		return personOnly(v.rest, v.restKnown, fed)
	case "script", "expect", "unbuffer", "winpty", "parallel":
		// parallel appends the words it reads to the command it is given.
		fed := v.name == "parallel" && feedOf(v.argv, v.argvKnown).runner != ""
		tokens := strings.FieldsFunc(strings.Join(v.rest, " "), func(r rune) bool {
			return strings.ContainsRune(" \t\r\n;&|'\"{}[]()", r)
		})
		for i, t := range tokens {
			if progName(t) == toolName {
				if personOnly(tokens[i+1:], nil, fed) {
					return true
				}
			}
		}
	}
	return false
}

// unknownArgs are the words of git's arguments in v that cannot be told.
func unknownArgs(v view) []string {
	var unknown []string
	if v.name == "git" {
		for i, k := range v.restKnown {
			if !k {
				unknown = append(unknown, v.rest[i])
			}
		}
	}
	return unknown
}

// gitRule decides rule 2 for v, from each spelling of its working
// directory; from an unknown one, as if from each protected root, where
// every relative pathspec touches the root. An argument that cannot be
// told (an expansion) could be any pathspec, revision, option or
// directory: a subcommand that prints content is then unprovable
// (gitContent).
func (s *setup) gitRule(v view) *gitDenial {
	// An absolute Git Bash path in its words (/tmp/x) is a path as written
	// and the one Git Bash hands git: the line is judged as each.
	if placed, ok := s.placedView(v); ok {
		if g := s.gitRuleAs(placed); g != nil {
			return g
		}
	}
	return s.gitRuleAs(v)
}

// placedView is v, a git command, with each absolute Git Bash path in its
// words (or the value of a --flag=value) replaced by the Windows path
// cygpath says it is (msys.go), for the rules that read git's arguments.
// ok is false when no word changes. The words of the line itself are never
// changed, only this copy.
func (s *setup) placedView(v view) (view, bool) {
	if s.msys == nil || v.name != "git" {
		return v, false
	}
	var nonRedirect []int
	for wi, w := range v.words {
		if !w.redirect {
			nonRedirect = append(nonRedirect, wi)
		}
	}
	first := len(nonRedirect) - len(v.argv)
	if first < 0 {
		return v, false
	}
	swap := func(t string) (string, bool) {
		if strings.HasPrefix(t, "/") {
			if pl, needed, ok := s.msys.word(t); needed && ok {
				return pl, true
			}
			return t, false
		}
		if head, val, ok := strings.Cut(t, "="); ok && strings.HasPrefix(val, "/") {
			if pl, needed, ok := s.msys.word(val); needed && ok {
				return head + "=" + pl, true
			}
		}
		return t, false
	}
	argv, words := slices.Clone(v.argv), slices.Clone(v.words)
	changed := false
	for p := len(v.argv) - len(v.rest); p < len(v.argv); p++ {
		text := v.argv[p]
		wi := nonRedirect[first+p]
		if words[wi].message || !words[wi].known {
			continue
		}
		nt, ok := swap(text)
		if !ok {
			continue
		}
		changed = true
		argv[p], words[wi].text = nt, nt
		if np, ok := swap(words[wi].pattern); ok && words[wi].glob {
			words[wi].pattern = np
		} else {
			words[wi].pattern = escapeGlob(nt)
		}
	}
	if !changed {
		return v, false
	}
	v.argv, v.words = argv, words
	v.name, v.rest, v.under = command(v.argv)
	v.restKnown = v.argvKnown[len(v.argvKnown)-len(v.rest):]
	return v, true
}

// gitRuleAs is gitRule for v as it stands.
func (s *setup) gitRuleAs(v view) *gitDenial {
	v = s.expandShellGlobs(v)
	unknown := unknownArgs(v)
	if v.name == "git" && feedOf(v.argv, v.argvKnown).runner != "" {
		// xargs (and parallel) add the words they read to git's: words
		// the gate cannot tell, which could be options (echo -p | xargs
		// git log). With -I they replace a word instead, as unknown.
		const stdinWord = "\x00stdin"
		v.argv = append(slices.Clone(v.argv), stdinWord)
		unknown = append(unknown, stdinWord)
	}
	if v.name == "git" {
		// find -exec's and fd -x's placeholder is a file name the gate
		// cannot tell, which could be an option (a file named -p).
		for _, a := range v.rest {
			if strings.Contains(a, "{}") || strings.Contains(a, "{/}") || strings.Contains(a, "{.}") {
				unknown = append(unknown, a)
			}
		}
	}
	dirs := v.cwd.dirs
	if !v.cwd.known() {
		dirs = []string{s.repo.Root}
		for _, root := range s.policy.ProtectedRoots() {
			dirs = append(dirs, filepath.Join(s.repo.Root, filepath.FromSlash(root)))
		}
	}
	for _, d := range dirs {
		in := *s
		in.cwd = d
		if g := in.gitContent(v.argv, unknown, v.argvShapes); g != nil {
			if slices.Contains(unknown, "\x00stdin") && feedOf(v.argv, v.argvKnown).runner != "" {
				g.hint += " Under xargs or parallel, put -- before the input words (xargs git add --)."
			}
			return g
		}
	}
	return nil
}

// namesLocalState reports whether xw, in v, names a local-state file or a
// directory local state is kept in, under any spelling. It returns the
// path, repository-relative.
func (s *setup) namesLocalState(v view, xw xword) (string, bool) {
	abss, _ := s.wordSpellings(v, xw)
	c := v.cache
	if c == nil {
		c = newSpellCache()
	}
	for _, a := range abss {
		key := "local\x00" + a
		if rel, ok := c.paths[key]; ok {
			if rel != "" {
				return rel, true
			}
			continue
		}
		hit := ""
		for _, rel := range s.relsOf([]string{a}) {
			if class, _ := s.policy.Classify(rel); class == policy.LocalState || s.policy.LocalStateDir(rel) || s.nestedLocalState(rel) {
				hit = rel
				break
			}
		}
		c.paths[key] = hit
		if hit != "" {
			return hit, true
		}
	}
	return "", false
}

// memo is f(a), remembered for the decision under kind.
func (c *spellCache) memo(kind, a string, f func(string) bool) bool {
	key := kind + "\x00" + a
	if r, ok := c.paths[key]; ok {
		return r != ""
	}
	r := f(a)
	c.paths[key] = ""
	if r {
		c.paths[key] = "y"
	}
	return r
}

// readers are commands that print or copy what files hold. An argument of
// one the gate cannot expand could name any file.
var readers = map[string]bool{
	"cat": true, "tac": true, "nl": true, "head": true, "tail": true, "less": true, "more": true,
	"most": true, "bat": true, "batcat": true, "view": true, "zcat": true, "zless": true, "zmore": true,
	"grep": true, "egrep": true, "fgrep": true, "zgrep": true, "rg": true, "ag": true, "ack": true,
	"sed": true, "awk": true, "gawk": true, "mawk": true, "nawk": true, "strings": true, "od": true,
	"xxd": true, "hexdump": true, "hd": true, "base64": true, "base32": true, "sort": true, "uniq": true,
	"cut": true, "wc": true, "paste": true, "join": true, "comm": true, "diff": true, "cmp": true,
	"jq": true, "yq": true, "cp": true, "rsync": true, "tar": true, "zip": true, "find": true,
	"iconv": true, "fold": true, "fmt": true, "pr": true, "column": true, "source": true, ".": true,
	"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true, "ed": true, "ex": true,
}

// reaches reports whether the command line can reach a protected root
// (rules 3 and 4): a command runs from inside one or from a working
// directory that cannot be told; a line names one as a whole word; a word
// of a command, placed against its working directory, is one, is inside
// one, or contains one; a reading command has a word that cannot be
// placed; a search reads under a path that touches one; or find, xargs
// or a nested line reach one (walker.reach). A command that only lists
// names (lister) reaches nothing. It returns the first trigger found, the
// zero trigger when there is none. A trigger names programs only from the
// gate's own tables and paths only as protected roots, never a word of the
// command.
func (w *walker) reaches() trigger {
	s := w.s
	if w.reach.set() {
		return w.reach
	}
	for i, line := range w.lines {
		if w.message[i] && !w.redefines {
			continue
		}
		for _, root := range s.policy.ProtectedRoots() {
			if namesRootOutsideListers(line, root, !w.redefines) {
				return trigger{"the command names the protected root " + displayPath(root), hintNames}
			}
		}
	}
	for _, v := range w.views {
		if v.cwd.known() && lister(v.name, v.rest) {
			continue
		}
		inRoot := func(d string) bool { return w.cache.memo("inroot", d, s.dirInRoot) }
		touches := func(a string) bool { return w.cache.memo("touches", a, s.touchesAbs) }
		if !v.cwd.known() {
			return trigger{"a cd, pushd or popd moves a command to a working directory the gate cannot tell", hintExpand}
		}
		if i := slices.IndexFunc(v.cwd.dirs, inRoot); i >= 0 {
			d := v.cwd.dirs[i]
			if samePath(d, s.cwd, s.fold) {
				return trigger{"the working directory " + s.whereDir(d), hintCwd}
			}
			return trigger{"a cd moves a command into a directory that " + s.whereDir(d), hintNames}
		}
		_, search := searchers[v.name]
		reader := readers[v.name] || search
		operands := operandWords(v)
		operandReader := !reader && v.name != "" && v.name != "gh" && readsOperands(v.name)
		mover := v.name == "cd" || v.name == "pushd" || v.name == "chdir"
		filter := jqFilter(v)
		for wi, xw := range v.words {
			if xw.message && !w.redefines {
				continue
			}
			if mover && !xw.redirect {
				// A cd target moves the working directory, which is
				// judged for the commands after it; it reads nothing.
				continue
			}
			if wi == filter {
				// jq's filter is a program, not a path.
				continue
			}
			abss, unknown := s.wordSpellings(v, xw)
			readsInput := xw.redirect && xw.input // < FILE, <> FILE: read by whatever it is for
			if unknown && !xw.here && (reader || readsInput || operandReader && operands[wi] || v.name == "gh" && ghFileValue(v, wi) || xw.fed && isInterpreter(v.name)) && !optionOnly(xw.text) {
				return trigger{v.name + " is given a word with an expansion the gate cannot resolve", hintExpand}
			}
			if i := slices.IndexFunc(abss, touches); i >= 0 {
				hint := hintNames
				who := "a word of the command names a path that "
				switch {
				case search:
					who, hint = v.name+" searches a path that ", hintSearch
				case reader:
					who = v.name + " is given a path that "
				case v.name == "cd" || v.name == "pushd" || v.name == "chdir":
					who = "a cd moves the shell into a directory that "
				}
				return trigger{who + s.whereAbs(abss[i]), hint}
			}
		}
		sc := w.searchScope(v)
		if sc.everywhere != "" {
			return trigger{v.name + " is taken to read files anywhere: " + sc.everywhere, hintSearch}
		}
		touched := func(ps []string) string {
			for _, p := range ps {
				for _, d := range v.cwd.dirs {
					for _, a := range s.spellingsAt(d, p) {
						if s.touchesAbs(a) {
							return s.whereAbs(a)
						}
					}
				}
			}
			return ""
		}
		if where := touched(sc.operands); where != "" {
			return trigger{v.name + " searches a path that " + where, hintSearch}
		}
		if where := touched([]string{"."}); jqImports(v, jqFilter(v)) && where != "" {
			return trigger{v.name + " imports modules, which it can read from its working directory, and the working directory " +
				where, hintNames}
		}
		if where := touched([]string{"."}); sc.widen != "" && where != "" {
			return trigger{v.name + " is taken to search its working directory recursively (" + sc.widen +
				"), and the working directory " + where, hintSearch}
		}
		if where := touched(sc.reads); where != "" {
			return trigger{v.name + " reads a file an option names, in a path that " + where, hintSearch}
		}
	}
	return trigger{}
}

// whereDir is whereAbs for a working directory inside a root, as given or
// with its symlinks resolved.
func (s *setup) whereDir(d string) string {
	for _, a := range []string{d, resolvePath(d)} {
		if r, ok := relTo(s.repo.Root, a, true); ok && s.underRoot(r) {
			return s.whereRel(r)
		}
	}
	return "is in a protected root"
}

// optionOnly reports whether a word is an option with no value in it.
func optionOnly(t string) bool { return strings.HasPrefix(t, "-") && !strings.Contains(t, "=") }

// dirInRoot reports whether the working directory d, as given or with its
// symlinks resolved, is a protected root or inside one.
func (s *setup) dirInRoot(d string) bool {
	for _, a := range []string{d, resolvePath(d)} {
		if r, ok := relTo(s.repo.Root, a, true); ok && s.underRoot(r) {
			return true
		}
	}
	return false
}

// namesWord reports whether name occurs in line as a whole word, without
// regard to case: neither side touches a letter, a digit, "_", "-" or
// ".".
func namesWord(line, name string) bool {
	line, name = strings.ToLower(line), strings.ToLower(name)
	for i := 0; ; {
		j := strings.Index(line[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if (start == 0 || !wordByte(line[start-1])) && (end == len(line) || !wordByte(line[end])) {
			return true
		}
		i = start + 1
	}
}

func wordByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b >= 0x80
}
