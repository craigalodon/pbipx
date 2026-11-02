package gate

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Git subcommands that always print file content (behavior.md, "The gate",
// Bash rule 2). grep is judged by the files it would print from.
var gitAlwaysContent = map[string]bool{
	"blame": true, "annotate": true, "grep": true, "archive": true,
	"format-patch": true, "difftool": true, "range-diff": true,
}

// Git subcommands that print content, or run a command, whatever their
// pathspecs say: what they print cannot be placed.
var gitUnplaceable = map[string]bool{
	"fast-export": true, "pack-objects": true, "unpack-file": true, "filter-branch": true,
}

// gitSummaryFlags turn diff and show into a summary with no content.
var gitSummaryFlags = []string{
	"--stat", "--numstat", "--shortstat", "--name-only", "--name-status",
	"--dirstat", "--compact-summary", "--summary", "--raw", "--no-patch",
	"-s", "--quiet",
}

// gitPatchFlags make git print a patch, or search content (-S, -G): content.
// Each also matches with a value attached ("-U3", "--unified=3",
// "-L1,2:file").
var gitPatchFlags = []string{
	"-p", "-u", "--patch", "--patch-with-stat", "--patch-with-raw", "-U", "--unified",
	"--cc", "-c", "--dd", "--remerge-diff", "--full-diff", "-L", "--word-diff", "--color-words",
	"--function-context", "-W", "--binary", "--ext-diff", "--textconv", "-S", "-G", "--check",
}

// gitValueFlags are flags whose value can be attached without "=".
var gitValueFlags = []string{"-U", "-L", "-S", "-G"}

// gitCatFileSafe are the cat-file modes that print no content.
var gitCatFileSafe = []string{"-t", "-s", "-e", "--batch-check"}

// gitRefSets are revision options of show that name objects by the
// repository's refs, or read them from standard input: what they show
// cannot be told from the text.
var gitRefSets = []string{
	"--all", "--branches", "--tags", "--remotes", "--glob", "--stdin", "--reflog",
	"--bisect", "--alternate-refs", "--indexed-objects",
}

// gitGlobalWithValue are git's global options that take the next word.
var gitGlobalWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--config-env": true, "--super-prefix": true, "--list-cmds": true, "--attr-source": true,
}

// gitRevValueOpts are options of log, show and the diff commands that take
// their value as the next word (git show --max-count 1 HEAD). Only options
// known to do so are listed: a word wrongly taken for a value could hide a
// revision.
var gitRevValueOpts = map[string]bool{
	"-n": true, "--max-count": true, "--skip": true, "--since": true, "--after": true,
	"--until": true, "--before": true, "--since-as-filter": true, "--author": true,
	"--committer": true, "--grep": true, "--grep-reflog": true, "--date": true,
	"--diff-merges": true, "--diff-filter": true, "-S": true, "-G": true, "-O": true,
	"-L": true, "--find-object": true, "--output": true, "--line-prefix": true,
	"--word-diff-regex": true, "--stat-width": true, "--stat-name-width": true,
	"--stat-graph-width": true, "--stat-count": true, "--color-moved-ws": true,
	"--src-prefix": true, "--dst-prefix": true, "--anchored": true, "--diff-algorithm": true,
	"-I": true, "--ignore-matching-lines": true, "--skip-to": true, "--rotate-to": true,
	"--inter-hunk-context": true, "--output-indicator-new": true, "--output-indicator-old": true,
	"--output-indicator-context": true, "--ws-error-highlight": true, "--default": true,
	"--exclude": true,
}

// gitStripNext are the diff options removed from a name-only run that
// take their value as the next word, which goes with them.
var gitStripNext = map[string]bool{
	"--output": true, "--line-prefix": true, "--word-diff-regex": true, "--stat-width": true,
	"--stat-name-width": true, "--stat-graph-width": true, "--stat-count": true,
	"--color-moved-ws": true,
}

// gitStripNames are the options removed from a name-only run, alone or
// with "=value": each changes only how a diff is printed, or prints it
// elsewhere (--output), and some cannot be combined with --name-status.
var gitStripNames = map[string]bool{
	"-p": true, "-u": true, "--patch": true, "--patch-with-stat": true, "--patch-with-raw": true,
	"--no-patch": true, "-s": true, "--quiet": true, "--stat": true, "--numstat": true,
	"--shortstat": true, "--summary": true, "--compact-summary": true, "--raw": true,
	"--name-only": true, "--name-status": true, "--check": true, "--dirstat": true,
	"--dirstat-by-file": true, "--cumulative": true, "--function-context": true, "-W": true,
	"--binary": true, "--ext-diff": true, "--no-ext-diff": true, "--textconv": true,
	"--no-textconv": true, "--word-diff": true, "--color-words": true, "--color": true,
	"--no-color": true, "--color-moved": true, "--graph": true, "--oneline": true,
	"--pretty": true, "--format": true, "--relative": true, "--no-relative": true,
	"--show-signature": true, "--log-size": true, "--show-linear-break": true, "--unified": true,
	"--output": true, "--line-prefix": true, "--word-diff-regex": true, "--stat-width": true,
	"--stat-name-width": true, "--stat-graph-width": true, "--stat-count": true,
	"--color-moved-ws": true,
}

// gitNameOnly are the options every name-only run of a diff, log or show
// is given: names and statuses separated by NUL, nothing run but git, and
// paths relative to the top of the work tree.
var gitNameOnly = []string{
	"--name-status", "-z", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative",
}

// gitNeverContent are git's commands that print no file content whatever
// their options: a word given to one that cannot be told is judged as
// usual. Before "--", such a word given to any other could be an option
// that turns content on (-p, -v, --cc, --file), and denies it.
var gitNeverContent = map[string]bool{
	"branch": true, "fetch": true, "push": true, "remote": true, "tag": true, "init": true,
	"ls-remote": true, "ls-files": true, "ls-tree": true, "rev-parse": true, "merge-base": true,
	"describe": true, "show-ref": true, "for-each-ref": true, "count-objects": true, "version": true,
	"help": true, "gc": true,
}

// unknownOption reports whether one of args, sub's words before "--", is
// one that cannot be told (unknown) and could be an option: any but a
// message (gitMessageArgs), for a subcommand whose -m takes one (git
// commit -m "$(cat <<EOF ...)", git commit -am "$MSG"). A message the
// shell makes several words (shape(i) for args[i]: git commit -m $MSG,
// -m "$@", -m *) could hold options too.
//
// why says what the argument is, in fixed words after the subcommand.
func unknownOption(sub string, args, unknown []string, shape func(int) argShape) (why string) {
	msg, _ := gitMessageArgs(sub, args)
	for i, a := range args {
		sh := shape(i)
		if msg[i] && sh.glob {
			return "is given, before --, a message word with a glob outside quotes, which the shell expands " +
				"to the names it matches, and which could be an option that prints content (-p, -v, --cc)"
		}
		if (!msg[i] || sh.splits) && slices.Contains(unknown, a) {
			return "is given, before --, an argument the gate cannot expand " + unknownWhat(sh) +
				", which could be an option that prints content (-p, -v, --cc)"
		}
	}
	return ""
}

// unknownWhat names, in parentheses, what kind of argument the gate
// cannot expand.
func unknownWhat(sh argShape) string {
	if sh.brace {
		return "(a brace expansion of more than 64 words)"
	}
	return "(a variable or a command substitution)"
}

// argShape is how the shell may turn one argument into several: splits
// for an expansion it splits into words (unquoted, or "$@" and the like),
// glob for a glob character outside quotes, brace for a brace expansion
// the gate does not expand.
type argShape struct {
	splits, glob, brace bool
}

// gitMessageSubs are the subcommands whose -m and --message take a message.
var gitMessageSubs = map[string]bool{"commit": true, "merge": true, "stash": true, "notes": true, "tag": true}

// gitOptions are a subcommand's options, as far as the gate reads them to
// tell a message from an option: the short options that take no value
// (plain), that take one attached or as the next word (value, m among
// them), and that take one only attached (optional); and the long options
// known to take no value (long) and to take the next word as their value
// unless given with "=" (longValue). An option in none of these could take
// the next word or not: the words after it cannot be read.
type gitOptions struct {
	plain, value, optional string
	long, longValue        map[string]bool
}

// gitMessageOptions are the options of each subcommand in gitMessageSubs,
// from git's documentation.
var gitMessageOptions = map[string]gitOptions{
	"commit": {plain: "apsneiovqz", value: "cCFmt", optional: "Su", longValue: setOf(
		"--file", "--message", "--template", "--reuse-message", "--reedit-message", "--fixup",
		"--squash", "--author", "--date", "--cleanup", "--trailer", "--pathspec-from-file"), long: setOf(
		"--all", "--patch", "--interactive", "--signoff", "--no-signoff", "--verbose", "--no-verbose",
		"--quiet", "--dry-run", "--short", "--branch", "--porcelain", "--long", "--null", "--amend",
		"--no-edit", "--edit", "--reset-author", "--allow-empty", "--allow-empty-message", "--no-verify",
		"--verify", "--include", "--only", "--status", "--no-status", "--gpg-sign", "--no-gpg-sign",
		"--no-post-rewrite", "--untracked-files", "--ahead-behind", "--no-ahead-behind",
		"--pathspec-file-nul")},
	"merge": {plain: "nevq", value: "sXFm", optional: "S", longValue: setOf(
		"--file", "--message", "--strategy", "--strategy-option", "--into-name", "--cleanup"), long: setOf(
		"--commit", "--no-commit", "--edit", "--no-edit", "--ff", "--no-ff", "--ff-only", "--stat",
		"--no-stat", "--summary", "--no-summary", "--log", "--no-log", "--squash", "--no-squash",
		"--signoff", "--no-signoff", "--verify", "--no-verify", "--verify-signatures",
		"--no-verify-signatures", "--quiet", "--verbose", "--progress", "--no-progress",
		"--allow-unrelated-histories", "--rerere-autoupdate", "--no-rerere-autoupdate", "--autostash",
		"--no-autostash", "--overwrite-ignore", "--no-overwrite-ignore", "--gpg-sign", "--no-gpg-sign")},
	"stash": {plain: "pSkuaq", value: "m", longValue: setOf("--message", "--pathspec-from-file"), long: setOf(
		"--patch", "--staged", "--keep-index", "--no-keep-index", "--include-untracked",
		"--no-include-untracked", "--all", "--quiet", "--pathspec-file-nul")},
	"notes": {plain: "f", value: "FmcC", longValue: setOf(
		"--file", "--message", "--reuse-message", "--reedit-message"), long: setOf(
		"--force", "--allow-empty", "--stripspace", "--no-stripspace", "--separator", "--no-separator")},
	"tag": {plain: "asfe", value: "umF", optional: "n", longValue: setOf(
		"--file", "--message", "--local-user", "--cleanup"), long: setOf(
		"--annotate", "--sign", "--no-sign", "--force", "--edit", "--no-edit")},
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// gitMessageArgs reads args, sub's words before "--", as git reads them,
// and marks each that is a message, or holds one attached: the word after
// -m or --message, a word -m<text> or --message=<text>, and the same after
// a cluster of short options that take no value (-am, -am<text>). An m
// after a letter that takes a value is that value (-Fm), and a -m that is
// itself the value of the option before it (-F -m, --author -m) starts no
// message. After an option the gate does not know (an abbreviation
// included) given without "=", whether the next word is its value cannot
// be told, so no later word is a message. stdin is set when -F or --file reads the
// message from standard input ("-"). Only commit, merge, tag, stash push
// (or stash given options alone) and notes add and append take a message.
func gitMessageArgs(sub string, args []string) (msg []bool, stdin bool) {
	msg = make([]bool, len(args))
	if !gitMessageSubs[sub] {
		return msg, false
	}
	opts := gitMessageOptions[sub]
	start := 0
	switch sub {
	case "stash":
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			if args[0] != "push" {
				return msg, false
			}
			start = 1
		}
	case "notes":
		if len(args) == 0 || (args[0] != "add" && args[0] != "append") {
			return msg, false
		}
		start = 1
	}
	for i := start; i < len(args); i++ {
		a := args[i]
		next := ""
		if i+1 < len(args) {
			next = args[i+1]
		}
		name, value, hasValue := strings.Cut(a, "=")
		switch {
		case strings.HasPrefix(a, "--"):
			switch {
			case !opts.longValue[name] && !opts.long[name]:
				if !hasValue {
					return msg, stdin
				}
				// With "=" it takes no other word.
			case opts.long[name]:
			case name == "--message":
				if hasValue {
					msg[i] = true
				} else if i+1 < len(args) {
					msg[i+1] = true
				}
			case name == "--file":
				stdin = stdin || (hasValue && value == "-") || (!hasValue && next == "-")
			}
			if opts.longValue[name] && !hasValue {
				i++
			}
		case len(a) > 1 && a[0] == '-':
			c, ok := opts.cluster(a, next)
			if !ok {
				return msg, stdin
			}
			msg[i] = c.attached
			stdin = stdin || c.stdin
			if c.nextMsg && i+1 < len(args) {
				msg[i+1] = true
			}
			if c.nextValue {
				i++
			}
		}
	}
	return msg, stdin
}

// gitCluster is what a cluster of short options says of the message:
// whether it holds one attached (-am<text>), whether the next word is a
// value (nextValue), the message among them (nextMsg), and whether -F
// reads the message from standard input.
type gitCluster struct {
	attached, nextMsg, nextValue, stdin bool
}

// cluster reads a, a cluster of short options, followed by the word next.
// ok is false when a holds a letter the gate does not know.
func (o gitOptions) cluster(a, next string) (c gitCluster, ok bool) {
	for j := 1; j < len(a); j++ {
		letter, rest := a[j], a[j+1:]
		switch {
		case strings.IndexByte(o.plain, letter) >= 0:
			continue
		case strings.IndexByte(o.optional, letter) >= 0:
			// The rest of the cluster is its value.
			return c, true
		case strings.IndexByte(o.value, letter) >= 0:
			value := rest
			if rest == "" {
				value = next
				c.nextValue = true
			}
			c.attached = letter == 'm' && rest != ""
			c.nextMsg = letter == 'm' && rest == ""
			c.stdin = letter == 'F' && value == "-"
			return c, true
		default:
			return c, false
		}
	}
	return c, true
}

// gitExpandHint ends a git refusal for an argument the gate cannot expand:
// for a subcommand that reads its message from standard input with -F -,
// it adds how to give one the gate can read. merge reads no message from
// "-", and stash has no -F.
func gitExpandHint(sub string) string {
	switch sub {
	case "commit", "tag":
		return hintGitExpand + " `git " + sub + " -F - <<'EOF'` takes the message from a here-document."
	case "notes":
		return hintGitExpand + " `git notes add -F - <<'EOF'` takes the message from a here-document."
	}
	return hintGitExpand
}

// gitMessage is what of a simple command is git's message text, read by
// gitMessageText: prose, not paths, so a protected root's name in it does
// not make the command reach the root. words and subs are indexed as the
// command's words and command substitutions.
type gitMessage struct {
	// words are the message words: the value of -m or --message given
	// apart, or a word holding it attached (gitMessageArgs).
	words []bool
	// subs are the command substitutions in a message word whose one
	// command is cat reading a here-document: its body is the message.
	subs []bool
	// docs is set when git reads its message from standard input (-F -,
	// --file=-): the command's here-documents are the message.
	docs bool
}

// gitMessageText reads c, when it runs git with a message: which of its
// words, command substitutions and here-documents are message text. A
// command that does not, or whose words do not line up as the gate reads
// them, has none.
func gitMessageText(c simple) gitMessage {
	m := gitMessage{words: make([]bool, len(c.words)), subs: make([]bool, len(c.subs))}
	if c.fallback || !slices.ContainsFunc(c.words, func(wd word) bool { return progName(wd.text) == "git" }) {
		return m
	}
	var idx []int // the index in c.words of each argument
	var args []string
	for i, wd := range c.words {
		if !wd.redirect {
			idx = append(idx, i)
			args = append(args, wd.text)
		}
	}
	// Under a wrapper (xargs, say) git's input or its words may be other
	// than the line shows.
	name, rest, under := command(args)
	if name != "git" || len(under) > 0 || !isSuffix(args, rest) {
		return m
	}
	inv, ok := parseGit(rest, "")
	if !ok || inv.ambiguous || !gitMessageSubs[inv.sub] || len(inv.rest) > len(rest) {
		return m
	}
	start := len(args) - len(inv.rest)
	msg, stdin := gitMessageArgs(inv.sub, inv.args)
	for i, isMsg := range msg {
		// A word the shell makes several may be more than the message,
		// and a ${...} that runs a command is not text.
		w := idx[start+i]
		wd := c.words[w]
		m.words[w] = isMsg && !wd.splits && !wd.globs() && !slices.ContainsFunc(wd.parts, runsInBraces)
	}
	if stdin && inv.sub != "merge" {
		// -F - reads standard input: a here-document or a here-string. git
		// merge takes "-" for a file name.
		m.docs = true
		for i, wd := range c.words {
			if wd.here && !wd.splits && !slices.ContainsFunc(wd.parts, runsInBraces) {
				m.words[i] = true
			}
		}
	}
	// The command substitutions in the words, in order, are c.subs: one
	// for each $(...) or backtick part, and those a ${...} part holds.
	k := 0
	for i, wd := range c.words {
		for _, p := range wd.parts {
			n := 0
			switch {
			case p.kind != opaquePart:
			case strings.HasPrefix(p.raw, "$("), strings.HasPrefix(p.raw, "`"):
				n = 1
			case strings.HasPrefix(p.raw, "${"):
				n = len(bodySubs([]rune(p.raw[2:len(p.raw)-1]), 0))
			}
			for range n {
				if k >= len(c.subs) {
					return gitMessage{words: m.words, subs: make([]bool, len(c.subs)), docs: m.docs}
				}
				m.subs[k] = n == 1 && strings.HasPrefix(p.raw, "$(") && m.words[i] && catHeredoc(c.subs[k]) != nil
				k++
			}
		}
	}
	if k != len(c.subs) {
		m.subs = make([]bool, len(c.subs))
	}
	return m
}

// runsInBraces reports whether p is a ${...} whose body runs a command.
func runsInBraces(p part) bool {
	return p.kind == opaquePart && strings.HasPrefix(p.raw, "${") &&
		(strings.Contains(p.raw, "$(") || strings.Contains(p.raw, "`"))
}

// catHeredoc is the here-document a command line reads when the line is
// one command, cat with no argument, reading one here-document; nil
// otherwise.
func catHeredoc(line string) *heredoc {
	cmds := splitCommands(line)
	if len(cmds) != 1 {
		return nil
	}
	c := cmds[0]
	if c.fallback || c.pipeIn || len(c.subs) > 0 || len(c.docs) != 1 || len(c.words) != 1 ||
		c.words[0].redirect || c.words[0].text != "cat" {
		return nil
	}
	return c.docs[0]
}

// isSuffix reports whether tail is a suffix of words.
func isSuffix(words, tail []string) bool {
	return len(tail) <= len(words) && slices.Equal(words[len(words)-len(tail):], tail)
}

// whyUnknownSpec is why git given an argument that cannot be told is
// denied when that argument could be a pathspec, a revision or a
// directory. It reads after the subcommand.
const whyUnknownSpec = "is given an argument the gate cannot expand (a variable or a command substitution), " +
	"which could name a path in a protected root"

// gitBuiltins are git's own commands. git runs a built-in command before it
// looks for an alias of the same name, so these are never resolved as
// aliases. A name that is neither one of these nor an alias is denied when
// the command might print content: it could be an alias defined earlier
// in the same command line, or a git-<name> program.
var gitBuiltins = map[string]bool{}

func init() {
	for _, list := range [][]string{
		mapKeys(gitAlwaysContent), mapKeys(gitUnplaceable),
		{
			"add", "am", "apply", "bisect", "branch", "bundle", "cat-file", "check-attr",
			"check-ignore", "check-mailmap", "check-ref-format", "checkout", "checkout-index",
			"cherry", "cherry-pick", "clean", "clone", "column", "commit", "commit-graph",
			"commit-tree", "config", "count-objects", "describe", "diff", "diff-files",
			"diff-index", "diff-tree", "fetch", "fetch-pack", "for-each-ref", "for-each-repo",
			"fsck", "gc", "hash-object", "help", "index-pack", "init", "interpret-trailers",
			"log", "ls-files", "ls-remote", "ls-tree", "maintenance", "merge", "merge-base",
			"merge-file", "merge-tree", "mktag", "mktree", "mv", "name-rev", "notes", "prune",
			"pull", "push", "read-tree", "rebase", "reflog", "remote", "repack", "replace",
			"replay", "request-pull", "rerere", "reset", "restore", "rev-list", "rev-parse",
			"revert", "rm", "shortlog", "show", "show-branch", "show-index", "show-ref",
			"sparse-checkout", "stash", "status", "submodule", "switch", "symbolic-ref", "tag",
			"update-index", "update-ref", "var", "verify-commit", "verify-pack", "verify-tag",
			"version", "whatchanged", "worktree", "write-tree", "mergetool", "instaweb",
			"stage", "init-db", "pack-refs", "prune-packed", "multi-pack-index",
			"update-server-info", "patch-id", "stripspace", "fmt-merge-msg", "mailsplit",
			"mailinfo", "get-tar-commit-id", "unpack-objects", "refs", "last-modified",
		},
	} {
		for _, name := range list {
			gitBuiltins[name] = true
		}
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// gitPlace is where a git command runs, relative to this repository.
type gitPlace int

const (
	// gitHere is this repository: its work tree, a work tree linked to it,
	// or a repository nested inside its work tree.
	gitHere gitPlace = iota
	// gitUnknown is a directory that does not exist, cannot be asked
	// about, or is a repository whose work tree holds this one.
	gitUnknown
	// gitForeign is another repository, outside this one's work tree.
	gitForeign
	// gitNongit is a directory outside every repository (and not above
	// this one).
	gitNongit
)

// gitInvocation is one git command line, read far enough to judge it.
type gitInvocation struct {
	sub string // the subcommand
	// rest is every word after the subcommand, and args those before any
	// "--".
	rest, args []string
	// specs are the words after "--", and dashdash whether there was one.
	specs    []string
	dashdash bool
	// globals are the global options before the subcommand, with their
	// values, but -C: a name-only run passes them on.
	globals []string
	// dir is the directory relative paths start from (-C moves it).
	dir string
	// elsewhere is true when --git-dir or --work-tree point git at a tree
	// these paths cannot be checked against.
	elsewhere bool
	// unsafe is true when a global option can make git run a command, or
	// print what these rules cannot see: -c or --config-env with a key
	// not known to be harmless, --exec-path=. unsafeWhy says which came
	// first, in fixed words.
	unsafe    bool
	unsafeWhy string
	// ambiguous is set when a long option abbreviates more than one of
	// the subcommand's options (expandLong).
	ambiguous bool
	// dashHidden is set when a "--" right after an option that may take
	// it as its value is read as the end of the options, and a word after
	// it looks like an option: it could be one (dashDash).
	dashHidden bool
	// oddSpecs is true when a global option changes how every pathspec
	// matches (--literal-pathspecs, --glob-pathspecs, --icase-pathspecs).
	oddSpecs bool
	// unknown is true when an argument cannot be told (gitContent): no
	// query that could find the command prints nothing is made with it.
	unknown bool

	// place is where dir is (locate). For gitHere, top is the root of the
	// work tree git runs in ("" inside a git directory), and prefix is
	// where that work tree is in this repository's tree: "" for this
	// repository and its linked work trees, whose paths are this
	// repository's, and the nested repository's path for one nested in
	// it.
	place  gitPlace
	top    string
	prefix string
}

// parseGit reads a git command's words after "git". ok is false when there
// is no subcommand.
func parseGit(words []string, cwd string) (inv gitInvocation, ok bool) {
	inv.dir = cwd
	return inv.parse(words)
}

// setUnsafe marks inv unsafe, for the reason why, when bad is true.
func (inv *gitInvocation) setUnsafe(bad bool, why string) {
	if bad && !inv.unsafe {
		inv.unsafe, inv.unsafeWhy = true, why
	}
}

// parse reads words, global options then a subcommand, on top of what inv
// already holds.
func (inv gitInvocation) parse(words []string) (gitInvocation, bool) {
	inv.globals = slices.Clone(inv.globals)
	i := 0
	for i < len(words) {
		w := words[i]
		name, value, hasValue := strings.Cut(w, "=")
		if !strings.HasPrefix(w, "-") {
			break
		}
		next := ""
		if i+1 < len(words) {
			next = words[i+1]
		}
		switch {
		case w == "-C":
			if next != "" {
				inv.dir = abs(inv.dir, next)
			}
		case name == "--git-dir" || name == "--work-tree":
			inv.elsewhere = true
		case w == "-c":
			inv.setUnsafe(!safeConfig(next), "runs with -c setting a key that can run a command or change what git prints")
		case name == "--config-env":
			kv := next
			if hasValue {
				kv = value
			}
			// The value comes from the environment: only its key is known.
			key, _, _ := strings.Cut(kv, "=")
			inv.setUnsafe(!safeConfig(key+"=\x00"), "runs with --config-env setting a key that can run a command or change what git prints")
		case name == "--exec-path" && hasValue:
			inv.setUnsafe(true, "runs with --exec-path, which points git at other programs")
		case w == "--literal-pathspecs" || w == "--glob-pathspecs" || w == "--icase-pathspecs":
			inv.oddSpecs = true
		}
		takesNext := gitGlobalWithValue[name] && !hasValue
		if w != "-C" {
			inv.globals = append(inv.globals, w)
			if takesNext && next != "" {
				inv.globals = append(inv.globals, next)
			}
		}
		i++
		if takesNext {
			i++
		}
	}
	if i >= len(words) {
		return inv, false
	}
	inv.sub = words[i]
	inv.rest, inv.ambiguous = expandLong(inv.sub, words[i+1:])
	inv.args, inv.specs, inv.dashdash = inv.rest, nil, false
	k, hidden := dashDash(inv.sub, inv.rest)
	if k >= 0 {
		inv.args, inv.specs, inv.dashdash = inv.rest[:k], inv.rest[k+1:], true
	}
	inv.dashHidden = hidden
	return inv, true
}

// safeConfig reports whether a -c key=value setting is known to change
// nothing these rules depend on: it cannot run a command (an alias, a
// pager, an editor, a diff or textconv driver, a filter, a hook), and does
// not change what a command prints (stash.showPatch, log.*). Every other
// key is unprovable. A value of "\x00" stands for one that is not known.
func safeConfig(kv string) bool {
	key, value, _ := strings.Cut(kv, "=")
	key = strings.ToLower(key)
	section, _, _ := strings.Cut(key, ".")
	switch section {
	case "color", "column", "advice", "user", "author", "committer", "i18n", "init", "safe":
		return true
	case "pager":
		return safePager(value) || isBool(value)
	}
	switch key {
	case "commit.verbose", "stash.showpatch":
		// Off turns nothing on.
		return isFalse(value)
	case "core.pager":
		return safePager(value)
	case "core.editor", "sequence.editor":
		return value == "true" || value == ":"
	case "core.quotepath", "core.autocrlf", "core.safecrlf", "core.eol", "core.abbrev",
		"core.filemode", "commit.gpgsign", "tag.gpgsign", "gc.auto", "maintenance.auto",
		"fetch.prune", "pull.rebase", "pull.ff", "push.default", "push.autosetupremote",
		"rebase.autostash", "status.short", "status.branch", "status.showuntrackedfiles",
		"diff.noprefix", "diff.renames", "diff.mnemonicprefix", "merge.ff", "merge.conflictstyle":
		return true
	}
	return false
}

// git config's options, after expandLong: those that take a value
// (attached with "=" or as the next word), those that only read, those
// that write whatever the keys, and the others, which change neither.
var (
	gitConfigValue = []string{"--file", "-f", "--blob", "--type", "--default", "--comment", "--value", "--url"}
	gitConfigReads = []string{
		"--get", "--get-all", "--get-regexp", "--get-urlmatch", "--get-color", "--get-colorbool",
		"--list", "-l",
	}
	gitConfigWrites = []string{
		"--add", "--replace-all", "--unset", "--unset-all", "--rename-section", "--remove-section",
		"--edit", "-e",
	}
	gitConfigFlags = []string{
		"--global", "--system", "--local", "--worktree", "--fixed-value", "--bool", "--int",
		"--bool-or-int", "--bool-or-str", "--path", "--expiry-date", "--null", "-z", "--name-only",
		"--includes", "--no-includes", "--show-origin", "--show-scope", "--all", "--regexp",
		"--show-names", "--append",
	}
)

// configWrite reports whether argv (wrappers and assignments included)
// runs git config to change the configuration in a way not known to be
// harmless: any write mode (--add, --replace-all, --unset, --rename-section,
// --remove-section, --edit and the subcommands unset, rename-section,
// remove-section and edit), or a set of a key that is not known to be
// harmless (safeConfig), or of more than a key and a value. Such a change,
// earlier in the line, can change what a later git command prints
// (commit.verbose, stash.showPatch, diff.submodule, an alias), while the
// gate reads the configuration as it is before the line runs. git config
// is read as git reads it: the value of an option that takes one is never
// an option (--comment --get), a long option is expanded (expandLong),
// and one that abbreviates more than one option, an option the gate does
// not know, or a word that cannot be told (unknown) counts as a write.
// Reading the configuration (--get, --list, get, list, or one key alone)
// is no write.
func configWrite(argv, unknown []string) bool {
	name, rest := program(argv)
	if name != "git" {
		return false
	}
	inv, ok := parseGit(rest, "")
	if !ok || inv.sub != "config" {
		return false
	}
	if inv.ambiguous {
		return true
	}
	read := false
	var pos []string
	for i := 0; i < len(inv.rest); i++ {
		a := inv.rest[i]
		if slices.Contains(unknown, a) {
			return true
		}
		opt, _, hasValue := strings.Cut(a, "=")
		switch {
		case a == "--":
			pos = append(pos, inv.rest[i+1:]...)
			i = len(inv.rest)
		case slices.Contains(gitConfigValue, opt):
			if !hasValue {
				i++
			}
		case slices.Contains(gitConfigReads, opt):
			read = true
		case slices.Contains(gitConfigWrites, opt):
			return true
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if !slices.Contains(gitConfigFlags, opt) {
				return true
			}
		default:
			pos = append(pos, a)
		}
	}
	sub := ""
	if !read && len(pos) > 0 && !strings.Contains(pos[0], ".") {
		sub, pos = pos[0], pos[1:]
	}
	switch sub {
	case "get", "list":
		return false
	case "", "set":
	default:
		// unset, rename-section, remove-section, edit, or one the gate
		// does not know.
		return true
	}
	switch {
	case read:
		return false
	case len(pos) == 1 && sub == "":
		// One key alone reads it.
		return false
	case len(pos) == 2:
		return !safeConfig(pos[0] + "=" + pos[1])
	}
	return true
}

// afterConfig is why a git command (argv) is denied when the line it is
// in sets a key with git config (configWrite): any git command but git
// config itself and those that print no file content whatever their
// options; "" otherwise.
func afterConfig(argv []string) string {
	name, rest := program(argv)
	if name != "git" {
		return ""
	}
	inv, ok := parseGit(rest, "")
	if !ok || inv.sub == "config" || gitNeverContent[inv.sub] {
		return ""
	}
	return inv.sub
}

// whyAfterConfig is the refusal for a git command after git config in the
// same line, which follows the subcommand.
const whyAfterConfig = "runs in a line that sets a git configuration key that can change what it prints " +
	"(commit.verbose, stash.showPatch, an alias), and the gate reads the configuration as it is before the line runs"

// hintAfterConfig is what to do instead.
const hintAfterConfig = "Run git config on its own, then the git command as a separate call."

// safePager reports whether a pager setting only passes git's output on.
func safePager(v string) bool { return v == "" || v == "cat" }

// isFalse reports whether v is a value git reads as false.
func isFalse(v string) bool {
	switch strings.ToLower(v) {
	case "false", "no", "off", "0":
		return true
	}
	return false
}

func isBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "1", "0":
		return true
	}
	return false
}

// unsafeEnv reports whether environment assignments before git can make it
// run a command or read what these rules cannot see: GIT_CONFIG_*, GIT_DIR,
// GIT_EXTERNAL_DIFF, GIT_PAGER and the like. A pager set to cat and an
// editor set to true are harmless, and so are identities and tracing.
func unsafeEnv(words []string) bool {
	for _, w := range words {
		if !isAssignment(w) {
			continue
		}
		name, value, _ := strings.Cut(w, "=")
		switch {
		case name == "GIT_PAGER" || name == "PAGER":
			if !safePager(value) {
				return true
			}
		case name == "GIT_EDITOR" || name == "GIT_SEQUENCE_EDITOR" || name == "EDITOR" || name == "VISUAL":
			if value != "true" && value != ":" {
				return true
			}
		case name == "LESSOPEN" || name == "LESSCLOSE":
			return true
		case strings.HasPrefix(name, "GIT_AUTHOR_"), strings.HasPrefix(name, "GIT_COMMITTER_"),
			strings.HasPrefix(name, "GIT_TRACE"), name == "GIT_TERMINAL_PROMPT",
			name == "GIT_OPTIONAL_LOCKS", name == "GIT_FLUSH", name == "GIT_PROGRESS_DELAY",
			name == "GIT_ADVICE":
		case strings.HasPrefix(name, "GIT_"):
			return true
		}
	}
	return false
}

// hasFlag reports whether args hold one of flags, alone, with "=value", or,
// for those in gitValueFlags, with the value attached.
func hasFlag(args, flags []string) bool {
	for _, a := range args {
		for _, f := range flags {
			switch {
			case a == f, strings.HasPrefix(a, f+"="):
				return true
			case slices.Contains(gitValueFlags, f) && strings.HasPrefix(a, f) && len(a) > len(f):
				return true
			}
		}
	}
	return false
}

// hasShort reports whether args hold the long option long, or the short
// option c alone or in a cluster ("-ap"), where a cluster ends at the
// first letter of withValue, whose value follows it.
func hasShort(args []string, c byte, long, withValue string) bool {
	for _, a := range args {
		if a == long || (long != "" && strings.HasPrefix(a, long+"=")) {
			return true
		}
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for j := 1; j < len(a); j++ {
			if a[j] == c {
				return true
			}
			if strings.IndexByte(withValue, a[j]) >= 0 {
				break
			}
		}
	}
	return false
}

// positional is the arguments that are not options.
func positional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// positionalIndex is the indexes in args of the arguments that are neither
// options nor the value of an option in values, which take the next word
// unless their value is attached with "=". "-" alone is positional.
func positionalIndex(args []string, values map[string]bool) []int {
	var out []int
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			out = append(out, i)
			continue
		}
		if !strings.Contains(a, "=") && values[a] {
			i++
		}
	}
	return out
}

// positionalOf is the words of args at positionalIndex.
func positionalOf(args []string, values map[string]bool) []string {
	var out []string
	for _, i := range positionalIndex(args, values) {
		out = append(out, args[i])
	}
	return out
}

// pathspec is one path a git command is limited to, or excludes.
type pathspec struct {
	rel     string // repository-relative, clean; "" when it cannot be placed
	exclude bool
	// unprovable is true when what it matches cannot be told from the
	// text: a glob, case folding, attributes, or a path outside the
	// repository's tree; why says which, in fixed words.
	unprovable bool
	why        string
}

// unprovable is the answer for content whose source cannot be placed, for
// the reason why.
func unprovable(why string) []pathspec { return []pathspec{{unprovable: true, why: why}} }

// Reasons a pathspec cannot be placed that recur. Each reads after "is
// given".
const (
	whyFromFile = "--pathspec-from-file, which reads pathspecs the gate cannot see"
	whyOutside  = "a path outside the work tree"
)

// gitCheck is what judging a git command needs: whether it prints content,
// and how to tell where the content comes from.
type gitCheck struct {
	content bool
	// deny is content that cannot be placed, for the reason why.
	deny bool
	why  string
	// bySpecs: specs must provably avoid every root (specsAvoid). Nil
	// specs are the whole tree.
	bySpecs bool
	specs   []pathspec
	// runs are name-only forms of the command: every file each one names
	// must be allowed.
	runs []nameRun
}

func noContent() gitCheck { return gitCheck{} }

// unplaceable is content that cannot be placed; why says what makes it so,
// in fixed words that follow the subcommand ("git show" + why).
func unplaceable(why string) gitCheck   { return gitCheck{content: true, deny: true, why: why} }
func bySpecs(specs []pathspec) gitCheck { return gitCheck{content: true, bySpecs: true, specs: specs} }
func byRuns(runs ...nameRun) gitCheck   { return gitCheck{content: true, runs: runs} }
func contentIf(c bool, why string) gitCheck {
	if !c {
		return noContent()
	}
	return unplaceable(why)
}

// gitDenial is why rule 2 denies a git command: the subcommand (printed
// only when it is one of git's own, gitLabel), what makes its content
// unsafe or unprovable in fixed words and paths printed by displayPath,
// and one short thing to do instead.
type gitDenial struct {
	sub, why, hint string
}

// message is the denial as a Decision's Message.
func (g gitDenial) message() string {
	return gitLabel(g.sub) + " " + g.why + "; git history cannot be vetted. " + g.hint
}

// gitContent decides rule 2 for one simple command: whether it is git
// printing content that could come from a protected root. args is the
// whole simple command, with any assignments and wrappers before git. It
// returns why the command is denied, and nil otherwise. unknown are git's
// arguments that cannot be told: with any, a subcommand that prints
// content is denied, as one whose pathspecs are unprovable.
func (s *setup) gitContent(args, unknown []string, shapes []argShape) *gitDenial {
	name, rest := program(args)
	if name != "git" {
		return nil
	}
	inv, ok := parseGit(rest, s.cwd)
	if !ok {
		return nil
	}
	if unsafeEnv(args[:len(args)-len(rest)-1]) {
		return &gitDenial{inv.sub, "runs after an assignment (a GIT_ variable, a pager, an editor or LESSOPEN) " +
			"that can change what it prints", "Drop the assignment before git."}
	}
	if slices.Contains(unknown, inv.sub) {
		// The subcommand itself cannot be told; gitLabel never echoes it.
		return &gitDenial{inv.sub, "runs a subcommand the gate cannot expand (a variable or a command substitution), " +
			"whose output cannot be told", hintGitExpand}
	}
	if slices.ContainsFunc(inv.globals, func(g string) bool { return slices.Contains(unknown, g) }) {
		return &gitDenial{inv.sub, "is given a global option the gate cannot expand (a variable or a command substitution), " +
			"which could be any of them", hintGitExpand}
	}
	// The words the line gave after the subcommand end both args and
	// inv.rest; an alias's expansion goes before them.
	given := len(inv.rest)
	inv, why := s.resolveAlias(inv)
	if why != "" {
		return &gitDenial{inv.sub, why, "Run the git command the alias stands for, with the paths limited (`-- src`) or --stat."}
	}
	if inv.unsafe {
		return &gitDenial{inv.sub, inv.unsafeWhy, "Drop that global option, or use --stat."}
	}
	if inv.ambiguous && !gitNeverContent[inv.sub] {
		return &gitDenial{inv.sub, "is given an abbreviated long option that could stand for more than one of its options, " +
			"one of which could print content", "Write each long option out in full."}
	}
	if inv.dashHidden && !gitNeverContent[inv.sub] {
		return &gitDenial{inv.sub, "is given -- right after an option that may take it as its value, " +
			"and a word after it that could then be an option that prints content", "Give the option its value with =, or drop the --."}
	}
	inv.unknown = len(unknown) > 0
	shapeOf := func(i int) argShape {
		fromEnd := len(inv.rest) - i
		j := len(args) - fromEnd
		if fromEnd <= given && j >= 0 && j < len(shapes) {
			return shapes[j]
		}
		return argShape{}
	}
	if why := unknownOption(inv.sub, inv.args, unknown, shapeOf); why != "" && !gitNeverContent[inv.sub] {
		// Before "--" a word that cannot be told could be an option that
		// turns content on: -p, -v, --cc, a stash or reflog subcommand.
		return &gitDenial{inv.sub, why, gitExpandHint(inv.sub)}
	}
	// What an argument the gate cannot expand is, when it names a path.
	whySpec := whyUnknownSpec
	if j := slices.IndexFunc(args, func(a string) bool { return slices.Contains(unknown, a) }); j >= 0 && j < len(shapes) && shapes[j].brace {
		whySpec = "is given an argument the gate cannot expand " + unknownWhat(shapes[j]) +
			", which could name a path in a protected root"
	}
	s.locate(&inv)
	switch inv.place {
	case gitForeign, gitNongit:
		// The protected roots are this repository's: elsewhere only a path
		// the command names can reach them (diff --no-index, grep
		// --no-index).
		if where := s.foreignReads(inv); where != "" {
			return &gitDenial{inv.sub, "runs outside this repository and names a path that " + where, hintGit}
		}
		if inv.unknown && s.gitSpecs(inv).content {
			// An argument that cannot be told could name one.
			return &gitDenial{inv.sub, whySpec, gitExpandHint(inv.sub)}
		}
		return nil
	}
	c := s.gitSpecs(inv)
	switch {
	case !c.content:
		return nil
	case c.deny:
		return &gitDenial{inv.sub, c.why, hintGit}
	case inv.elsewhere:
		return &gitDenial{inv.sub, "is pointed at another tree by --git-dir or --work-tree, which the gate cannot check",
			"Drop --git-dir and --work-tree, or use --stat."}
	case inv.place == gitUnknown:
		return &gitDenial{inv.sub, "runs in a directory the gate cannot place in a work tree (one not made yet, or above the repository)",
			"Run it from inside the repository, with the directory made first."}
	case inv.unknown:
		// Never the name-only query with a word left unexpanded.
		return &gitDenial{inv.sub, whySpec, gitExpandHint(inv.sub)}
	case c.bySpecs && inv.oddSpecs:
		return &gitDenial{inv.sub, "runs with --literal-pathspecs, --glob-pathspecs or --icase-pathspecs, " +
			"which make every pathspec unprovable", "Drop that global option, or use --stat."}
	case c.bySpecs:
		if why := s.specsAvoid(inv, c.specs); why != "" {
			return &gitDenial{inv.sub, why, hintGit}
		}
	}
	for _, r := range c.runs {
		forbidden := ""
		judge := func(p string) bool {
			rel, bad := s.namedForbidden(inv, p, r)
			if bad {
				forbidden = rel
			}
			return !bad
		}
		switch err := s.git().names(inv.dir, r, judge); {
		case err == nil:
		case errors.Is(err, errNamed) && forbidden != "":
			return &gitDenial{inv.sub, "would print content from " + displayPath(forbidden) + ", which " +
				s.whereRel(forbidden), hintGit}
		case errors.Is(err, errNamed):
			return &gitDenial{inv.sub, "would print content from a path above the repository, which holds every protected root", hintGit}
		case errors.Is(err, errGitTimeout):
			return &gitDenial{inv.sub, "could not be checked in time: git took too long to list the files it would print",
				"Limit the revisions or the paths (`-- src`), or use --stat."}
		default:
			return &gitDenial{inv.sub, "could not be checked: git failed to list the files it would print, or its list could not be read",
				hintGit}
		}
	}
	if len(c.runs) > 0 && !c.runs[0].grep && s.submoduleDiff(inv) {
		return &gitDenial{inv.sub, "shows the changes inside submodules (--submodule=diff, or diff.submodule set to diff), " +
			"whose files the name-only check does not list", "Use --submodule=short, or --stat."}
	}
	return nil
}

// submoduleDiff reports whether a diff git prints shows the changes inside
// submodules as a patch: --submodule=diff, or, with no --submodule given,
// diff.submodule set to diff. The name-only check lists the submodule, not
// the files in it. A failed lookup counts.
func (s *setup) submoduleDiff(inv gitInvocation) bool {
	given := ""
	for _, a := range inv.args {
		if a == "--submodule" || strings.HasPrefix(a, "--submodule=") {
			given = strings.TrimPrefix(strings.TrimPrefix(a, "--submodule"), "=")
			if given == "" {
				given = "log"
			}
		}
	}
	if given != "" {
		return given == "diff"
	}
	v, err := s.git().configValue(inv.dir, "diff.submodule", "")
	return !errors.Is(err, errNotSet) && (err != nil || v == "diff")
}

// git is the decision's git session.
func (s *setup) git() *gitSession {
	if s.gitq == nil {
		s.gitq = newGitSession()
	}
	return s.gitq
}

// resolveAlias returns the invocation git runs when inv's subcommand is an
// alias: its expansion, split as git splits it, then inv's arguments. why
// is not "" when that cannot be told, and says which, in fixed words: a
// shell alias ("!"), an alias of an alias, an expansion that does not
// split, git failing to answer, or a name that is neither a built-in
// command nor an alias (an alias defined earlier in the same command line,
// or a git-<name> program). The alias's name is never in why.
func (s *setup) resolveAlias(inv gitInvocation) (gitInvocation, string) {
	const unknown = "runs a subcommand that is neither one of git's own nor an alias the gate can expand " +
		"(an alias defined in the same line, or a git-<name> program), whose output cannot be told"
	if gitBuiltins[inv.sub] || inv.unsafe {
		return inv, ""
	}
	if inv.elsewhere {
		// The alias would come from another repository's configuration.
		return inv, "runs an alias from the configuration of another tree (--git-dir or --work-tree), which the gate cannot read"
	}
	expansion, err := s.git().configValue(inv.dir, "alias."+inv.sub, "")
	switch {
	case err != nil:
		return inv, unknown
	case strings.HasPrefix(expansion, "!"):
		return inv, "runs a shell alias, which runs a command whose output cannot be told"
	}
	words, ok := splitAlias(expansion)
	if !ok || len(words) == 0 {
		return inv, "runs an alias whose expansion the gate cannot split"
	}
	next, ok := inv.parse(slices.Concat(words, inv.rest))
	if !ok || !gitBuiltins[next.sub] {
		return inv, "runs an alias of an alias, or of a subcommand that is not one of git's own, whose output cannot be told"
	}
	return next, ""
}

// splitAlias splits an alias's expansion into words as git does
// (split_cmdline): at whitespace, with single and double quotes and
// backslash escapes. ok is false on an unclosed quote or a trailing
// backslash.
func splitAlias(v string) ([]string, bool) {
	var (
		out     []string
		buf     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	for _, r := range v {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped, inWord = false, true
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				out = append(out, buf.String())
				buf.Reset()
				inWord = false
			}
		default:
			buf.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inWord {
		out = append(out, buf.String())
	}
	return out, true
}

// locate sets where inv runs: in this repository (or one nested in its
// work tree), in another repository, outside every repository, or
// somewhere that cannot be told.
func (s *setup) locate(inv *gitInvocation) {
	inv.place = gitUnknown
	if inv.elsewhere {
		return
	}
	if st, err := os.Stat(inv.dir); err != nil || !st.IsDir() {
		// It may be made earlier in the same command line.
		return
	}
	info, err := s.git().repoAt(inv.dir)
	if err != nil {
		return
	}
	// git prints the paths resolved and, on Windows, with forward
	// slashes; the paths compared with them are resolved too (resolve).
	info.top, info.common = resolveGit(info.top), resolveGit(info.common)
	if info.nongit {
		if !s.holdsRepo(inv.dir) {
			inv.place = gitNongit
		}
		return
	}
	ours, err := s.git().commonDir(s.repo.Root)
	if err != nil {
		return
	}
	if samePath(resolve(info.common), ours, s.fold) {
		inv.place, inv.top = gitHere, info.top
		return
	}
	// Another repository: nested in this one's work tree, holding it, or
	// apart from it.
	where := info.top
	if where == "" {
		where = info.common
	}
	switch r, ok := s.relAny(where); {
	case ok && info.top != "" && r != ".":
		inv.place, inv.top, inv.prefix = gitHere, info.top, r
	case ok:
		// A git directory inside this work tree with no work tree of its
		// own: its paths cannot be placed here.
	case !s.holdsRepo(where):
		inv.place = gitForeign
	}
}

// holdsRepo reports whether the absolute path a is this repository's root
// or a directory above it.
func (s *setup) holdsRepo(a string) bool {
	for _, sp := range []string{a, resolve(a)} {
		if _, above := relTo(sp, s.repo.Root, true); above {
			return true
		}
	}
	return false
}

// relAny is a relative to the repository root, as spelled or with its
// symlinks resolved.
func (s *setup) relAny(a string) (string, bool) {
	if r, ok := s.rel(a); ok {
		return r, true
	}
	return s.rel(resolve(a))
}

// samePath compares two clean absolute paths.
func samePath(a, b string, fold bool) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if fold {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// foreignReads says how a git command run outside this repository names a
// path in it that it could print, "" when it names none: any word, or the
// value of a --flag=value, that is a protected root, is inside or contains
// one (whereAbs, which names the root and never the word).
func (s *setup) foreignReads(inv gitInvocation) string {
	for _, w := range inv.rest {
		if w == "--" || w == "-" {
			continue
		}
		if strings.HasPrefix(w, "-") {
			_, v, ok := strings.Cut(w, "=")
			if !ok {
				continue
			}
			w = v
		}
		if i := strings.IndexAny(w, "*?["); i >= 0 {
			w = path.Dir(w[:i] + "x")
		}
		for _, a := range spellings(inv.dir, w) {
			if s.touchesAbs(a) {
				return s.whereAbs(a)
			}
		}
		for _, r := range s.relsOf(spellings(inv.dir, w)) {
			if s.forbiddenRel(r) {
				return s.whereRel(r)
			}
		}
	}
	return ""
}

// forbiddenRel reports whether content from the repository-relative path
// rel may not be printed by git: rel is a protected root, is inside one or
// contains one, a nested work tree's roots included (local state lives
// there too). The files that control the gate (the policy, the baseline,
// Claude Code's settings) are readable, so git may print them as a Read
// may; only writing them is denied.
func (s *setup) forbiddenRel(rel string) bool { return s.touchesRel(rel) }

// namedForbidden judges one path a name-only run printed. git prints it
// relative to the top of the work tree it runs in; grep prints it with a
// revision as "REV:path", and grep --no-index and diff comparing files
// outside the work tree relative to the directory they run in (r.dirRel).
// Every reading is judged. When one is forbidden it returns that reading,
// repository-relative ("" for a path above the repository): a file's path
// from the tree, never a word of the command.
func (s *setup) namedForbidden(inv gitInvocation, p string, r nameRun) (string, bool) {
	cands := []string{p}
	if r.grep {
		for i := range len(p) {
			if p[i] != ':' {
				continue
			}
			rest := p[i+1:]
			cands = append(cands, rest)
			if tree, ok := revPath(p[:i]); ok {
				cands = append(cands, path.Join(tree, rest))
			}
		}
	}
	for _, c := range cands {
		if filepath.IsAbs(c) {
			if rel, bad := s.absForbidden(c); bad {
				return rel, true
			}
			continue
		}
		clean := path.Clean(c)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			if inv.top == "" {
				return "", true
			}
			if rel, bad := s.absForbidden(filepath.Join(inv.top, filepath.FromSlash(c))); bad {
				return rel, true
			}
		} else if rel := path.Join(inv.prefix, clean); s.forbiddenRel(rel) {
			return rel, true
		}
		if r.dirRel {
			if rel, bad := s.absForbidden(abs(inv.dir, c)); bad {
				return rel, true
			}
		}
	}
	return "", false
}

// absForbidden is forbiddenRel for an absolute path, as spelled and with
// symlinks resolved; a path outside the repository is allowed, and one
// above it holds every root. It returns the path, repository-relative, ""
// for one above the repository.
func (s *setup) absForbidden(a string) (string, bool) {
	for _, sp := range []string{a, resolve(a)} {
		if s.touchesAbs(sp) {
			rel, _ := relTo(s.repo.Root, sp, true)
			return rel, true
		}
	}
	for _, rel := range s.relsOf([]string{a, resolve(a)}) {
		if s.forbiddenRel(rel) {
			return rel, true
		}
	}
	return "", false
}

// gitSpecs says whether inv prints content and, if it does, how to place
// it.
func (s *setup) gitSpecs(inv gitInvocation) gitCheck {
	patch := hasFlag(inv.args, gitPatchFlags)
	after := func() []pathspec {
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return unprovable(whyFromFile)
		}
		return s.parseSpecs(inv, inv.specs)
	}
	switch inv.sub {
	case "blame", "annotate":
		return bySpecs(s.blameSpecs(inv))
	case "diff":
		if hasFlag(inv.args, gitSummaryFlags) && !patch {
			return noContent()
		}
		return s.diffCheck(inv)
	case "diff-tree":
		if hasFlag(inv.args, []string{"--stdin"}) {
			return unplaceable("with --stdin reads revisions the gate cannot see")
		}
		if !patch {
			return noContent()
		}
		extra := []string{"-r", "--no-commit-id"}
		if hasFlag(inv.args, []string{"-c", "--cc"}) {
			extra = append(extra, "--combined-all-paths")
		}
		return byRuns(s.diffRun(inv, []string{"diff-tree"}, extra, inv.args, nil))
	case "diff-index", "diff-files":
		if !patch {
			return noContent()
		}
		return byRuns(s.diffRun(inv, []string{inv.sub}, nil, inv.args, nil))
	case "show":
		return s.showCheck(inv, patch)
	case "log", "whatchanged":
		if !patch {
			return noContent()
		}
		return s.logCheck(inv, []string{inv.sub}, inv.args)
	case "reflog":
		return s.reflogCheck(inv)
	case "stash":
		return s.stashCheck(inv)
	case "grep":
		return s.grepCheck(inv)
	case "status":
		if shortFormat(inv.args, true) {
			// The short and porcelain formats print no diff with -v.
			return noContent()
		}
		return s.verboseCheck(inv, verbosity(inv.args, "u"), false)
	case "add", "stage":
		if !hasShort(inv.args, 'p', "--patch", "") && !hasShort(inv.args, 'i', "--interactive", "") {
			return noContent()
		}
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return bySpecs(unprovable(whyFromFile))
		}
		vals := map[string]bool{"--chmod": true}
		return bySpecs(s.parseSpecs(inv, slices.Concat(positionalOf(inv.args, vals), inv.specs)))
	case "restore":
		if !hasShort(inv.args, 'p', "--patch", "s") {
			return noContent()
		}
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return bySpecs(unprovable(whyFromFile))
		}
		vals := map[string]bool{"-s": true, "--source": true, "--conflict": true}
		return bySpecs(s.parseSpecs(inv, slices.Concat(positionalOf(inv.args, vals), inv.specs)))
	case "checkout", "reset":
		if !hasShort(inv.args, 'p', "--patch", "bB") {
			return noContent()
		}
		return bySpecs(s.treeishSpecs(inv))
	case "commit":
		if hasShort(inv.args, 'p', "--patch", "mFCcSt") || hasFlag(inv.args, []string{"--interactive"}) {
			return bySpecs(after())
		}
		if hasFlag(inv.args, []string{"--dry-run", "--short", "--porcelain", "--long"}) {
			// The dry run (which --short, --porcelain and --long imply)
			// prints the diff that would be committed: with -a, -o or -i,
			// unstaged changes too.
			v := s.commitVerbosity(inv)
			c := s.verboseCheck(inv, v, true)
			if v > 0 && hasFlag(inv.args, []string{"--amend"}) {
				// Amending commits the index on HEAD's parent: the diff is
				// from there. A root commit has none, and that run fails,
				// which denies.
				c.runs = append(c.runs, nameRun{argv: slices.Concat(inv.globals, []string{"diff", "--cached", "HEAD^"}, gitNameOnly)})
			}
			return c
		}
		return noContent()
	case "rebase", "am":
		if hasFlag(inv.args, []string{"--show-current-patch"}) ||
			(inv.sub == "rebase" && hasShort(inv.args, 'x', "--exec", "sSXo")) {
			return unplaceable("with --show-current-patch or --exec prints a patch, or runs a command, whose content cannot be placed")
		}
		return noContent()
	case "request-pull":
		return contentIf(hasShort(inv.args, 'p', "", ""), "with -p prints a patch whose files cannot be placed")
	case "bundle":
		return contentIf(bundleToOutput(inv.args), "writes a bundle to standard output or a device, printing objects whose files cannot be placed")
	case "merge-tree":
		// The trivial merge prints the merged content; --write-tree, the
		// default for two commits, prints object names.
		trivial := hasFlag(inv.args, []string{"--trivial-merge"}) ||
			(!hasFlag(inv.args, []string{"--write-tree"}) && len(positional(inv.args)) >= 3)
		return contentIf(trivial, "prints merged file content, whose files cannot be placed")
	case "merge-file":
		return contentIf(hasFlag(inv.args, []string{"--object-id"}), "with --object-id prints merged content of objects whose files cannot be placed")
	case "submodule":
		return contentIf(slices.Contains(positional(inv.args), "foreach"), "runs a command in each submodule, whose output cannot be told")
	case "bisect":
		return contentIf(len(inv.args) > 0 && inv.args[0] == "run", "runs a command whose output cannot be told")
	case "cat-file":
		return s.catFileSpecs(inv)
	}
	if gitUnplaceable[inv.sub] {
		return unplaceable("prints objects whose files cannot be placed")
	}
	if gitAlwaysContent[inv.sub] {
		return bySpecs(after())
	}
	return noContent()
}

// filterDiffArgs is args without the options that only change how a diff
// is printed (gitStripNames, "-U<n>", "-X<param>"), and their values;
// keep names options to leave in place (stash show's -u).
func filterDiffArgs(args []string, keep ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, hasValue := strings.Cut(a, "=")
		switch {
		case slices.Contains(keep, a):
		case gitStripNames[name]:
			if gitStripNext[name] && !hasValue {
				i++
			}
			continue
		case strings.HasPrefix(a, "-U") || strings.HasPrefix(a, "-X"):
			continue
		}
		out = append(out, a)
	}
	return out
}

// diffRun is the name-only form of a diff, log or show: the global
// options, head (the subcommand and any of its own subcommand), the
// name-only options, extra, args without their printing options, and the
// pathspecs after "--". keep is passed to filterDiffArgs.
func (s *setup) diffRun(inv gitInvocation, head, extra, args, keep []string) nameRun {
	argv := slices.Concat(inv.globals, head, gitNameOnly, extra, filterDiffArgs(args, keep...))
	if inv.dashdash {
		argv = append(append(argv, "--"), inv.specs...)
	}
	return nameRun{argv: argv, combined: slices.Contains(extra, "--combined-all-paths")}
}

// diffCheck judges git diff. With --no-index, or two paths of which one is
// outside the work tree (git then compares the two files, wherever they
// are), the paths it names are judged; otherwise its name-only form is.
func (s *setup) diffCheck(inv gitInvocation) gitCheck {
	noIndex := hasFlag(inv.args, []string{"--no-index"})
	outside := noIndex || s.namesOutside(inv)
	if outside {
		if s.foreignReads(inv) != "" || s.namesDir(inv) {
			return unplaceable("compares files outside the index, and names a path in a protected root or a directory of the repository")
		}
		if noIndex {
			return noContent()
		}
	}
	r := s.diffRun(inv, []string{"diff"}, []string{"-c", "--combined-all-paths"}, inv.args, nil)
	r.dirRel = outside
	return byRuns(r)
}

// namesOutside reports whether a word of inv, taken as a path, is an
// existing file or directory outside the work tree inv runs in. Like git,
// it compares each spelling with its symlinks resolved: a path spelled
// through a link to the work tree (macOS's /var for /private/var) is
// inside it, and a link inside the work tree that points out of it is not.
func (s *setup) namesOutside(inv gitInvocation) bool {
	for _, w := range inv.rest {
		if w == "--" || strings.HasPrefix(w, "-") {
			continue
		}
		for _, a := range spellings(inv.dir, w) {
			if _, err := os.Stat(a); err != nil {
				continue
			}
			if _, in := relTo(inv.top, resolve(a), s.fold); inv.top == "" || !in {
				return true
			}
		}
	}
	return false
}

// namesDir reports whether a word of inv, taken as a path, is a directory
// inside this repository: diff --no-index would compare every file under
// it.
func (s *setup) namesDir(inv gitInvocation) bool {
	for _, w := range inv.rest {
		if w == "--" || strings.HasPrefix(w, "-") {
			continue
		}
		for _, a := range spellings(inv.dir, w) {
			if st, err := os.Stat(a); err == nil && st.IsDir() {
				if _, ok := s.relAny(a); ok {
					return true
				}
			}
		}
	}
	return false
}

// mergeFlags are the options that choose how log and show print a merge.
var mergeFlags = []string{"-c", "--cc", "-m", "--dd", "--diff-merges", "--remerge-diff", "--no-diff-merges"}

// mergeRisk says how a log-like command prints merges. remerge is true
// when it may print a remerge diff, whose content cannot be placed by
// names: --remerge-diff, --diff-merges=remerge, or -m with
// log.diffMerges set to remerge (or a setting that cannot be read).
func (s *setup) mergeRisk(inv gitInvocation, args []string) (remerge bool) {
	on := false
	for i, a := range args {
		name, v, hasValue := strings.Cut(a, "=")
		if name == "--diff-merges" && !hasValue && i+1 < len(args) {
			v, hasValue = args[i+1], true
		}
		switch {
		case a == "--remerge-diff":
			return true
		case name == "--diff-merges" && hasValue && (v == "remerge" || v == "r"):
			return true
		case a == "-m", name == "--diff-merges" && hasValue && (v == "on" || v == "m"):
			on = true
		}
	}
	if !on {
		return false
	}
	v, err := s.git().configValue(inv.dir, "log.diffMerges", "")
	if errors.Is(err, errNotSet) {
		return false
	}
	return err != nil || v == "remerge" || v == "r"
}

// logCheck judges a log-like command printing patches: log, whatchanged,
// reflog show, stash list. -L follows lines through history and has no
// name-only form; --stdin reads revisions the gate cannot see.
func (s *setup) logCheck(inv gitInvocation, head, args []string) gitCheck {
	if hasFlag(args, []string{"-L", "--stdin"}) || s.mergeRisk(inv, args) {
		return unplaceable("with -L, --stdin or a remerge diff prints content that has no name-only form, so it cannot be placed")
	}
	extra := []string{"--format="}
	if hasFlag(args, mergeFlags) {
		// A combined diff of a merge prints a parent's file under the
		// merge's path when it was renamed: name every parent's path.
		extra = append(extra, "--cc", "--combined-all-paths")
	}
	return byRuns(s.diffRun(inv, head, extra, args, nil))
}

// reflogCheck judges git reflog: reflog show (the default) is log.
func (s *setup) reflogCheck(inv gitInvocation) gitCheck {
	head, args := []string{"reflog"}, inv.args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] != "show" {
			return noContent() // expire, delete, exists, drop, list
		}
		head, args = []string{"reflog", "show"}, args[1:]
	}
	if !hasFlag(args, gitPatchFlags) {
		return noContent()
	}
	return s.logCheck(inv, head, args)
}

// showCheck judges git show. Its revisions (the positional arguments up
// to the first that is not one, which with the rest are paths) are looked
// up in the repository: a commit prints a diff (unless a summary flag is
// given), judged by its name-only form; a blob or a tree prints itself, a
// tag prints its target, and an object that cannot be resolved, or is
// named by searching commit messages, cannot be placed.
func (s *setup) showCheck(inv gitInvocation, patch bool) gitCheck {
	if hasFlag(inv.args, gitRefSets) || s.mergeRisk(inv, inv.args) {
		return unplaceable("is given a ref set (--all, --branches and the like) or a remerge diff, whose content cannot be placed")
	}
	diff := !hasFlag(inv.args, gitSummaryFlags) || patch
	var blobs []string
	commit, revs := false, 0
	args := slices.Clone(inv.args)
	drop := map[int]bool{}
	for _, i := range positionalIndex(inv.args, gitRevValueOpts) {
		a := inv.args[i]
		if revs < 0 {
			continue
		}
		kind, p := s.kindOf(inv, a)
		if kind == revUnplaceable && !strings.Contains(a, ":") && s.isPath(inv, a) {
			revs = -1 // a path: it and every positional after it
			continue
		}
		revs++
		switch kind {
		case revBlobPath:
			blobs = append(blobs, p)
			drop[i] = true
		case revTag:
			// The name-only form shows the commit, without the tag's
			// header.
			args[i] = a + "^{commit}"
			commit = true
		case revCommit:
			commit = true
		default:
			return unplaceable("is given a revision that is not a commit (a blob, a tree, a search of commit messages, or a name that does not resolve), whose content cannot be placed")
		}
	}
	if revs <= 0 {
		commit = true // HEAD
	}
	c := gitCheck{content: len(blobs) > 0, bySpecs: len(blobs) > 0, specs: s.blobSpecs(inv, blobs)}
	if diff && commit {
		var kept []string
		for i, a := range args {
			if !drop[i] {
				kept = append(kept, a)
			}
		}
		// show prints a merge as a combined diff, unless --first-parent
		// alone makes it a diff against the first parent: name every
		// parent's path, as logCheck does.
		extra := []string{"--format="}
		if !hasFlag(inv.args, []string{"--first-parent"}) || hasFlag(inv.args, mergeFlags) {
			extra = append(extra, "--cc", "--combined-all-paths")
		}
		c.content = true
		c.runs = append(c.runs, s.diffRun(inv, []string{"show"}, extra, kept, nil))
	}
	return c
}

// isPath reports whether a, relative to inv's directory, exists.
func (s *setup) isPath(inv gitInvocation, a string) bool {
	_, err := os.Lstat(abs(inv.dir, a))
	return err == nil
}

// revKind is what a revision argument names.
type revKind int

const (
	revUnplaceable revKind = iota
	revCommit              // a commit, or a range of commits
	revTag                 // an annotated tag of a commit
	revBlobPath            // REV:path or :path, with the path returned
)

// kindOf is what a revision argument of show names.
func (s *setup) kindOf(inv gitInvocation, a string) (revKind, string) {
	if strings.Contains(a, ":/") || strings.Contains(a, "^{/") {
		return revUnplaceable, "" // a search of commit messages
	}
	if p, ok := revPath(a); ok {
		return revBlobPath, p
	}
	if inv.elsewhere || inv.unknown || inv.place != gitHere {
		return revUnplaceable, ""
	}
	g := s.git()
	if left, right, ok := cutRange(a); ok {
		for _, side := range []string{left, right} {
			if side != "" && g.objectType(inv.dir, side+"^{commit}") != "commit" {
				return revUnplaceable, ""
			}
		}
		return revCommit, ""
	}
	if strings.HasPrefix(a, "^") {
		if g.objectType(inv.dir, a[1:]+"^{commit}") == "commit" {
			return revCommit, ""
		}
		return revUnplaceable, ""
	}
	switch g.objectType(inv.dir, a) {
	case "commit":
		return revCommit, ""
	case "tag":
		// An annotated tag prints its target: a commit's diff is judged as
		// a commit's, anything else is content.
		if g.objectType(inv.dir, a+"^{}") == "commit" {
			return revTag, ""
		}
	}
	return revUnplaceable, ""
}

// cutRange splits A..B and A...B.
func cutRange(a string) (left, right string, ok bool) {
	if l, r, found := strings.Cut(a, "..."); found {
		return l, r, true
	}
	return strings.Cut(a, "..")
}

// stashCheck judges git stash. stash show prints a diff with a patch flag
// (its -u is --include-untracked), or with none at all when
// stash.showPatch is true; stash list prints each stash's patch with a
// patch flag; stash -p and stash push -p print hunks.
func (s *setup) stashCheck(inv gitInvocation) gitCheck {
	sub, args := "push", inv.args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "show":
		patchFlags := slices.DeleteFunc(slices.Clone(gitPatchFlags), func(f string) bool { return f == "-u" })
		if !hasFlag(args, patchFlags) {
			if hasFlag(args, gitSummaryFlags) {
				return noContent()
			}
			v, err := s.git().configValue(inv.dir, "stash.showPatch", "bool")
			if errors.Is(err, errNotSet) || (err == nil && v == "false") {
				return noContent()
			}
		}
		if s.stashAbsent(inv, args) {
			return noContent()
		}
		return byRuns(s.diffRun(inv, []string{"stash", "show"}, nil, args, []string{"-u"}))
	case "list":
		if !hasFlag(args, gitPatchFlags) {
			return noContent()
		}
		return s.logCheck(inv, []string{"stash", "list"}, args)
	case "push", "save":
		if !hasShort(args, 'p', "--patch", "m") {
			return noContent()
		}
		return bySpecs(s.parseSpecs(inv, inv.specs))
	}
	return noContent()
}

// stashAbsent reports whether git stash show (args are its words after
// "show", before any "--") names a stash that does not exist: it then
// fails before it prints anything. stash show takes no pathspec: every
// word not starting with "-", before or after "--", names the stash, a
// number n meaning refs/stash@{n}, and with none it shows refs/stash@{0}.
// The stash is absent only when git rev-parse --verify --quiet exits 1
// with no output, git's answer that the name resolves to nothing. Any other
// answer, more than one word, or a repository the gate cannot ask about
// leaves the decision to the name-only run, which fails closed.
func (s *setup) stashAbsent(inv gitInvocation, args []string) bool {
	if inv.elsewhere || inv.unknown || inv.place != gitHere {
		return false
	}
	var words []string
	for _, w := range slices.Concat(args, inv.specs) {
		if !strings.HasPrefix(w, "-") {
			words = append(words, w)
		}
	}
	rev := "refs/stash@{0}"
	switch {
	case len(words) > 1:
		return false
	case len(words) == 1 && strings.Trim(words[0], "0123456789") == "":
		rev = "refs/stash@{" + words[0] + "}"
	case len(words) == 1:
		rev = words[0]
	}
	out, exit, err := s.git().output(inv.dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev)
	return err == nil && exit == 1 && out == ""
}

// grepLongValues are git grep's long options that take the next word.
var grepLongValues = map[string]bool{
	"--max-depth": true, "--threads": true, "--context": true, "--after-context": true,
	"--before-context": true, "--max-count": true,
}

// grepCheck judges git grep. With -l, -L, -c or -q it prints names,
// counts or nothing. Otherwise its name-only form (-l -z) names the files
// it would print lines from. -O runs a pager on the files; -f - reads
// patterns from standard input, which the gate cannot see.
func (s *setup) grepCheck(inv gitInvocation) gitCheck {
	names, negated := false, false
	for i := 0; i < len(inv.args); i++ {
		a := inv.args[i]
		name, _, hasValue := strings.Cut(a, "=")
		switch {
		case len(a) < 2 || a[0] != '-':
		case strings.HasPrefix(a, "--"):
			switch name {
			case "--files-with-matches", "--name-only", "--files-without-match", "--count", "--quiet":
				names = true
			case "--no-files-with-matches", "--no-name-only", "--no-files-without-match", "--no-count",
				"--no-quiet", "--no-null", "--no-full-name":
				negated = true
			case "--open-files-in-pager":
				return unplaceable("with -O opens the files it matches in a pager")
			}
			if grepLongValues[name] && !hasValue {
				i++
			}
		default:
			for j := 1; j < len(a); j++ {
				c := a[j]
				switch {
				case c == 'O':
					return unplaceable("with -O opens the files it matches in a pager")
				case strings.IndexByte("lLcq", c) >= 0:
					names = true
				case strings.IndexByte("ABCefm", c) >= 0:
					if c == 'f' && (a[j+1:] == "-" || (j == len(a)-1 && i+1 < len(inv.args) && inv.args[i+1] == "-")) {
						return unplaceable("with -f - reads patterns from standard input, which the gate cannot see")
					}
					if j == len(a)-1 {
						i++
					}
					j = len(a)
				}
			}
		}
	}
	switch {
	case negated:
		return unplaceable("negates -l, -c or -q, so it may print the lines it matches")
	case names:
		return noContent()
	}
	var args []string
	for _, a := range inv.args {
		name, _, _ := strings.Cut(a, "=")
		if name == "--color" || name == "--no-color" || name == "--textconv" {
			continue
		}
		args = append(args, a)
	}
	argv := slices.Concat(inv.globals, []string{"grep", "-l", "-z", "--full-name", "--no-color", "--no-textconv"}, args)
	if inv.dashdash {
		argv = append(append(argv, "--"), inv.specs...)
	}
	return byRuns(nameRun{argv: argv, grep: true, dirRel: hasFlag(inv.args, []string{"--no-index"})})
}

// verbosity counts -v and --verbose in args (--no-verbose resets it). A
// cluster of short options ends at a letter of withValue.
func verbosity(args []string, withValue string) int {
	n := 0
	for _, a := range args {
		switch {
		case a == "--verbose":
			n++
		case a == "--no-verbose":
			n = 0
		case len(a) > 1 && a[0] == '-' && a[1] != '-':
			for j := 1; j < len(a); j++ {
				if a[j] == 'v' {
					n++
				}
				if strings.IndexByte(withValue, a[j]) >= 0 {
					break
				}
			}
		}
	}
	return n
}

// gitIndexChangers are git's subcommands that change the index or HEAD
// (as well as those that change the working tree, gitTreeChangers), each
// with the first words that make it not do so.
var gitIndexChangers = map[string][]string{
	"add": nil, "rm": nil, "mv": nil, "commit": nil, "cherry-pick": nil, "revert": nil,
	"update-index": nil, "read-tree": nil, "reset": nil, "restore": nil, "update-ref": nil,
	"merge": nil, "rebase": nil, "pull": nil, "am": nil, "checkout": nil, "switch": nil,
	"stash": {"list", "show"}, "notes": {"list", "show"}, "apply": nil,
}

// indexChangeOf names the git command argv runs when it changes the index
// or HEAD: git's own changers (gitIndexChangers; apply only with --index,
// --cached or --3way, else as a tree change), every tree change
// (gitChangesTree), and a subcommand that is not git's own. "" otherwise;
// creating a branch at HEAD changes nothing a later command prints.
func (s *setup) indexChangeOf(argv []string, dir string) string {
	name, rest := program(argv)
	if name != "git" {
		return ""
	}
	inv, ok := parseGit(rest, dir)
	switch {
	case !ok:
		return ""
	case !gitBuiltins[inv.sub] || inv.ambiguous:
		return gitLabel(inv.sub)
	case s.branchOnly(inv):
		return ""
	case gitChangesTree(inv.sub, inv.rest):
		return "git " + inv.sub
	}
	except, ok := gitIndexChangers[inv.sub]
	if !ok {
		return ""
	}
	if inv.sub == "apply" && !hasFlag(inv.args, []string{"--index", "--cached", "--3way"}) {
		return ""
	}
	if len(inv.args) > 0 && slices.Contains(except, inv.args[0]) {
		return ""
	}
	return "git " + inv.sub
}

// contentCapable reports whether the git command argv, run from dir, could
// print file content given its arguments: what rule 2 would check (its
// content as gitSpecs reads it), whatever the files in it. A subcommand
// that prints none whatever its options, or a git command whose arguments
// turn none on (git commit -m msg, git status, git log --oneline), is not.
func (s *setup) contentCapable(argv []string, dir string) bool {
	name, rest := program(argv)
	if name != "git" {
		return false
	}
	inv, ok := parseGit(rest, dir)
	if !ok || gitNeverContent[inv.sub] {
		return false
	}
	inv, why := s.resolveAlias(inv)
	if why != "" || inv.unsafe || inv.ambiguous || inv.dashHidden {
		return true
	}
	if gitNeverContent[inv.sub] {
		return false
	}
	s.locate(&inv)
	// What git holds will have changed: a stash looked up now may not
	// exist yet, so nothing is taken to be absent.
	inv.unknown = true
	return s.gitSpecs(inv).content
}

// shortFormat reports whether git status (status true) or git commit's
// dry run prints the short or porcelain format, or NUL-terminated output,
// none of which shows a diff whatever -v says (checked with git 2.53).
// The options are read in order, as git reads them: --short (status -s)
// and --porcelain[=v1|v2] set a short format, --long the long one, and a
// negation (--no-short, --no-porcelain, --no-long) none; -z or --null
// turns NUL-terminated output on and --no-null off. Short options are read
// in clusters up to a letter that takes a value (status -u<mode>, commit
// -m, -F, -c, -C, -t, -S, -u), whose rest is that value. With nothing
// given it is the long format: a configured status.short is not read, so
// the gate errs towards the diff.
func shortFormat(args []string, status bool) bool {
	short, null := false, false
	stopAt := "mFcCtSu" // commit's letters that take a value
	if status {
		stopAt = "u"
	}
	for _, a := range args {
		name, _, _ := strings.Cut(a, "=")
		switch name {
		case "--short", "--porcelain":
			short = true
			continue
		case "--long", "--no-short", "--no-porcelain", "--no-long":
			short = false
			continue
		case "--null":
			null = true
			continue
		case "--no-null":
			null = false
			continue
		}
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte(stopAt, c) >= 0 {
				break
			}
			switch {
			case c == 's' && status:
				short = true
			case c == 'z':
				null = true
			}
		}
	}
	return short || null
}

// commitVerbosity is how verbose git commit is: -v counted, or with no
// -v, --verbose or --no-verbose given, commit.verbose (a boolean or a
// level), as git reads it. A value git cannot read as a boolean, or a
// failed lookup, counts as verbose.
func (s *setup) commitVerbosity(inv gitInvocation) int {
	if shortFormat(inv.args, false) {
		// The short and porcelain formats print no diff with -v.
		return 0
	}
	if hasFlag(inv.args, []string{"--verbose", "--no-verbose"}) || hasShort(inv.args, 'v', "", "mFCcSt") {
		return verbosity(inv.args, "mFCcSt")
	}
	v, err := s.git().configValue(inv.dir, "commit.verbose", "bool")
	if errors.Is(err, errNotSet) || (err == nil && v == "false") {
		return 0
	}
	return 1
}

// verboseCheck judges git status -v, which prints the staged diff (and
// with -vv the unstaged one too), and commit --dry-run -v, which prints
// what would be committed: both, here. Each diff is judged by its
// name-only form.
func (s *setup) verboseCheck(inv gitInvocation, v int, both bool) gitCheck {
	if v == 0 {
		return noContent()
	}
	runs := []nameRun{{argv: slices.Concat(inv.globals, []string{"diff", "--cached"}, gitNameOnly)}}
	if v >= 2 || both {
		runs = append(runs, nameRun{argv: slices.Concat(inv.globals, []string{"diff"}, gitNameOnly)})
	}
	return byRuns(runs...)
}

// treeishSpecs are the paths checkout -p and reset -p show hunks of: the
// positional arguments, but a first one that names a tree-ish (when it
// also names a file, git cannot tell which it is, nor can the gate), then
// those after "--".
func (s *setup) treeishSpecs(inv gitInvocation) []pathspec {
	if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
		return unprovable(whyFromFile)
	}
	vals := map[string]bool{"-b": true, "-B": true, "--orphan": true, "--conflict": true}
	pos := positionalOf(inv.args, vals)
	if len(pos) > 0 && s.git().objectType(inv.dir, pos[0]+"^{tree}") == "tree" {
		if !inv.dashdash && s.isPath(inv, pos[0]) {
			return unprovable("a first argument that names both a tree-ish and a file")
		}
		pos = pos[1:]
	}
	return s.parseSpecs(inv, slices.Concat(pos, inv.specs))
}

// blameSpecs are the paths blame and annotate can read: every positional
// argument, before "--" or after it, and the value of --contents. git takes
// the file before or after the revision, so each one may be it.
func (s *setup) blameSpecs(inv gitInvocation) []pathspec {
	var paths []string
	for i := 0; i < len(inv.args); i++ {
		switch a := inv.args[i]; {
		case a == "-L":
			i++ // a line range, not a file
		case !strings.HasPrefix(a, "-"):
			// A file, a revision, or the value of --contents, -S or
			// --ignore-revs-file, which are files too.
			paths = append(paths, a)
		case strings.HasPrefix(a, "--contents="), strings.HasPrefix(a, "--ignore-revs-file="):
			_, v, _ := strings.Cut(a, "=")
			paths = append(paths, v)
		}
	}
	paths = append(paths, inv.specs...)
	if len(paths) == 0 {
		return nil
	}
	return s.literalSpecs(inv, paths)
}

// bundleToOutput reports whether args, after "bundle", create a bundle on
// standard output ("-") or a device, where its objects are printed.
func bundleToOutput(args []string) bool {
	if len(args) == 0 || args[0] != "create" {
		return false
	}
	for _, a := range args[1:] {
		if a == "-" || !strings.HasPrefix(a, "-") {
			return a == "-" || strings.HasPrefix(a, "/dev/")
		}
	}
	return false
}

// catFileSpecs judges git cat-file: every mode but -t, -s, -e and
// --batch-check prints an object.
func (s *setup) catFileSpecs(inv gitInvocation) gitCheck {
	if hasFlag(inv.args, gitCatFileSafe) {
		return noContent()
	}
	var blobs []string
	for _, a := range positional(inv.args) {
		switch a {
		case "blob", "tree", "commit", "tag":
			continue
		}
		if strings.Contains(a, ":/") || strings.Contains(a, "^{/") {
			return unplaceable("is given a search of commit messages (:/ or ^{/), whose content cannot be placed")
		}
		p, ok := revPath(a)
		if !ok {
			// An object name: what it holds cannot be told.
			return unplaceable("is given an object name, whose content cannot be placed")
		}
		blobs = append(blobs, p)
	}
	if len(blobs) == 0 {
		// --batch reads object names from standard input.
		return unplaceable("with --batch reads object names from standard input, which the gate cannot see")
	}
	return bySpecs(s.blobSpecs(inv, blobs))
}

// revPath splits REV:path, as show and cat-file take it, and returns the
// path. ":path" and ":N:path" name the index. The path starts after the
// first ":" outside braces: HEAD@{10:00}:x is the path x at a time.
func revPath(a string) (string, bool) {
	if strings.HasPrefix(a, ":") {
		rest := a[1:]
		if len(rest) >= 2 && isDigit(rune(rest[0])) && rest[1] == ':' {
			rest = rest[2:]
		}
		return rest, true
	}
	depth := 0
	for i := range len(a) {
		switch a[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return a[i+1:], true
			}
		}
	}
	return "", false
}

// blobSpecs places REV:path paths: relative to the top of the work tree,
// unless they start with ./ or ../, which are relative to inv's directory.
func (s *setup) blobSpecs(inv gitInvocation, paths []string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		if p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
			out = append(out, s.place(inv, abs(inv.dir, p), false))
			continue
		}
		clean := path.Clean(p)
		if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			out = append(out, pathspec{unprovable: true, why: whyOutside})
			continue
		}
		out = append(out, pathspec{rel: path.Join(inv.prefix, clean)})
	}
	return out
}

// literalSpecs places file paths that git takes literally, relative to
// inv's directory.
func (s *setup) literalSpecs(inv gitInvocation, paths []string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		out = append(out, s.place(inv, abs(inv.dir, p), false))
	}
	return out
}

// parseSpecs reads pathspecs with their magic (gitglossary, "pathspec"):
// ":!x", ":^x" and ":(exclude)x" exclude; ":/x" and ":(top)x" start from
// the top of the work tree; ":(literal)" turns globs off. Other magic, and
// a glob, make a pathspec unprovable.
func (s *setup) parseSpecs(inv gitInvocation, specs []string) []pathspec {
	out := make([]pathspec, 0, len(specs))
	for _, raw := range specs {
		exclude, top, literal, odd := false, false, false, false
		p := raw
		switch {
		case strings.HasPrefix(p, ":("):
			end := strings.Index(p, ")")
			if end < 0 {
				out = append(out, pathspec{unprovable: true, why: "a pathspec whose magic is not closed"})
				continue
			}
			for m := range strings.SplitSeq(p[2:end], ",") {
				switch m {
				case "exclude":
					exclude = true
				case "top":
					top = true
				case "literal":
					literal = true
				default: // glob, icase, attr:...
					odd = true
				}
			}
			p = p[end+1:]
		case strings.HasPrefix(p, ":"):
			i := 1
			for i < len(p) && strings.ContainsRune("/!^", rune(p[i])) {
				switch p[i] {
				case '/':
					top = true
				default:
					exclude = true
				}
				i++
			}
			if i < len(p) && p[i] == ':' {
				i++
			}
			p = p[i:]
		}
		if odd || (!literal && hasGlob(p)) {
			out = append(out, pathspec{exclude: exclude, unprovable: true, why: "a pathspec with a glob, or magic such as icase or attr, that only git can expand"})
			continue
		}
		a := abs(inv.dir, p)
		if top {
			if inv.top == "" {
				out = append(out, pathspec{exclude: exclude, unprovable: true, why: "a pathspec from the top of a work tree the gate cannot find"})
				continue
			}
			a = filepath.Join(inv.top, filepath.FromSlash(p))
		}
		out = append(out, s.place(inv, a, exclude))
	}
	return out
}

// place makes a pathspec of the absolute path a: relative to the top of
// the work tree inv runs in, then to this repository's root.
func (s *setup) place(inv gitInvocation, a string, exclude bool) pathspec {
	if inv.top == "" {
		return pathspec{exclude: exclude, unprovable: true, why: "a path the gate cannot place in a work tree"}
	}
	r, ok := relTo(inv.top, a, s.fold)
	if !ok {
		r, ok = relTo(inv.top, resolve(a), s.fold)
	}
	if !ok {
		return pathspec{exclude: exclude, unprovable: true, why: whyOutside}
	}
	return pathspec{rel: path.Join(inv.prefix, path.Clean(r)), exclude: exclude}
}

// specsAvoid is rootsReached for inv's pathspecs and the protected roots:
// "" when the specs provably avoid every root, and otherwise why not. In a
// repository nested in this one's work tree the roots apply relative to it
// as well (frames): the specs are judged again from its top, against its
// roots.
func (s *setup) specsAvoid(inv gitInvocation, specs []pathspec) string {
	if why := rootsReached(s.policy.ProtectedRoots(), "", specs); why != "" {
		return why
	}
	if inv.prefix == "" {
		return ""
	}
	fs := s.frames(inv.prefix)
	f := fs[len(fs)-1]
	if f.sub != "." {
		return ""
	}
	inner := make([]pathspec, 0, len(specs))
	for _, sp := range specs {
		if !sp.unprovable {
			r, ok := relTo(inv.prefix, sp.rel, false)
			if !ok {
				continue // outside the nested work tree
			}
			sp.rel = r
		}
		inner = append(inner, sp)
	}
	return rootsReached(f.roots(), inv.prefix, inner)
}

// rootsReached says why pathspecs do not provably keep a git command away
// from every one of roots, "" when they do: each root is excluded, or no
// positive pathspec touches it. No positive pathspec means the whole tree.
// An exclusion counts only when it can be placed and covers the whole
// root. prefix is where the roots are, for the message: "" for this
// repository, a nested work tree's path for its roots. A pathspec's own
// text is never in the reason.
func rootsReached(roots []string, prefix string, specs []pathspec) string {
	var positives []pathspec
	for _, sp := range specs {
		if !sp.exclude {
			positives = append(positives, sp)
		}
	}
	whole := len(positives) == 0
	if whole {
		positives = []pathspec{{rel: "."}}
	}
	for _, root := range roots {
		excluded := false
		for _, sp := range specs {
			if sp.exclude && !sp.unprovable && (sp.rel == "." || root == sp.rel || strings.HasPrefix(root, sp.rel+"/")) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		shown := displayPath(path.Join(prefix, root))
		for _, sp := range positives {
			switch {
			case sp.unprovable && sp.why != "":
				return "is given " + sp.why + ", so it may print content from the protected root " + shown
			case sp.unprovable:
				return "is given a pathspec the gate cannot place, so it may print content from the protected root " + shown
			case whole:
				return "has no pathspec that keeps it out of the protected root " + shown
			case touchesRoot(sp.rel, root):
				return "is given a path that reaches the protected root " + shown
			}
		}
	}
	return ""
}

// touchesRoot reports whether the repository-relative path rel is root, is
// inside it, or contains it.
func touchesRoot(rel, root string) bool {
	return rel == "." || rel == root || strings.HasPrefix(rel, root+"/") || strings.HasPrefix(root, rel+"/")
}
